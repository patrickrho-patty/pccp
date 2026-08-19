// Canonical sandbox lifecycle definitions (PAT-1513) — the web mirror of
// internal/sandbox/lifecycle.go. List and detail surfaces derive every
// action button from sandboxActions so no UI can offer an action the
// service would reject, and every disabled control carries its reason.

export type SandboxStatus = 'pending' | 'defined' | 'running' | 'paused' | 'destroyed' | 'failed'

export interface SandboxStatusMeta {
  ko: string
  badge: string
}

export const SANDBOX_STATUS_META: Record<string, SandboxStatusMeta> = {
  pending:   { ko: '대기',                  badge: 'bg-gray-100 text-gray-500 border-gray-200' },
  defined:   { ko: '정의됨 (런타임 미연결)', badge: 'bg-yellow-50 text-yellow-700 border-yellow-200' },
  running:   { ko: '실행 중',               badge: 'bg-green-50 text-green-700 border-green-200' },
  paused:    { ko: '일시정지',              badge: 'bg-amber-50 text-amber-700 border-amber-200' },
  destroyed: { ko: '파괴됨',                badge: 'bg-gray-100 text-gray-400 border-gray-200' },
  failed:    { ko: '실패',                  badge: 'bg-red-50 text-red-700 border-red-200' },
}

// sandboxStatusMeta returns the canonical meta with a safe fallback.
export function sandboxStatusMeta(status: string): SandboxStatusMeta {
  return SANDBOX_STATUS_META[status] || { ko: status || '알 수 없음', badge: 'bg-gray-100 text-gray-500 border-gray-200' }
}

export type SandboxAction = 'snapshot' | 'destroy' | 'retry'

export interface SandboxActionInfo {
  id: SandboxAction
  ko: string
  enabled: boolean
  // reason explains a disabled action to the operator.
  reason?: string
  danger?: boolean
}

// ACTION_TABLE mirrors validActions in internal/sandbox/lifecycle.go:
// snapshot captures live runtime state (running/paused only), destroy is
// cleanup admitted in every non-terminal state, retry re-attempts
// provisioning after a runtime-disconnected (defined) or failed outcome.
const ACTION_TABLE: Record<string, SandboxAction[]> = {
  pending:   ['destroy'],
  defined:   ['destroy', 'retry'],
  running:   ['snapshot', 'destroy'],
  paused:    ['snapshot', 'destroy'],
  destroyed: [],
  failed:    ['destroy', 'retry'],
}

const ACTION_LABEL: Record<SandboxAction, string> = {
  snapshot: '스냅샷',
  destroy: '파괴',
  retry: '프로비저닝 재시도',
}

const DISABLED_REASON: Record<SandboxAction, string> = {
  snapshot: '실행 중(또는 일시정지) 상태에서만 스냅샷을 기록할 수 있습니다 — 캡처할 런타임 상태가 없습니다',
  destroy: '이미 파괴된 샌드박스입니다',
  retry: '런타임 미연결(정의됨) 또는 실패 상태에서만 재시도할 수 있습니다',
}

// sandboxActions derives the full action set for a sandbox: valid actions
// are enabled, the rest are disabled with the operator-facing reason.
// Unknown states fail closed (everything disabled), matching the server.
export function sandboxActions(sb: { status?: string }): SandboxActionInfo[] {
  const valid = ACTION_TABLE[sb?.status || ''] || []
  return (['snapshot', 'retry', 'destroy'] as SandboxAction[]).map(id => ({
    id,
    ko: ACTION_LABEL[id],
    enabled: valid.includes(id),
    reason: valid.includes(id) ? undefined : DISABLED_REASON[id],
    danger: id === 'destroy',
  }))
}

// sandboxRuntimeConnected reports whether the recorded provisioning
// outcome names a real runtime — "none (...)" means definition-only.
export function sandboxRuntimeConnected(sb: { runtime_provider?: string }): boolean {
  const p = sb?.runtime_provider || ''
  return p !== '' && !p.startsWith('none')
}
