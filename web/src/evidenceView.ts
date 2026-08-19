// evidenceView.ts — PAT-1498: shared evidence presentation/route registry.
//
// Session investigation, Audit, and the dashboard activity feed (PAT-1485,
// PAT-1503) all render evidence records. This module is the SINGLE source of
// the presentation intent — Korean summary title, action verb, target label,
// outcome/severity semantics, icon/color token, and the exact destination
// route — so no page hand-rolls raw `action_type`/`event_type` strings.
//
// Every renderer returns an EvidenceView{ title (Korean sentence), actor,
// target, outcome, meta (icon/color), route, rawHints } so a page can show a
// compact Korean summary first and an expandable raw/technical area second.

export interface EvidenceView {
  /** Korean admin sentence, e.g. "사용자가 결제 서비스 저장소에서 file_write 액션을 수행했다 (허용)." */
  title: string
  actor?: string
  target?: string
  outcome?: string
  severity?: string
  icon: string
  color: string // tailwind token (secondary; icon+text primary)
  /** exact drill-down route, or '' when not navigable */
  route?: string
  /** short reason/verdict for the decision surface */
  reason?: string
}

export type OutcomeKind = 'success' | 'warning' | 'danger' | 'info' | 'unknown'

const OUTCOME_META: Record<OutcomeKind, { icon: string; color: string }> = {
  success: { icon: '🟢', color: 'bg-green-50 text-green-700 border-green-200' },
  warning: { icon: '🟡', color: 'bg-yellow-50 text-yellow-700 border-yellow-200' },
  danger:  { icon: '🔴', color: 'bg-red-50 text-red-700 border-red-200' },
  info:    { icon: '🔵', color: 'bg-blue-50 text-blue-700 border-blue-200' },
  unknown: { icon: '⚪', color: 'bg-gray-100 text-gray-500 border-gray-200' },
}

export function outcomeMeta(kind: OutcomeKind) { return OUTCOME_META[kind] }

/** Human-readable action verbs for the canonical action_type set (Korean). */
const ACTION_VERB_KO: Record<string, string> = {
  ai_inference: 'AI 추론을 수행', file_write: '파일을 작성', tool_use: '도구를 실행',
  execute: '명령을 실행', read: '파일을 읽음', policy_check: '정책을 검사',
  exchange_open: '교환을 시작', exchange_close: '교환을 종료', session_open: '세션을 시작',
  session_close: '세션을 종료', harness_enroll: '하네스를 등록', harness_revoke: '하네스 자격을 해제',
  change_approve: '변경을 승인', change_reject: '변경을 거부',
}

function actionVerb(type: string): string {
  return ACTION_VERB_KO[type] || type.replace(/_/g, ' ')
}

const ATTR_LABEL_KO: Record<string, string> = {
  AI_GENERATED: 'AI 생성', AI_THEN_HUMAN_EDITED: 'AI 이후 사람 수정',
  HUMAN_THEN_AI_ASSISTED: '사람 이후 AI 보조', HUMAN_WRITTEN: '사람 작성',
}

/** Session timeline action (ActionEnvelope). */
export function sessionActionView(a: Record<string, any>): EvidenceView {
  const type = a.action_type || a.type || a.kind || 'action'
  const outcome: OutcomeKind =
    a.verdict_result === 'allowed' || a.verdict_result === 'approved' || a.verdict_result === 'success' ? 'success'
    : a.verdict_result === 'denied' || a.verdict_result === 'rejected' || a.verdict_result === 'blocked' ? 'danger'
    : a.verdict_result === 'pending' || a.verdict_result === 'warning' ? 'warning' : 'info'
  const m = outcomeMeta(outcome)
  const actor = a.user_id ? short(a.user_id) : a.harness_id ? short(a.harness_id) : undefined
  const targetDesc = a.repository_id ? `저장소 ${short(a.repository_id)}` : a.file_path ? a.file_path : undefined
  const verdictKo = a.verdict_result ? `(${verdictLabelKo(a.verdict_result)})` : ''
  return {
    title: `${actor ? actor + ' ' : ''}${actionVerb(type)} ${targetDesc ? targetDesc + ' ' : ''}${verdictKo}`.trim(),
    actor, target: targetDesc, outcome: verdictLabelKo(a.verdict_result),
    icon: m.icon, color: m.color,
    route: a.exchange_id ? `/sessions/${a.session_id}/provenance` : undefined,
    reason: typeof a.description === 'string' ? a.description : undefined,
  }
}

/** ChangeSet. */
export function changeSetView(c: Record<string, any>): EvidenceView {
  const attr = ATTR_LABEL_KO[c.attribution_state] || c.attribution_state || '미분류'
  const files = parseList(c.files_changed)
  const diff = c.lines_added || c.lines_removed
    ? `+${c.lines_added ?? 0} / -${c.lines_removed ?? 0}`
    : undefined
  const target = (files[0] ? files[0] : '') + (files.length > 1 ? ` 외 ${files.length - 1}개` : '')
  return {
    title: [`변경셋`, c.summary || c.message || ''].filter(Boolean).join(' · '),
    target: target || undefined,
    outcome: diff || attr,
    icon: '📝', color: 'bg-orange-50 text-orange-700 border-orange-200',
    route: c.id ? undefined : undefined,
  }
}

/** Security finding. */
export function findingView(f: Record<string, any>): EvidenceView {
  const sev = (f.severity || 'low').toLowerCase()
  const danger = sev === 'critical' || sev === 'high'
  const m = danger ? outcomeMeta('danger') : sev === 'medium' ? outcomeMeta('warning') : outcomeMeta('info')
  return {
    title: f.title_ko || f.title || f.finding_type || '보안 발견',
    target: f.finding_type, outcome: f.status, severity: f.severity,
    icon: m.icon, color: m.color,
    route: f.id ? `/findings/${f.id}` : undefined,
  }
}

/** Policy decision (per-exchange verdict). */
export function decisionView(d: Record<string, any>): EvidenceView {
  const outcome: OutcomeKind =
    d.verdict === 'allowed' ? 'success' : d.verdict === 'denied' ? 'danger' : 'warning'
  const m = outcomeMeta(outcome)
  return {
    title: `AI 추론 요청이 ${d.verdict === 'allowed' ? '허용' : d.verdict === 'denied' ? '거부' : (d.verdict || '미기록')}되었습니다`,
    actor: d.model_package_id ? `모델 ${short(d.model_package_id)}` : undefined,
    outcome: d.verdict, reason: d.policy_epoch_id ? `epoch ${short(d.policy_epoch_id)}` : undefined,
    icon: m.icon, color: m.color,
    route: d.exchange_id ? undefined : undefined,
  }
}

/** Replay timeline event. */
export function replayEventView(ev: Record<string, any>): EvidenceView {
  const kind = ev.kind || 'event'
  const id = ev.payload?.id || ev.id
  return {
    title: kind === 'action' ? '액션' : kind === 'change_set' ? '변경셋' : kind === 'exchange' ? '교환' : kind,
    target: typeof id === 'string' ? short(id) : undefined,
    icon: '🕐', color: 'bg-gray-100 text-gray-600 border-gray-200',
  }
}

function verdictLabelKo(v: string): string {
  if (!v) return '미기록'
  return { allowed: '허용', denied: '거부', approved: '승인', rejected: '거부', blocked: '차단', success: '성공', pending: '대기', warning: '경고' }[v] || v
}

function short(id: string): string { return id && id.length > 14 ? id.slice(0, 8) + '…' + id.slice(-4) : (id || '—') }

function parseList(v: unknown): string[] {
  if (Array.isArray(v)) return v.map(String)
  if (typeof v === 'string') { try { const p = JSON.parse(v); if (Array.isArray(p)) return p.map(String) } catch { /* not json */ } }
  return []
}

/** Drill-down route for a session's exchange/decision evidence. */
export function sessionEvidenceRoute(sessionId?: string, kind?: string): string | undefined {
  if (!sessionId) return undefined
  if (kind === 'provenance') return `/sessions/${sessionId}/provenance`
  return `/sessions/${sessionId}`
}
