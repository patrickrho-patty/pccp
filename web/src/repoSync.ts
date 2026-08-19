// repoSync.ts — canonical repository sync status shared by the list,
// detail, and explorer pages (PAT-1493). The phase is derived only from
// the repository row (sync_status / timestamps), never from the presence
// of individual evidence rows; dataset availability is attached
// explicitly by the caller so partial sync is labeled per dataset.

export type SyncPhase = 'never' | 'queued' | 'running' | 'current' | 'stale' | 'partial' | 'failed'

export interface SyncDataset {
  key: string
  label: string
  available: boolean
  reason?: string
}

export interface RepoSyncStatus {
  phase: SyncPhase
  phaseLabel: string
  badgeClass: string
  lastSuccessAt: string | null
  lastAttemptAt: string | null
  lastAttemptResult: 'success' | 'failed' | 'running' | 'queued' | null
  lastError: string | null
  sourceRevision: string | null
  datasets: SyncDataset[]
}

export const SYNC_PHASE_LABELS: Record<SyncPhase, string> = {
  never: '미동기화',
  queued: '대기 중',
  running: '동기화 중',
  current: '동기화됨',
  stale: '오래된 스냅샷',
  partial: '부분 동기화',
  failed: '실패',
}

export const SYNC_PHASE_BADGES: Record<SyncPhase, string> = {
  never: 'badge-gray',
  queued: 'badge-gray',
  running: 'badge-yellow',
  current: 'badge-green',
  stale: 'badge-yellow',
  partial: 'badge-yellow',
  failed: 'badge-red',
}

// Labels for RepoSyncStatus.lastAttemptResult. running/queued reuse the
// phase labels so the attempt wording can never drift from the phase
// wording (R7).
export const ATTEMPT_RESULT_LABELS: Record<NonNullable<RepoSyncStatus['lastAttemptResult']>, string> = {
  success: '성공',
  failed: '실패',
  running: SYNC_PHASE_LABELS.running,
  queued: SYNC_PHASE_LABELS.queued,
}

// Snapshots older than this are treated as stale even when the last sync
// succeeded — evidence may no longer match the source.
export const SYNC_STALE_AFTER_MS = 24 * 60 * 60 * 1000

interface RepoSyncRow {
  sync_status?: string
  last_sync_at?: string
  last_sync_attempt_at?: string
  last_sync_head?: string
  last_sync_error?: string
  last_commit_at?: string
}

interface ResolveOptions {
  now?: number
  staleAfterMs?: number
  datasets?: SyncDataset[]
}

const parseTime = (v?: string): number | null => {
  if (!v) return null
  const t = Date.parse(v)
  return Number.isNaN(t) ? null : t
}

export function resolveRepoSync(repo: RepoSyncRow | null | undefined, opts?: ResolveOptions): RepoSyncStatus {
  const r = repo || {}
  const status = r.sync_status || (r.last_sync_at ? 'synced' : 'never')
  const now = opts?.now ?? Date.now()
  const staleAfterMs = opts?.staleAfterMs ?? SYNC_STALE_AFTER_MS
  const datasets = opts?.datasets || []

  let phase: SyncPhase
  switch (status) {
    case 'syncing':
      phase = 'running'
      break
    case 'queued':
      phase = 'queued'
      break
    case 'failed':
      phase = 'failed'
      break
    case 'synced': {
      // Freshness hinges on the sync success timestamp only: an empty
      // repository has no commits, so last_commit_at is legitimately
      // empty right after a successful sync and must not read as stale.
      const successAt = parseTime(r.last_sync_at)
      if (successAt === null || now - successAt > staleAfterMs) {
        phase = 'stale'
      } else {
        phase = 'current'
      }
      break
    }
    default:
      phase = 'never'
  }
  // A successful-but-incomplete snapshot is partial, never current.
  if ((phase === 'current' || phase === 'stale') && datasets.some(d => !d.available)) {
    phase = 'partial'
  }

  const lastAttemptResult: RepoSyncStatus['lastAttemptResult'] =
    phase === 'running' ? 'running'
    : phase === 'queued' ? 'queued'
    : status === 'failed' ? 'failed'
    : status === 'synced' ? 'success'
    : null

  return {
    phase,
    phaseLabel: SYNC_PHASE_LABELS[phase],
    badgeClass: SYNC_PHASE_BADGES[phase],
    lastSuccessAt: r.last_sync_at || null,
    lastAttemptAt: r.last_sync_attempt_at || r.last_sync_at || null,
    lastAttemptResult,
    lastError: r.last_sync_error || null,
    sourceRevision: r.last_sync_head || null,
    datasets,
  }
}

export type SyncErrorKind =
  | 'not_synced'
  | 'in_progress'
  | 'auth'
  | 'rate_limit'
  | 'not_found'
  | 'unsupported'
  | 'path_missing'
  | 'unknown'

export interface ClassifiedSyncError {
  kind: SyncErrorKind
  label: string
}

// classifySyncError maps raw SCM/git error text (never shown directly in
// the UI) to a Korean explanation an administrator can act on.
export function classifySyncError(message?: string | null): ClassifiedSyncError {
  const m = (message || '').toLowerCase()
  if (!m) return { kind: 'unknown', label: '알 수 없는 오류가 발생했습니다.' }
  if (m.includes('already in progress') || m.includes('sync in progress')) {
    return { kind: 'in_progress', label: '동기화가 이미 진행 중입니다. 완료 후 다시 시도하세요.' }
  }
  if (m.includes('not synced yet')) {
    return { kind: 'not_synced', label: '동기화된 스냅샷이 없습니다. 동기화를 먼저 실행하세요.' }
  }
  if (m.includes('rate limit') || m.includes('429') || m.includes('too many requests')) {
    return { kind: 'rate_limit', label: 'SCM API 레이트 리밋에 걸렸습니다. 잠시 후 다시 시도하세요.' }
  }
  if (m.includes('authentication') || m.includes('credentials') || m.includes('permission denied')
    || m.includes('401') || m.includes('403') || m.includes('could not read username')) {
    return { kind: 'auth', label: 'SCM 인증에 실패했습니다. 자격 증명(PCCP_SCM_TOKEN)을 확인하세요.' }
  }
  if (m.includes('unsupported') || m.includes('not supported')) {
    return { kind: 'unsupported', label: '이 SCM 커넥터가 지원하지 않는 기능입니다.' }
  }
  if (m.includes('not found') || m.includes('404') || m.includes("couldn't find remote ref")
    || m.includes('remote branch') || m.includes('does not exist')) {
    return { kind: 'not_found', label: '저장소 또는 기본 브랜치를 찾을 수 없습니다. 삭제·이름 변경 여부를 확인하세요.' }
  }
  if (m.includes('read dir') || m.includes('no such file') || m.includes('path escapes')) {
    return { kind: 'path_missing', label: '해당 경로가 스냅샷에 없거나 접근이 제한되어 있습니다.' }
  }
  return { kind: 'unknown', label: '동기화 데이터를 사용할 수 없습니다.' }
}

// treeViewState reduces a file-browser fetch to one of the distinct UI
// states: loading, ok, empty (repo/path genuinely empty), or unavailable
// (sync snapshot missing/failed) with a classified reason.
export type TreeViewState =
  | { state: 'loading' }
  | { state: 'ok' }
  | { state: 'empty'; scope: 'repository' | 'folder' }
  | { state: 'unavailable'; error: ClassifiedSyncError }

export function treeViewState(
  loading: boolean,
  entries: unknown[] | null,
  errorMessage: string | null,
  path: string,
): TreeViewState {
  if (loading) return { state: 'loading' }
  if (errorMessage) return { state: 'unavailable', error: classifySyncError(errorMessage) }
  if (!entries || entries.length === 0) {
    return { state: 'empty', scope: path ? 'folder' : 'repository' }
  }
  return { state: 'ok' }
}
