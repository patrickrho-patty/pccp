// harnessHealth.ts — PAT-1492: unified, explainable harness health.
//
// A harness's trustworthiness cannot be inferred from any single raw column
// (status, risk_state, last_heartbeat, license_state, ...). This module is
// the SINGLE shared derivation used by fleet list, harness detail, dashboard,
// and secondary surfaces. It separates independent canonical dimensions and
// reduces them to one overall health state whose derivation is documented and
// exposed as evidence (threshold, observed time, responsible signal, and the
// investigation/action path) — so a green indicator can never sit next to an
// expired signal unless it is explicitly a different dimension.
//
// Raw/internal enum values are never rendered; every dimension carries a
// governed Korean label + icon + color. Color is always secondary to label+icon.

export const HEARTBEAT_STALE_MS = 10 * 60 * 1000 // matches server harnessStaleAfter
export const ATTESTATION_STALE_MS = 24 * 60 * 60 * 1000 // 24h freshness window

// Shared online predicate — the single source of truth for "is this harness
// reachable now", used by the fleet surfaces and enterprise feature
// governance. Aligned with the heartbeat dimension below: a harness is
// online only when it is active AND a heartbeat was observed within the
// staleness window. Missing heartbeat = not online.
export function isHarnessOnlineNow(h: { status?: string; last_heartbeat?: string }, now: number = Date.now()): boolean {
  if (h.status !== 'active') return false
  if (!h.last_heartbeat) return false
  const t = Date.parse(h.last_heartbeat)
  if (Number.isNaN(t)) return false
  return now - t <= HEARTBEAT_STALE_MS
}

export interface HarnessHealthFacts {
  status: string            // pending, enrolled, active, quarantined, revoked
  risk_state: string        // normal, low, elevated, high, critical
  last_heartbeat?: string    // RFC3339
  last_attestation?: string  // RFC3339
  license_state?: string
  enrollment_mode?: string
  binary_version?: string
  build_channel?: string
  stale?: boolean             // server-computed heartbeat staleness (optional)
  version_blocked?: boolean   // server-computed version-floor breach (optional)
  revoked?: boolean           // credential revoked (optional)
  at?: number                 // evaluation time (ms), default Date.now()
}

export type HealthLevel = 'healthy' | 'attention' | 'warning' | 'critical' | 'unknown'

// A dimension is one independent axis of health. It always explains itself.
export interface HealthDimension {
  key: string                 // canonical dimension id
  label: string               // Korean title
  state: HealthLevel
  icon: string
  color: string               // tailwind badge/color token (secondary signal)
  reason: string              // Korean why (threshold + observed)
  observed?: string           // observed timestamp RFC3339
  threshold?: string          // Korean threshold description
  actionPath?: string         // where to investigate / act
}

export interface HarnessHealth {
  overall: HealthLevel
  overallLabel: string
  dimensions: HealthDimension[]
  /** brief Korean summary used in compact/dense list rows */
  summary: string
}

// Severity ordering — the highest-affecting dimension wins the overall state.
const LEVEL_RANK: Record<HealthLevel, number> = {
  critical: 4,
  warning: 3,
  attention: 2,
  healthy: 1,
  unknown: 0,
}

const stateMeta: Record<HealthLevel, { label: string; icon: string; color: string }> = {
  healthy:   { label: '정상',     icon: '🟢', color: 'badge-green' },
  attention: { label: '주의',     icon: '🟡', color: 'badge-yellow' },
  warning:   { label: '경고',     icon: '🟠', color: 'badge-orange' },
  critical:  { label: '심각',     icon: '🔴', color: 'badge-red' },
  unknown:   { label: '확인 필요', icon: '⚪', color: 'badge-gray' },
}

export function healthMeta(level: HealthLevel) { return stateMeta[level] }

const STATUS_LABEL_KO: Record<string, string> = {
  pending: '대기', enrolled: '등록됨', active: '활성', quarantined: '격리', revoked: '폐기',
}
const RISK_LABEL_KO: Record<string, string> = {
  normal: '정상', low: '낮음', elevated: '주의', high: '높음', critical: '심각',
}

export function deriveHarnessHealth(f: HarnessHealthFacts): HarnessHealth {
  const now = f.at ?? Date.now()
  const dims: HealthDimension[] = []

  const addDim = (d: HealthDimension) => dims.push(d)

  // 1) Lifecycle / enrollment standing (authoritative gate).
  const status = f.status || 'unknown'
  const lifecycleLevel: HealthLevel =
    status === 'revoked' ? 'critical'
    : status === 'quarantined' ? 'warning'
    : status === 'active' || status === 'enrolled' ? 'healthy'
    : 'unknown'
  addDim({
    key: 'lifecycle', label: '등록 상태', state: lifecycleLevel,
    icon: stateMeta[lifecycleLevel].icon, color: stateMeta[lifecycleLevel].color,
    reason: STATUS_LABEL_KO[status] ? `현재 상태: ${STATUS_LABEL_KO[status]}` : `알 수 없는 상태: ${status}`,
    actionPath: status === 'quarantined' || status === 'revoked' ? '/fleet' : undefined,
  })

  // 2) Connectivity / heartbeat (independent of lifecycle).
  const hb = f.last_heartbeat || ''
  const hbTime = hb ? Date.parse(hb) : NaN
  const stale = f.stale ?? (f.status === 'enrolled' || f.status === 'active' ? (hb ? (now - hbTime > HEARTBEAT_STALE_MS) : true) : false)
  if (f.status === 'revoked') {
    addDim({ key: 'heartbeat', label: '연결' , state: 'unknown', icon: '⚪', color: 'badge-gray',
      reason: '폐기된 하네스 — 연결 상태 무관', observed: hb || undefined, threshold: '하트비트 창 ' + HEARTBEAT_STALE_MS / 60000 + '분' })
  } else if (hb && Number.isFinite(hbTime)) {
    const level: HealthLevel = stale ? 'warning' : 'healthy'
    const ago = Math.max(0, Math.round((now - hbTime) / 60000))
    addDim({ key: 'heartbeat', label: '연결', state: level, icon: stateMeta[level].icon, color: stateMeta[level].color,
      reason: stale ? `하트비트 만료 (마지막 ${ago}분 전, 창 ${HEARTBEAT_STALE_MS / 60000}분)` : `정상 연결 (마지막 ${ago}분 전)`,
      observed: hb, threshold: `창 ${HEARTBEAT_STALE_MS / 60000}분`, actionPath: stale ? '/fleet' : undefined })
  } else {
    addDim({ key: 'heartbeat', label: '연결', state: f.status === 'enrolled' || f.status === 'active' ? 'attention' : 'unknown',
      icon: '⚪', color: 'badge-gray',
      reason: f.status === 'enrolled' || f.status === 'active' ? '하트비트 기록 없음 — 연결 확인 필요' : '연결 정보 없음',
      threshold: `창 ${HEARTBEAT_STALE_MS / 60000}분` })
  }

  // 3) Risk posture.
  const risk = f.risk_state || 'normal'
  const riskLevel: HealthLevel = risk === 'critical' || risk === 'high' ? 'critical' : risk === 'elevated' ? 'warning' : 'healthy'
  addDim({ key: 'risk', label: '위험 상태', state: riskLevel, icon: stateMeta[riskLevel].icon, color: stateMeta[riskLevel].color,
    reason: RISK_LABEL_KO[risk] ? `위험 등급: ${RISK_LABEL_KO[risk]}` : `위험 등급: ${risk}`,
    actionPath: riskLevel === 'critical' ? '/fleet' : undefined })

  // 4) Attestation freshness.
  const at = f.last_attestation || ''
  const atTime = at ? Date.parse(at) : NaN
  if (at && Number.isFinite(atTime)) {
    const fresh = now - atTime <= ATTESTATION_STALE_MS
    const hrs = Math.max(0, Math.round((now - atTime) / 3600000))
    addDim({ key: 'attestation', label: '증명 신선도', state: fresh ? 'healthy' : 'attention', icon: fresh ? '🟢' : '🟡', color: fresh ? 'badge-green' : 'badge-yellow',
      reason: fresh ? `증명 ${hrs}시간 전 · 정상` : `증명 ${hrs}시간 전 · 창 24시간 초과`, observed: at, threshold: '창 24시간' })
  } else {
    addDim({ key: 'attestation', label: '증명 신선도', state: 'unknown', icon: '⚪', color: 'badge-gray',
      reason: '증명 기록 없음 — 대기/오류일 수 있음', threshold: '창 24시간' })
  }

  // 5) Version compliance.
  const vb = f.version_blocked ?? false
  addDim({ key: 'version', label: '버전 준수', state: vb ? 'critical' : 'healthy', icon: vb ? '🔴' : '🟢', color: vb ? 'badge-red' : 'badge-green',
    reason: vb ? `버전 ${f.binary_version || '-'}이(가) 하한 미만` : f.binary_version ? `v${f.binary_version} · 준수` : '버전 정보 없음 · 준수로 간주',
    actionPath: vb ? '/fleet' : undefined })

  // Overall = worst dimension; tie-break by lifecycle then heartbeat.
  let overall: HealthLevel = 'healthy'
  let worst: HealthDimension | null = null
  for (const d of dims) {
    if (LEVEL_RANK[d.state] > LEVEL_RANK[overall]) { overall = d.state; worst = d }
  }
  // Ensure a credentialed-but-revoked harness surfaces as critical even if
  // risk is normal (lifecycle already covers revoked → critical).
  return {
    overall,
    overallLabel: stateMeta[overall].label,
    dimensions: dims,
    summary: worst ? `${stateMeta[overall].icon} ${stateMeta[overall].label} · ${worst.reason}` : `${stateMeta[overall].icon} 정상`,
  }
}

// Kotlin-friendly label helper for secondary surfaces (compact row).
export function statusLabelKo(status: string): string { return STATUS_LABEL_KO[status] || status }
export function riskLabelKo(risk: string): string { return RISK_LABEL_KO[risk] || risk }
export function levelOf(level: HealthLevel) { return level }
