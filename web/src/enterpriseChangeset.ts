// Enterprise feature changeset — validation, preview, and the versioned
// rollout/rollback recorder. Holds the business rules for what a change
// can do; calls governanceTrace for state mirror and enterpriseCatalog
// for the immutable catalog reference.

import {
  ADMIN_ROLES,
  FEATURE_CATALOG,
  PATTY_ROLES,
  type CatalogEntry,
  type FeatureLike,
  type Governance,
  type HarnessEval,
  type HarnessInfo,
  type RolloutRecord,
  type Scope,
} from './enterpriseCatalog.ts'
import { headEpoch, parseGovernance, evaluateHarnesses } from './governanceTrace.ts'

// Bound the in-config rollout history so the JSON column doesn't grow
// unboundedly. A future backend table (enterprise_feature_rollouts) is
// tracked separately; until then we cap and drop the oldest entries.
export const MAX_ROLLOUTS_PER_FEATURE = 50

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
  evals?: HarnessEval[] // precomputed so callers can share one evaluation with buildPreview
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

export interface ApplyResult {
  config?: string
  record?: RolloutRecord
  error?: string
}

// Returns a new governance record list capped at MAX_ROLLOUTS_PER_FEATURE
// (oldest dropped). Caller is responsible for persisting the new config.
function appendCappedRollout(prev: Governance, record: RolloutRecord): Governance {
  const next = [...prev.rollouts, record]
  if (next.length <= MAX_ROLLOUTS_PER_FEATURE) return { scope: prev.scope, rollouts: next }
  return { scope: prev.scope, rollouts: next.slice(next.length - MAX_ROLLOUTS_PER_FEATURE) }
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
  const next = appendCappedRollout(g, record)
  return { config: JSON.stringify({ scope, rollouts: next.rollouts }), record }
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
  const next = appendCappedRollout(g, record)
  return { config: JSON.stringify({ scope: g.scope, rollouts: next.rollouts }), record }
}