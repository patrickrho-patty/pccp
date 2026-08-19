// toolGovernance.ts — PAT-1509: 승인 게이트·프로젝트 허용 목록 변경의
// 초안 diff / 영향 미리보기를 계산하는 순수 로직. Tools.tsx가 사용하며
// 서버의 런타임 강제 의미론(internal/tools/service.go)과 일치시킨다.

export interface ToolRef {
  name: string
  danger_level?: string
  requires_approval?: boolean
  status?: string
}

export const HIGH_RISK_LEVELS = ['high', 'critical']

export interface AllowlistDiff {
  added: string[]
  removed: string[]
  kept: string[]
}

export function diffAllowlist(current: string[], proposed: string[]): AllowlistDiff {
  const cur = new Set(current)
  const next = new Set(proposed)
  return {
    added: [...next].filter(n => !cur.has(n)).sort(),
    removed: [...cur].filter(n => !next.has(n)).sort(),
    kept: [...next].filter(n => cur.has(n)).sort(),
  }
}

export interface AllowlistImpact {
  diff: AllowlistDiff
  hasChanges: boolean
  addedHighRisk: string[]   // 새로 허용되는 high/critical 도구 — 보호 약화
  removedGated: string[]    // 목록에서 빠지는 승인 게이트 도구 (차단 강화, 표시용)
  unknown: string[]         // 레지스트리에 없는(미등록/폐기) 이름
  becomesUnset: boolean     // 목록을 비워 "전체 허용"으로 되돌리는 변경 — 보호 약화
  weakening: boolean        // 보호가 약화되는 변경이면 확인 체크 필수
}

export function assessAllowlistImpact(current: string[], proposed: string[], tools: ToolRef[]): AllowlistImpact {
  const diff = diffAllowlist(current, proposed)
  const byName = new Map(tools.map(t => [t.name, t]))
  const addedHighRisk = diff.added.filter(n => HIGH_RISK_LEVELS.includes(byName.get(n)?.danger_level || ''))
  const removedGated = diff.removed.filter(n => !!byName.get(n)?.requires_approval)
  const unknown = [...diff.added, ...diff.removed, ...diff.kept].filter(n => !byName.has(n))
  const becomesUnset = current.length > 0 && proposed.length === 0
  const weakening = addedHighRisk.length > 0 || becomesUnset
  return {
    diff,
    hasChanges: diff.added.length > 0 || diff.removed.length > 0,
    addedHighRisk, removedGated, unknown, becomesUnset, weakening,
  }
}

export interface EffectiveAllowlist {
  mode: 'unset' | 'restricted'
  label: string       // 배너에 표시할 한글 설명
  allowed: string[]   // 유효 허용 도구 (미설정이면 활성 등록 도구 전체)
  unknown: string[]   // 목록에는 있으나 레지스트리에 없는 이름
}

// effectiveAllowlist는 저장된 로컬 목록으로 유효 정책을 계산한다.
// 서버 의미론: 행이 0개면 미설정(모든 등록 도구 허용), 1개 이상이면 나열된 도구만 허용.
export function effectiveAllowlist(savedNames: string[], tools: ToolRef[]): EffectiveAllowlist {
  const byName = new Map(tools.map(t => [t.name, t]))
  const unknown = savedNames.filter(n => !byName.has(n))
  if (savedNames.length === 0) {
    const active = tools.filter(t => (t.status ?? 'active') === 'active').map(t => t.name).sort()
    return {
      mode: 'unset',
      label: `허용 목록 미설정 — 등록된 활성 도구 ${active.length}개 모두 호출 가능`,
      allowed: active,
      unknown,
    }
  }
  return {
    mode: 'restricted',
    label: `허용 목록 설정됨 — ${savedNames.length}개 도구만 호출 가능`,
    allowed: [...savedNames].sort(),
    unknown,
  }
}

export function summarizeRisk(names: string[], tools: ToolRef[]): Record<string, number> {
  const byName = new Map(tools.map(t => [t.name, t]))
  const out: Record<string, number> = {}
  for (const n of names) {
    const level = byName.get(n)?.danger_level || 'unknown'
    out[level] = (out[level] || 0) + 1
  }
  return out
}

export interface GateChange {
  from: boolean
  to: boolean
  weakening: boolean // 게이트 해제(true→false) — 사유 필수
  highRisk: boolean  // high/critical 도구의 게이트 해제 — 확인 체크 추가 필수
}

export function assessGateChange(tool: ToolRef, next: boolean): GateChange {
  const weakening = !!tool.requires_approval && !next
  const highRisk = weakening && HIGH_RISK_LEVELS.includes(tool.danger_level || '')
  return { from: !!tool.requires_approval, to: next, weakening, highRisk }
}

// isStaleBase: 확인 시점에 재조회한 목록이 diff의 기준 스냅샷과 다르면
// 다른 관리자가 동시에 변경한 것이므로 저장을 중단한다.
export function isStaleBase(base: string[], latest: string[]): boolean {
  if (base.length !== latest.length) return true
  const a = [...base].sort()
  const b = [...latest].sort()
  return a.some((v, i) => v !== b[i])
}
