// approvalView.ts — PAT-1497: shared governed decision-queue presentation.
//
// Fleet and Tools both show pending approvals that used to render as raw
// `tool_use — pending` rows with an immediate approve/deny and no context.
// This module is the single source of the approval-presentation intent —
// Korean requested-effect summary, requester/session/harness/tool context,
// risk level, waiting age, expiry/SLA, and the exact evidence/detail route —
// so no surface decodes `approval_type` strings and the dashboard action
// center can reuse the same typed contract.
//
// The backend emits this contract (internal/api enrichApprovals); this
// module only formats it for the UI (no business inference in components).

export type ApprovalRisk = 'low' | 'medium' | 'high' | 'critical'

export interface ApprovalView {
  /** Korean requested-effect sentence, e.g. "도구 실행 승인 — refund.go (medium)". */
  title: string
  requestedBy: string
  harnessId?: string
  sessionTitle?: string
  toolName?: string
  risk: ApprovalRisk
  ageLabel: string          // e.g. "5분"
  expired: boolean
  expiresLabel?: string     // e.g. "3시간 내" or "만료됨"
  detailRoute: string
}

const RISK_KO: Record<ApprovalRisk, string> = {
  low: '낮음', medium: '중간', high: '높음', critical: '심각',
}

/** Korean label for an approval_type token (backend sends approval_type_ko too). */
export function approvalTypeKo(raw?: string): string {
  const t = (raw || '').toLowerCase()
  if (t.startsWith('tool_')) return '도구 실행'
  if (t.startsWith('file_write')) return '파일 작성'
  if (t.startsWith('model_use')) return '모델 사용'
  if (t.startsWith('network')) return '네트워크'
  return '승인'
}

export function approvalRiskKo(risk?: string): string {
  return (risk && RISK_KO[risk as ApprovalRisk]) || risk || '중간'
}

/** Human waiting-age label from seconds. */
export function approvalAgeLabel(seconds?: number): string {
  if (typeof seconds !== 'number' || Number.isNaN(seconds)) return '—'
  const s = Math.max(0, Math.floor(seconds))
  const m = Math.floor(s / 60)
  if (m < 1) return `${s}초`
  const h = Math.floor(m / 60)
  if (h < 1) return `${m}분`
  const d = Math.floor(h / 24)
  if (d < 1) return `${h}시간`
  return `${d}일`
}

/** Expiry/SLA label from the remaining seconds and expired flag. */
export function approvalExpiryLabel(remaining?: number, expired?: boolean): string | undefined {
  if (expired) return '만료됨'
  if (typeof remaining !== 'number' || Number.isNaN(remaining)) return undefined
  const r = Math.floor(remaining)
  if (r <= 0) return '만료 임박'
  if (r < 3600) return `${Math.floor(r / 60)}분 내`
  if (r < 86400) return `${Math.floor(r / 3600)}시간 내`
  return `${Math.floor(r / 86400)}일 내`
}

/** Full governed-decision view for one enriched row (backend contract). */
export function approvalView(a: Record<string, any>): ApprovalView {
  const typeKo = (a.approval_type_ko as string) || approvalTypeKo(a.approval_type)
  const tool = a.tool_name ? ` — ${a.tool_name}` : ''
  const risk = (a.risk || 'medium') as ApprovalRisk
  return {
    title: `${typeKo}${tool} (${approvalRiskKo(risk)})`,
    requestedBy: a.requested_by_name || a.requested_by || '—',
    harnessId: a.harness_id,
    sessionTitle: a.session_title,
    toolName: a.tool_name,
    risk,
    ageLabel: approvalAgeLabel(a.waiting_age_seconds),
    expired: !!a.expired,
    expiresLabel: approvalExpiryLabel(a.remaining_seconds, a.expired),
    detailRoute: a.detail_route || '/tools?tab=approvals',
  }
}

/** Sort/rank rule (PAT-1497): most urgent first — expired, then highest risk,
 *  then oldest (longest waiting). Documented and shared. */
export function rankApprovals(rows: Record<string, any>[]): Record<string, any>[] {
  const riskRank: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 }
  return [...rows].sort((a, b) => {
    const aExp = a.expired ? 0 : 1
    const bExp = b.expired ? 0 : 1
    if (aExp !== bExp) return aExp - bExp
    const ar = riskRank[a.risk || 'medium'] ?? 2
    const br = riskRank[b.risk || 'medium'] ?? 2
    if (ar !== br) return ar - br
    return (a.waiting_age_seconds ?? 0) - (b.waiting_age_seconds ?? 0) < 0 ? 1 : -1
  })
}
