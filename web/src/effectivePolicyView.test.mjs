import test from 'node:test'
import assert from 'node:assert/strict'

import { buildSourceTrace, buildScopePath, summarizeRuleConfig, summarizeValue, parseModelRefs, ackSummary } from './effectivePolicyView.ts'

const rule = (id, domain, scope, scopeName, config, extra = {}) => ({
  id, domain, name: `규칙-${id}`, scope, scopeName, status: 'approved', enabled: true, config, ...extra,
})

test('single org rule resolves as inherited with the org as winning source', () => {
  const rules = [rule('r1', 'network', 'org', '', { destinations: ['gitlab.example', 'internal-inference'] })]
  const effective = {
    network: { destinations: ['gitlab.example', 'internal-inference'] },
    rules: [{ rule_id: 'r1', domain: 'network', name: '규칙-r1', scope: 'org', scope_name: '' }],
  }
  const traces = buildSourceTrace(effective, rules, [])
  assert.equal(traces.length, 1)
  const t = traces[0]
  assert.equal(t.domainName, '네트워크 정책')
  assert.equal(t.keyLabel, '허용 통신 대상')
  assert.equal(t.summary, 'gitlab.example, internal-inference')
  assert.equal(t.state, 'inherited')
  assert.equal(t.stateLabel, '상속됨')
  assert.equal(t.winner.ruleId, 'r1')
  assert.equal(t.winner.scopeLabel, '조직')
  assert.deepEqual(t.overridden, [])
})

test('project rule overriding an org key wins and lists the parent as overridden', () => {
  const rules = [
    rule('org1', 'tools', 'org', '', { danger_levels: ['high', 'critical'] }),
    rule('prj1', 'tools', 'project', 'pcp', { danger_levels: ['critical'] }),
  ]
  const effective = {
    tools: { danger_levels: ['critical'] },
    rules: [
      { rule_id: 'org1', domain: 'tools', name: '규칙-org1', scope: 'org', scope_name: '' },
      { rule_id: 'prj1', domain: 'tools', name: '규칙-prj1', scope: 'project', scope_name: 'pcp' },
    ],
  }
  const [t] = buildSourceTrace(effective, rules, [])
  assert.equal(t.state, 'overridden')
  assert.equal(t.winner.ruleId, 'prj1')
  assert.equal(t.winner.scopeLabel, '프로젝트')
  assert.equal(t.winner.scopeName, 'pcp')
  assert.deepEqual(t.overridden.map(o => o.ruleId), ['org1'])
})

test('multiple overrides keep the full chain from org through repo', () => {
  const rules = [
    rule('a', 'session', 'org', '', { max_duration_minutes: 480 }),
    rule('b', 'session', 'project', 'pcp', { max_duration_minutes: 240 }),
    rule('c', 'session', 'repo', 'pcp-repo', { max_duration_minutes: 60 }),
  ]
  const effective = {
    session: { max_duration_minutes: 60 },
    rules: [
      { rule_id: 'a', domain: 'session', name: '규칙-a', scope: 'org', scope_name: '' },
      { rule_id: 'b', domain: 'session', name: '규칙-b', scope: 'project', scope_name: 'pcp' },
      { rule_id: 'c', domain: 'session', name: '규칙-c', scope: 'repo', scope_name: 'pcp-repo' },
    ],
  }
  const [t] = buildSourceTrace(effective, rules, [])
  assert.equal(t.winner.ruleId, 'c')
  assert.equal(t.summary, '60')
  assert.deepEqual(t.overridden.map(o => o.ruleId), ['a', 'b'])
})

test('conflicting sibling scopes on the same layer are flagged as conflicts', () => {
  const rules = [
    rule('x', 'network', 'project', 'pcp', { destinations: ['a.example'] }),
    rule('y', 'network', 'project', 'pcp', { destinations: ['b.example'] }),
  ]
  const effective = {
    network: { destinations: ['b.example'] },
    rules: [
      { rule_id: 'x', domain: 'network', name: '규칙-x', scope: 'project', scope_name: 'pcp' },
      { rule_id: 'y', domain: 'network', name: '규칙-y', scope: 'project', scope_name: 'pcp' },
    ],
  }
  const [t] = buildSourceTrace(effective, rules, [])
  assert.equal(t.conflict, true)
  assert.equal(t.winner.ruleId, 'y')
  assert.deepEqual(t.overridden.map(o => o.ruleId), ['x'])
})

test('an approved exception marks the winning rule as weakened; denied and pending do not', () => {
  const rules = [rule('r1', 'network', 'org', '', { destinations: ['gitlab.example'] })]
  const effective = {
    network: { destinations: ['gitlab.example'] },
    rules: [{ rule_id: 'r1', domain: 'network', name: '규칙-r1', scope: 'org', scope_name: '' }],
  }
  const approved = buildSourceTrace(effective, rules, [
    { id: 'ex1', scope: 'project', scopeName: 'pcp', status: 'approved', reason: '레거시 마이그레이션', rule_ids: '["r1"]' },
  ])
  assert.equal(approved[0].state, 'exception')
  assert.equal(approved[0].stateLabel, '예외로 완화')
  assert.equal(approved[0].exception.id, 'ex1')

  for (const status of ['denied', 'pending']) {
    const [t] = buildSourceTrace(effective, rules, [
      { id: 'ex2', scope: 'project', scopeName: 'pcp', status, rule_ids: '["r1"]' },
    ])
    assert.equal(t.state, 'inherited')
    assert.equal(t.exception, undefined)
  }
})

test('a winning ref missing from the rule registry is reported as a deleted source', () => {
  const effective = {
    network: { destinations: ['gitlab.example'] },
    rules: [{ rule_id: 'ghost', domain: 'network', name: '삭제된 규칙', scope: 'org', scope_name: '' }],
  }
  const [t] = buildSourceTrace(effective, [], [])
  assert.equal(t.state, 'deleted_source')
  assert.equal(t.stateLabel, '원본 삭제됨')
  assert.equal(t.winner.deleted, true)
  assert.equal(t.winner.name, '삭제된 규칙')
})

test('model domain intersects layers and an empty result reads as full denial', () => {
  const rules = [
    rule('m1', 'models', 'org', '', { allowed_models: ['pmp_demo_kocoder', 'pmp_large'] }),
    rule('m2', 'models', 'project', 'pcp', { allowed_models: ['pmp_large'] }),
  ]
  const effective = {
    allowed_models: ['pmp_large'],
    rules: [
      { rule_id: 'm1', domain: 'models', name: '규칙-m1', scope: 'org', scope_name: '' },
      { rule_id: 'm2', domain: 'models', name: '규칙-m2', scope: 'project', scope_name: 'pcp' },
    ],
  }
  const [t] = buildSourceTrace(effective, rules, [])
  assert.equal(t.keyLabel, '허용 모델')
  assert.equal(t.state, 'overridden')
  assert.equal(t.winner.ruleId, 'm2')
  assert.equal(summarizeValue('models', 'allowed_models', []), '없음 — 전체 차단')
})

test('unknown domains and keys fall back to generic labels without crashing', () => {
  const effective = {
    custom_domain: { unknown_key: { nested: true } },
    rules: [{ rule_id: 'u1', domain: 'custom_domain', name: '커스텀', scope: 'org', scope_name: '' }],
  }
  const [t] = buildSourceTrace(effective, [], [])
  assert.equal(t.domainName, 'custom_domain')
  assert.equal(t.keyLabel, 'unknown_key')
  assert.equal(t.summary, '세부 설정 1개 항목')
})

test('scope path lists contributing layers once, in resolution order', () => {
  const effective = {
    rules: [
      { rule_id: 'a', domain: 'tools', name: 'a', scope: 'org', scope_name: '' },
      { rule_id: 'b', domain: 'tools', name: 'b', scope: 'project', scope_name: 'pcp' },
      { rule_id: 'c', domain: 'network', name: 'c', scope: 'project', scope_name: 'pcp' },
      { rule_id: 'd', domain: 'network', name: 'd', scope: 'repo', scope_name: 'pcp-repo' },
    ],
  }
  assert.deepEqual(buildScopePath(effective), ['조직', '프로젝트 · pcp', '저장소 · pcp-repo'])
  assert.deepEqual(buildScopePath(null), [])
})

test('typed config summaries render Korean rows instead of raw JSON', () => {
  assert.deepEqual(summarizeRuleConfig('tools', { danger_levels: ['high', 'critical'], require_approval: true }), [
    { key: 'danger_levels', label: '허용 위험 등급', text: 'high, critical' },
    { key: 'require_approval', label: '승인 필요 도구', text: '적용' },
  ])
  assert.deepEqual(summarizeRuleConfig('tools', undefined), [])
})

test('epoch model refs parse from both arrays and JSON strings', () => {
  assert.deepEqual(parseModelRefs(['pmp_demo_kocoder']), ['pmp_demo_kocoder'])
  assert.deepEqual(parseModelRefs('["pmp_demo_kocoder","pmp_large"]'), ['pmp_demo_kocoder', 'pmp_large'])
  assert.deepEqual(parseModelRefs('not-json'), [])
  assert.deepEqual(parseModelRefs(undefined), [])
})

test('ack summary counts required, acknowledged, and pending users including partial acks', () => {
  assert.deepEqual(ackSummary([{ acked: true }, { acked: false }, {}]), { required: 3, acknowledged: 1, pending: 2 })
  assert.deepEqual(ackSummary([]), { required: 0, acknowledged: 0, pending: 0 })
})
