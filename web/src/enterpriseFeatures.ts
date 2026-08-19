// Enterprise harness feature governance — canonical catalog, change
// validation, impact preview, and versioned rollout/rollback records.
// Rollout metadata rides on the feature's existing `config` JSON text
// field (internal/models/enterprise.go), so no backend change is needed.

import { isHarnessOnlineNow } from './harnessHealth.ts'

export interface CatalogEntry {
  key: string
  purposeKo: string
  rationaleKo: string
  minHarnessVersion: string
  dependencies: string[]
  mandatory: boolean // Patty-mandatory: tenant admins cannot weaken it
}

export interface Scope {
  type: 'org' | 'selected'
  harness_ids: string[]
  exceptions: string[]
}

export interface HarnessInfo {
  harness_id: string
  name?: string
  binary_version?: string
  status?: string
  last_heartbeat?: string
}

export interface HarnessEval {
  harness_id: string
  name: string
  version: string
  online: boolean
  compatible: boolean
  result: '적용 가능' | '버전 미충족' | '오프라인 대기'
}

export interface RolloutRecord {
  epoch: number
  kind: 'change' | 'rollback'
  at: string
  by: string
  reason: string
  scope: Scope
  from: { enabled: boolean; enforced: boolean }
  to: { enabled: boolean; enforced: boolean }
  rollback_of?: number
  results: HarnessEval[]
}

export interface Governance {
  scope: Scope
  rollouts: RolloutRecord[]
}

export interface FeatureLike {
  feature_key: string
  enabled: boolean
  enforced: boolean
  status: string
  config?: string
}

export const PATTY_ROLES = ['super_admin', 'security_admin']
export const ADMIN_ROLES = [...PATTY_ROLES, 'admin', 'owner']

// Canonical catalog — keys match the seed defaults in
// internal/api/server.go (handleSeedEnterpriseFeatures).
export const FEATURE_CATALOG: Record<string, CatalogEntry> = {
  code_review: {
    key: 'code_review', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: '모든 코드 변경에 정책 기반 리뷰를 강제합니다.',
    rationaleKo: '승인되지 않은 코드 유입을 차단해 감사 추적성을 확보합니다 (PRD §33.4).',
  },
  code_signing: {
    key: 'code_signing', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: '배포되는 코드에 조직 키 서명을 의무화합니다.',
    rationaleKo: '서명되지 않은 산출물의 실행을 막아 공급망 무결성을 보장합니다 (PRD §18.6).',
  },
  coding_standards: {
    key: 'coding_standards', mandatory: false, minHarnessVersion: '1.0.0', dependencies: ['code_review'],
    purposeKo: '컴플라이언스 규칙을 반영한 코딩 표준을 적용합니다.',
    rationaleKo: '표준 위반 코드를 생성 단계에서 줄여 리뷰 부담을 낮춥니다 (PRD §33.11).',
  },
  audit_export: {
    key: 'audit_export', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: '감사 증거를 외부 보관소로 보냅니다.',
    rationaleKo: '규제 감사 대응을 위해 증거의 외부 보존이 필수입니다 (PRD §40.3).',
  },
  sso_binding: {
    key: 'sso_binding', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: '하네스 사용자를 기업 SSO/SCIM 신원에 연결합니다.',
    rationaleKo: '신원 없는 작업을 차단하고 퇴사자 접근을 즉시 해지합니다 (PRD §32.1).',
  },
  device_attestation: {
    key: 'device_attestation', mandatory: true, minHarnessVersion: '1.0.0', dependencies: ['sso_binding'],
    purposeKo: '하네스 기기의 보안 상태를 증명받습니다.',
    rationaleKo: '침해된 기기에서의 작업을 차단해 제로트러스트를 실현합니다 (PRD §14.1).',
  },
  sandbox_execution: {
    key: 'sandbox_execution', mandatory: false, minHarnessVersion: '2.0.0', dependencies: ['network_egress'],
    purposeKo: '생성된 코드를 격리된 샌드박스에서만 실행합니다.',
    rationaleKo: '검증되지 않은 코드의 호스트 실행을 원천 차단합니다 (PRD §31.2). 아직 시행되지 않았습니다.',
  },
  data_classification: {
    key: 'data_classification', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: '코드와 데이터에 분류 등급 태그를 부여합니다.',
    rationaleKo: 'DLP 정책 적용의 기준이 되는 분류 체계를 확립합니다 (PRD §16).',
  },
  supply_chain: {
    key: 'supply_chain', mandatory: true, minHarnessVersion: '1.0.0', dependencies: ['code_signing'],
    purposeKo: '의존성과 빌드 산출물의 공급망을 검증합니다.',
    rationaleKo: '악성 의존성 유입을 차단하고 SBOM 증적을 남깁니다 (PRD §15.3).',
  },
  network_egress: {
    key: 'network_egress', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: '하네스의 외부 네트워크 송신을 허용 목록으로 제한합니다.',
    rationaleKo: '데이터 유출 경로를 차단하는 최후 방어선입니다 (PRD §17.4).',
  },
  secret_broker: {
    key: 'secret_broker', mandatory: true, minHarnessVersion: '1.0.0', dependencies: ['sso_binding'],
    purposeKo: '키와 비밀정보를 중앙 브로커를 통해 발급합니다.',
    rationaleKo: '하네스 로컬에 비밀정보가 상주하지 않도록 합니다 (PRD §17.5).',
  },
  forensic_snapshot: {
    key: 'forensic_snapshot', mandatory: false, minHarnessVersion: '1.0.0', dependencies: ['audit_export'],
    purposeKo: '사고 시점의 작업 상태 스냅샷을 보존합니다.',
    rationaleKo: '침해 사고 분석과 증거 보전을 지원합니다 (PRD §14.2).',
  },
  exception_workflow: {
    key: 'exception_workflow', mandatory: false, minHarnessVersion: '1.0.0', dependencies: ['mandatory_ack'],
    purposeKo: '정책 예외를 승인 워크플로로 관리합니다.',
    rationaleKo: '예외를 무기한 방치하지 않고 승인·만료·감사를 강제합니다 (PRD §33.8).',
  },
  mandatory_ack: {
    key: 'mandatory_ack', mandatory: true, minHarnessVersion: '2.0.0', dependencies: [],
    purposeKo: '정책 고지에 대한 사용자 승인 확인을 의무화합니다.',
    rationaleKo: '고지 없는 정책 변경의 법적 분쟁을 예방합니다 (PRD §33.6). 아직 시행되지 않았습니다.',
  },
  change_freeze: {
    key: 'change_freeze', mandatory: false, minHarnessVersion: '1.0.0', dependencies: ['code_review'],
    purposeKo: '지정 기간 동안 모든 변경을 동결합니다.',
    rationaleKo: '감사·결산 기간의 변경 리스크를 통제합니다 (PRD §33.13).',
  },
  ai_attribution: {
    key: 'ai_attribution', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: 'AI 생성 코드의 기여를 추적·표기합니다.',
    rationaleKo: 'AI 생성물의 책임 소재와 라이선스 검토를 가능하게 합니다 (PRD §19).',
  },
  command_auth: {
    key: 'command_auth', mandatory: true, minHarnessVersion: '1.0.0', dependencies: [],
    purposeKo: '하네스 명령 실행에 인가를 요구합니다.',
    rationaleKo: '권한 없는 명령 실행을 차단해 내부자 위협을 줄입니다 (PRD §17.3).',
  },
  mcp_allowlist: {
    key: 'mcp_allowlist', mandatory: true, minHarnessVersion: '1.0.0', dependencies: ['command_auth'],
    purposeKo: '연결 가능한 MCP 서버를 허용 목록으로 제한합니다.',
    rationaleKo: '검증되지 않은 외부 도구 연결을 차단합니다 (PRD §17.2).',
  },
  model_recall: {
    key: 'model_recall', mandatory: true, minHarnessVersion: '1.0.0', dependencies: ['command_auth'],
    purposeKo: '결함 모델을 전 하네스에서 긴급 리콜합니다.',
    rationaleKo: '유해 모델의 지속 사용을 즉시 중단시킵니다 (PRD §33.9).',
  },
  project_offboard: {
    key: 'project_offboard', mandatory: false, minHarnessVersion: '1.0.0', dependencies: ['audit_export'],
    purposeKo: '프로젝트 종료 시 접근 권한과 잔여 데이터를 정리합니다.',
    rationaleKo: '종료 프로젝트의 잔여 접근 경로를 제거합니다 (PRD §33.14).',
  },
}

export function defaultScope(): Scope {
  return { type: 'org', harness_ids: [], exceptions: [] }
}

export function parseGovernance(config: string | undefined | null): Governance {
  const fallback: Governance = { scope: defaultScope(), rollouts: [] }
  if (!config) return fallback
  try {
    const parsed = JSON.parse(config)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return fallback
    const scope = parsed.scope && typeof parsed.scope === 'object'
      ? {
          type: parsed.scope.type === 'selected' ? 'selected' as const : 'org' as const,
          harness_ids: Array.isArray(parsed.scope.harness_ids) ? parsed.scope.harness_ids.filter((x: unknown) => typeof x === 'string') : [],
          exceptions: Array.isArray(parsed.scope.exceptions) ? parsed.scope.exceptions.filter((x: unknown) => typeof x === 'string') : [],
        }
      : defaultScope()
    const rollouts = Array.isArray(parsed.rollouts) ? parsed.rollouts.filter((r: unknown) => r && typeof r === 'object' && typeof (r as RolloutRecord).epoch === 'number') : []
    return { scope, rollouts }
  } catch {
    return fallback
  }
}

export function scopeHarnessIds(scope: Scope, allHarnessIds: string[]): string[] {
  if (scope.type === 'selected') return scope.harness_ids.filter(id => !scope.exceptions.includes(id))
  return allHarnessIds.filter(id => !scope.exceptions.includes(id))
}

// "1.10.0" >= "1.2.0" — numeric segment compare, non-numeric is incompatible.
export function versionAtLeast(version: string | undefined, min: string): boolean {
  if (!version) return false
  const a = version.split('.').map(Number)
  const b = min.split('.').map(Number)
  if (a.some(Number.isNaN) || b.some(Number.isNaN)) return false
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const x = a[i] ?? 0
    const y = b[i] ?? 0
    if (x !== y) return x > y
  }
  return true
}

// Heartbeat staleness is owned by harnessHealth.ts — do not re-implement.
export function isHarnessOnline(h: HarnessInfo, now: number): boolean {
  return isHarnessOnlineNow(h, now)
}

// Per-harness applicability for a scope: exact harness, online, and
// version-compatible against the catalog requirement.
export function evaluateHarnesses(entry: CatalogEntry, harnesses: HarnessInfo[], scope: Scope, now: number): HarnessEval[] {
  const ids = scopeHarnessIds(scope, harnesses.map(h => h.harness_id))
  const byId = new Map(harnesses.map(h => [h.harness_id, h]))
  return ids.map(id => {
    const h = byId.get(id)
    const online = h ? isHarnessOnline(h, now) : false
    const compatible = h ? versionAtLeast(h.binary_version, entry.minHarnessVersion) : false
    return {
      harness_id: id,
      name: h?.name || id,
      version: h?.binary_version || '알 수 없음',
      online,
      compatible,
      result: !online ? '오프라인 대기' : !compatible ? '버전 미충족' : '적용 가능',
    }
  })
}

export interface ChangeValidation {
  blockers: string[]
  warnings: string[]
}

export function validateChange(args: {
  feature: FeatureLike
  features: FeatureLike[]
  harnesses: HarnessInfo[]
  target: { enabled: boolean; enforced: boolean }
  scope: Scope
  role: string
  now: number
}): ChangeValidation {
  const { feature, features, harnesses, target, scope, role, now } = args
  const blockers: string[] = []
  const warnings: string[] = []
  const entry = FEATURE_CATALOG[feature.feature_key]

  if (!ADMIN_ROLES.includes(role)) {
    blockers.push('기능 변경 권한이 없습니다. 관리자 역할이 필요합니다.')
    return { blockers, warnings }
  }
  if (!entry) {
    blockers.push(`기능 키 '${feature.feature_key}'는 카탈로그에 등록되지 않았습니다.`)
    return { blockers, warnings }
  }
  if (target.enabled === feature.enabled && target.enforced === feature.enforced) {
    blockers.push('변경 사항이 없습니다.')
    return { blockers, warnings }
  }
  if (target.enforced && !target.enabled) {
    blockers.push('비활성화된 기능은 강제 적용할 수 없습니다.')
  }
  // Patty-mandatory: tenant admins may not weaken (disable or unenforce).
  const weakening = (feature.enabled && !target.enabled) || (feature.enforced && !target.enforced)
  if (entry.mandatory && weakening && !PATTY_ROLES.includes(role)) {
    blockers.push('패티 필수 기능은 테넌트 관리자가 비활성화하거나 강제를 해제할 수 없습니다.')
  }
  const strengthening = (!feature.enabled && target.enabled) || (!feature.enforced && target.enforced)
  if (strengthening) {
    if (feature.status === 'planned') {
      blockers.push('아직 시행되지 않은 기능입니다. 하네스 지원 버전이 출시된 후 활성화할 수 있습니다.')
    }
    for (const depKey of entry.dependencies) {
      const dep = features.find(f => f.feature_key === depKey)
      const depEntry = FEATURE_CATALOG[depKey]
      if (!dep || !dep.enabled || dep.status === 'planned') {
        blockers.push(`의존 기능 '${depKey}'이(가) 비활성 상태입니다. 먼저 활성화하세요.`)
      }
    }
    if (scope.type === 'selected' && scope.harness_ids.length === 0) {
      blockers.push('하네스 지정 범위를 선택했지만 대상 하네스가 없습니다.')
    }
  }
  const evals = evaluateHarnesses(entry, harnesses, scope, now)
  const incompatible = evals.filter(e => e.online && !e.compatible)
  const offline = evals.filter(e => !e.online)
  if (target.enforced && incompatible.length > 0) {
    blockers.push(`요구 버전(v${entry.minHarnessVersion}+) 미충족 하네스 ${incompatible.length}대가 있어 강제 적용할 수 없습니다: ${incompatible.map(e => e.name).join(', ')}`)
  } else if (incompatible.length > 0) {
    warnings.push(`요구 버전(v${entry.minHarnessVersion}+) 미충족 하네스 ${incompatible.length}대에는 적용되지 않습니다.`)
  }
  if (offline.length > 0) {
    warnings.push(`오프라인 하네스 ${offline.length}대는 재연결 후 적용됩니다: ${offline.map(e => e.name).join(', ')}`)
  }
  return { blockers, warnings }
}

export function buildPreview(
  feature: FeatureLike,
  target: { enabled: boolean; enforced: boolean },
  scope: Scope,
  evals: HarnessEval[],
): string[] {
  const lines: string[] = []
  const onOff = (v: boolean) => (v ? '켜짐' : '꺼짐')
  if (target.enabled !== feature.enabled) lines.push(`활성화: ${onOff(feature.enabled)} → ${onOff(target.enabled)}`)
  if (target.enforced !== feature.enforced) lines.push(`강제 적용: ${onOff(feature.enforced)} → ${onOff(target.enforced)}`)
  const scopeLabel = scope.type === 'org'
    ? `조직 전체${scope.exceptions.length > 0 ? ` (예외 ${scope.exceptions.length}개 하네스)` : ''}`
    : `지정 하네스 ${scope.harness_ids.length}대`
  lines.push(`적용 범위: ${scopeLabel}`)
  lines.push(`영향 하네스: 적용 가능 ${evals.filter(e => e.result === '적용 가능').length}대 · 버전 미충족 ${evals.filter(e => e.result === '버전 미충족').length}대 · 오프라인 ${evals.filter(e => e.result === '오프라인 대기').length}대`)
  return lines
}

export function headEpoch(g: Governance): number {
  return g.rollouts.length === 0 ? 0 : Math.max(...g.rollouts.map(r => r.epoch))
}

export function headEpochOf(config: string | undefined | null): number {
  return headEpoch(parseGovernance(config))
}

export interface ApplyResult {
  config?: string
  record?: RolloutRecord
  error?: string
}

// Appends a versioned rollout record. expectedEpoch guards concurrent
// changes: if the stored head moved since the dialog opened, fail.
export function applyChange(args: {
  feature: FeatureLike
  target: { enabled: boolean; enforced: boolean }
  scope: Scope
  reason: string
  actor: string
  now: string
  evals: HarnessEval[]
  expectedEpoch: number
}): ApplyResult {
  const { feature, target, scope, reason, actor, now, evals, expectedEpoch } = args
  if (!reason.trim()) return { error: '변경 사유를 입력해야 합니다.' }
  const g = parseGovernance(feature.config)
  if (headEpoch(g) !== expectedEpoch) {
    return { error: `동시 변경이 감지되었습니다(현재 에포크 ${headEpoch(g)}, 예상 ${expectedEpoch}). 목록을 새로고침한 후 다시 시도하세요.` }
  }
  const record: RolloutRecord = {
    epoch: expectedEpoch + 1,
    kind: 'change',
    at: now,
    by: actor,
    reason: reason.trim(),
    scope,
    from: { enabled: feature.enabled, enforced: feature.enforced },
    to: target,
    results: evals,
  }
  const next: Governance = { scope, rollouts: [...g.rollouts, record] }
  return { config: JSON.stringify(next), record }
}

// Rolls back to the `from` state of the record at `epoch`.
export function buildRollback(args: {
  feature: FeatureLike
  epoch: number
  reason: string
  actor: string
  now: string
  evals: HarnessEval[]
  expectedEpoch: number
}): ApplyResult {
  const { feature, epoch, reason, actor, now, evals, expectedEpoch } = args
  if (!reason.trim()) return { error: '롤백 사유를 입력해야 합니다.' }
  const g = parseGovernance(feature.config)
  if (headEpoch(g) !== expectedEpoch) {
    return { error: `동시 변경이 감지되었습니다(현재 에포크 ${headEpoch(g)}, 예상 ${expectedEpoch}). 목록을 새로고침한 후 다시 시도하세요.` }
  }
  const target = g.rollouts.find(r => r.epoch === epoch)
  if (!target) return { error: `에포크 ${epoch} 롤아웃 기록을 찾을 수 없습니다. 롤백에 실패했습니다.` }
  if (target.kind === 'rollback') return { error: '롤백 기록은 다시 롤백할 수 없습니다.' }
  if (feature.enabled === target.from.enabled && feature.enforced === target.from.enforced) {
    return { error: '현재 상태가 이미 롤백 대상 상태와 동일합니다.' }
  }
  const record: RolloutRecord = {
    epoch: expectedEpoch + 1,
    kind: 'rollback',
    at: now,
    by: actor,
    reason: reason.trim(),
    scope: target.scope,
    from: { enabled: feature.enabled, enforced: feature.enforced },
    to: { ...target.from },
    rollback_of: epoch,
    results: evals,
  }
  const next: Governance = { scope: g.scope, rollouts: [...g.rollouts, record] }
  return { config: JSON.stringify(next), record }
}
