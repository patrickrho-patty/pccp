// Effective-policy view logic (PAT-1505): typed Korean renderers per policy
// domain plus a single source-trace DTO built from the effective-policy
// response, the rule registry, and the exception marketplace. Pure module so
// inheritance/conflict fixtures run under node:test.

export interface RuleRef {
  rule_id: string
  domain: string
  name: string
  scope: string
  scope_name: string
}

export interface RegistryRule {
  id: string
  domain: string
  name: string
  nameEn?: string
  desc?: string
  scope: string
  scopeName?: string
  status?: string
  enabled?: boolean
  template_id?: string
  config?: Record<string, any>
  created_at?: string
  updated_at?: string
}

export interface ExceptionEntry {
  id: string
  scope: string
  scope_id?: string
  scopeName?: string
  status: string
  reason?: string
  rule_ids?: string
}

export interface SourceRef {
  ruleId: string
  name: string
  scope: string
  scopeLabel: string
  scopeName: string
  templateId?: string
  effectiveAt?: string
  deleted?: boolean
}

export type TraceState = 'inherited' | 'overridden' | 'exception' | 'deleted_source'

export interface RuleTrace {
  domain: string
  domainName: string
  key: string
  keyLabel: string
  summary: string
  state: TraceState
  stateLabel: string
  conflict: boolean
  winner: SourceRef | null
  overridden: SourceRef[]
  exception?: { id: string; scopeName: string; reason?: string }
}

export const DOMAIN_NAMES: Record<string, string> = {
  models: '모델 접근',
  tools: '도구 권한',
  data: '데이터 보호',
  scm: 'Git/SCM',
  network: '네트워크',
  session: '세션',
}

const KEY_LABELS: Record<string, Record<string, string>> = {
  models: { allowed_models: '허용 모델' },
  tools: { danger_levels: '허용 위험 등급', allowed_tools: '허용 도구', require_approval: '승인 필요 도구', blocked_tools: '차단 도구' },
  data: { redact_patterns: '마스킹 패턴', block_secrets: '비밀정보 차단', pii: '개인정보 보호' },
  scm: { protected_branches: '보호 브랜치', require_pr: 'PR 필수 여부', reviewers: '최소 승인자 수' },
  network: { destinations: '허용 통신 대상', allowed_hosts: '허용 호스트', deny: '차단 대상' },
  session: { max_duration_minutes: '최대 세션 시간(분)', max_concurrent: '최대 동시 세션', idle_timeout_minutes: '유휴 종료(분)' },
}

const SCOPE_LABELS: Record<string, string> = {
  org: '조직',
  project: '프로젝트',
  repo: '저장소',
  team: '부서',
  user: '사용자',
  session: '세션',
}

const STATE_LABELS: Record<TraceState, string> = {
  inherited: '상속됨',
  overridden: '재정의 적용',
  exception: '예외로 완화',
  deleted_source: '원본 삭제됨',
}

export function scopeLabel(scope: string): string {
  return SCOPE_LABELS[scope] || scope || '알 수 없는 범위'
}

export function keyLabel(domain: string, key: string): string {
  return KEY_LABELS[domain]?.[key] || key
}

// summarizeValue renders one config value as Korean text — never raw JSON
// unless the shape is genuinely unknown.
export function summarizeValue(domain: string, key: string, value: any): string {
  if (value == null) return '설정 없음'
  if (typeof value === 'boolean') return value ? '적용' : '미적용'
  if (typeof value === 'number') return `${value}`
  if (typeof value === 'string') return value
  if (Array.isArray(value)) {
    if (value.length === 0) return domain === 'models' || key === 'allowed_models' ? '없음 — 전체 차단' : '없음'
    return value.map(v => (typeof v === 'object' ? JSON.stringify(v) : `${v}`)).join(', ')
  }
  return JSON.stringify(value)
}

// summarizeRuleConfig turns a rule config into typed Korean rows.
export function summarizeRuleConfig(domain: string, config: Record<string, any> | undefined): Array<{ key: string; label: string; text: string }> {
  if (!config || typeof config !== 'object') return []
  return Object.keys(config).map(key => ({ key, label: keyLabel(domain, key), text: summarizeValue(domain, key, config[key]) }))
}

function parseExceptionRuleIds(ex: ExceptionEntry): string[] {
  try {
    const ids = JSON.parse(ex.rule_ids || '[]')
    return Array.isArray(ids) ? ids : []
  } catch {
    return []
  }
}

function toSourceRef(ref: RuleRef, registry: Map<string, RegistryRule>): SourceRef {
  const rule = registry.get(ref.rule_id)
  return {
    ruleId: ref.rule_id,
    name: ref.name || rule?.name || ref.rule_id,
    scope: ref.scope,
    scopeLabel: scopeLabel(ref.scope),
    scopeName: ref.scope_name || rule?.scopeName || '',
    templateId: rule?.template_id || undefined,
    effectiveAt: rule?.updated_at || rule?.created_at || undefined,
    deleted: !rule,
  }
}

// buildSourceTrace produces one trace DTO per effective domain key: the
// winning source, the parent sources it overrode, sibling-scope conflicts,
// and whether an approved exception weakened the rule.
export function buildSourceTrace(
  effective: Record<string, any> | null | undefined,
  rules: RegistryRule[],
  exceptions: ExceptionEntry[],
): RuleTrace[] {
  if (!effective) return []
  const refs: RuleRef[] = Array.isArray(effective.rules) ? effective.rules : []
  const registry = new Map(rules.map(r => [r.id, r]))
  const approvedExceptions = exceptions.filter(ex => ex.status === 'approved')
  const traces: RuleTrace[] = []

  const exceptionFor = (ruleId: string) => {
    const ex = approvedExceptions.find(e => parseExceptionRuleIds(e).includes(ruleId))
    return ex ? { id: ex.id, scopeName: ex.scopeName || ex.scope_id || ex.scope, reason: ex.reason } : undefined
  }

  const finish = (domain: string, key: string, value: any, domainRefs: RuleRef[]) => {
    // Winner: the deepest (last) layer that actually sets this key. A ref
    // whose rule left the registry is a deleted source; unknown rule
    // versions fall back to the ref's own metadata.
    const withKey = domainRefs.filter(ref => {
      const cfg = registry.get(ref.rule_id)?.config
      if (!cfg) return true // deleted from registry — assume it carried the key
      return Object.prototype.hasOwnProperty.call(cfg, key)
    })
    const winnerRef = withKey[withKey.length - 1] || domainRefs[domainRefs.length - 1]
    const winner = winnerRef ? toSourceRef(winnerRef, registry) : null
    const overridden = withKey.slice(0, -1).map(ref => toSourceRef(ref, registry))
    const conflict = new Set(withKey.map(ref => `${ref.scope}:${ref.scope_name}`)).size < withKey.length
    const exception = winnerRef ? exceptionFor(winnerRef.rule_id) : undefined
    const state: TraceState = winner?.deleted ? 'deleted_source' : exception ? 'exception' : overridden.length > 0 ? 'overridden' : 'inherited'
    traces.push({
      domain,
      domainName: DOMAIN_NAMES[domain] || domain,
      key,
      keyLabel: keyLabel(domain, key),
      summary: summarizeValue(domain, key, value),
      state,
      stateLabel: STATE_LABELS[state],
      conflict,
      winner,
      overridden,
      exception,
    })
  }

  if (effective.allowed_models !== undefined) {
    finish('models', 'allowed_models', effective.allowed_models, refs.filter(r => r.domain === 'models'))
  }
  for (const domain of Object.keys(effective)) {
    if (domain === 'rules' || domain === 'allowed_models') continue
    const cfg = effective[domain]
    if (!cfg || typeof cfg !== 'object' || Array.isArray(cfg)) continue
    const domainRefs = refs.filter(r => r.domain === domain)
    for (const key of Object.keys(cfg)) {
      finish(domain, key, cfg[key], domainRefs)
    }
  }
  return traces
}

// buildScopePath visualizes the resolution path of contributing layers.
export function buildScopePath(effective: Record<string, any> | null | undefined): string[] {
  const refs: RuleRef[] = Array.isArray(effective?.rules) ? effective.rules : []
  const path: string[] = []
  for (const ref of refs) {
    const label = ref.scope_name ? `${scopeLabel(ref.scope)} · ${ref.scope_name}` : scopeLabel(ref.scope)
    if (!path.includes(label)) path.push(label)
  }
  return path
}

// parseModelRefs normalizes epoch allowed_models (array or JSON string).
export function parseModelRefs(value: any): string[] {
  if (Array.isArray(value)) return value.filter(v => typeof v === 'string')
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value)
      return Array.isArray(parsed) ? parsed.filter(v => typeof v === 'string') : []
    } catch {
      return []
    }
  }
  return []
}

// ackSummary aggregates required/acknowledged/pending counts for one epoch.
export function ackSummary(acks: Array<{ acked?: boolean }>): { required: number; acknowledged: number; pending: number } {
  const required = acks.length
  const acknowledged = acks.filter(a => a.acked).length
  return { required, acknowledged, pending: required - acknowledged }
}
