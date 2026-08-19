import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  glossaryLabel, koLabel, sessionLifecycleLabel, personTerm,
  formatDurationKo, formatBytesKo, formatTokensKo, formatCurrencyKo, formatDateTimeKo,
  setGlossaryTelemetry, _resetGlossaryTelemetryForTests,
} from './glossary.ts'

test('entity/lifecycle/severity/decision/action/evidence resolve to Korean labels', () => {
  assert.equal(glossaryLabel('entity', 'user').ko, '사용자')
  assert.equal(glossaryLabel('lifecycle', 'quarantined').ko, '격리')
  assert.equal(glossaryLabel('severity', 'critical').ko, '심각')
  assert.equal(glossaryLabel('decision', 'allowed').ko, '허용')
  assert.equal(glossaryLabel('decision', 'partially_compliant').ko, '부분 준수')
  assert.equal(glossaryLabel('action', 'file_write').ko, '파일 작성')
  assert.equal(glossaryLabel('evidence', 'audit_chain').ko, '감사 체인')
})

test('known raw enum never leaks as the only label', () => {
  for (const raw of ['active', 'pending', 'in_progress', 'elevated', 'pre_approved', 'normal']) {
    const l = koLabel('lifecycle', raw)
    assert.notEqual(l, raw)
    assert.ok(l.length > 0)
  }
})

test('unknown/new enums fall back to a safe Korean label and emit telemetry', () => {
  const seen = []
  setGlossaryTelemetry((field, raw) => seen.push(`${field}:${raw}`))
  const l = glossaryLabel('decision', 'brand_new_state')
  assert.equal(l.ko, '알 수 없는 상태')
  assert.ok(seen.includes('decision:brand_new_state'))
  _resetGlossaryTelemetryForTests()
})

test('session lifecycle composes from the canonical module', () => {
  assert.equal(sessionLifecycleLabel('active'), '활성')
  assert.equal(sessionLifecycleLabel('paused'), '일시정지')
})

test('person term: 사용자 general, 개발자 only for explicit developer role', () => {
  assert.equal(personTerm(undefined), '사용자')
  assert.equal(personTerm('engineer'), '사용자')
  assert.equal(personTerm('developer'), '개발자')
  assert.equal(personTerm('admin'), '사용자')
})

test('Korean-first formatters', () => {
  assert.equal(formatDurationKo(90), '1분')
  assert.equal(formatDurationKo(7200), '2시간')
  assert.equal(formatBytesKo(2048), '2.0 KB')
  assert.equal(formatTokensKo(1500000), '1,500,000 토큰')
  assert.equal(formatCurrencyKo(1234), '1,234원')
  assert.equal(formatCurrencyKo(9.99, 'USD'), '$9.99')
  assert.equal(formatDurationKo(undefined), '—')
  assert.match(formatDateTimeKo('2026-08-18T10:00:00Z'), /2026/)
})
