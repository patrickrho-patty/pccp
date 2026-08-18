// Canonical session state definitions (PAT-1496) — the web mirror of
// internal/models/session_lifecycle.go. Live, Sessions, dashboard, and
// Fleet share one status vocabulary and one live predicate so no surface
// can disagree about what counts as an active session.
export type SessionStatus = 'pending' | 'active' | 'idle' | 'paused' | 'closed' | 'terminated'

export interface SessionStatusMeta {
  ko: string
  badge: string
  dot: string
}

export const SESSION_STATUS_META: Record<string, SessionStatusMeta> = {
  pending:    { ko: '대기',   badge: 'bg-gray-100 text-gray-600 border-gray-200',   dot: '⚪' },
  active:     { ko: '활성',   badge: 'bg-green-50 text-green-700 border-green-200', dot: '🟢' },
  idle:       { ko: '유휴',   badge: 'bg-yellow-50 text-yellow-700 border-yellow-200', dot: '🟡' },
  paused:     { ko: '일시정지', badge: 'bg-amber-50 text-amber-700 border-amber-200', dot: '⏸️' },
  closed:     { ko: '종료',   badge: 'bg-gray-100 text-gray-500 border-gray-200',    dot: '✅' },
  terminated: { ko: '강제종료', badge: 'bg-red-50 text-red-700 border-red-200',       dot: '🔴' },
}

// sessionStatusMeta returns the canonical meta with a safe fallback.
export function sessionStatusMeta(status: string): SessionStatusMeta {
  return SESSION_STATUS_META[status] || { ko: status || '알 수 없음', badge: 'bg-gray-100 text-gray-500 border-gray-200', dot: '⚪' }
}

// isLiveSession is THE live predicate (mirrors models.SessionIsLive):
// only an active session is live. Paused/idle sessions are in-progress
// but must never be counted or labeled as active.
export function isLiveSession(s: { status?: string }): boolean {
  return s?.status === 'active'
}

// isInProgressSession: sessions the Live view tracks in its secondary
// group — explicitly labeled, never counted as live.
export function isInProgressSession(s: { status?: string }): boolean {
  return s?.status === 'active' || s?.status === 'idle' || s?.status === 'paused'
}

// sessionLastActivity returns the session's last governed-exchange
// touch, falling back to the open time (mirrors the model contract).
export function sessionLastActivity(s: { last_activity_at?: string; opened_at?: string }): string {
  return s?.last_activity_at || s?.opened_at || ''
}

// formatSessionTime / relativeAge reuse the shared tenant formatters so
// every surface shows the same labeled KST time and relative age.
export { formatTenantTime as formatSessionTime } from './utils/format'
import { formatRelative } from './utils/format'

export function relativeAge(iso: string, now: number = Date.now()): string {
  return formatRelative(iso, now)
}

// streamFreshness classifies SSE health from the ms elapsed since the
// last received event — connection health, distinct from session health.
export function streamFreshness(msSinceLastEvent: number | null): { label: string; ok: boolean } {
  if (msSinceLastEvent === null) return { label: '수신 대기', ok: false }
  if (msSinceLastEvent < 15_000) return { label: `최신 (${Math.floor(msSinceLastEvent / 1000)}초 전 수신)`, ok: true }
  if (msSinceLastEvent < 60_000) return { label: `지연 (${Math.floor(msSinceLastEvent / 1000)}초 전 수신)`, ok: false }
  return { label: '지연 (60초+ 무수신)', ok: false }
}
