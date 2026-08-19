// PAT-1510: broadcast audience resolution, reachability, rendering and
// send-gate logic — pure module split from Communications.tsx so the
// zero/large/changing/offline/locale states are node:test-coverable.

export interface AudienceUser {
  id: string
  name?: string
  name_ko?: string
  email?: string
  status?: string
  locale?: string
}

export type BroadcastScopeType = '' | 'org' | 'project' | 'user'

export interface BroadcastScope {
  type: BroadcastScopeType
  targetId?: string
}

export interface AudienceExclusion {
  user: AudienceUser
  reason: 'suspended' | 'offboarded'
}

export interface AudiencePreview {
  eligible: AudienceUser[]
  excluded: AudienceExclusion[]
}

// resolveAudiencePreview maps a scope to eligible/excluded recipients.
// memberIds scopes project audiences (null = not project-scoped).
// Duplicate IDs collapse to a single recipient; suspended/offboarded
// users are excluded with a reason. No scope selected → empty preview.
export function resolveAudiencePreview(
  users: AudienceUser[],
  scope: BroadcastScope,
  memberIds: ReadonlySet<string> | null = null,
): AudiencePreview {
  let candidates: AudienceUser[] = []
  if (scope.type === 'org') candidates = users
  else if (scope.type === 'project') candidates = users.filter(u => memberIds?.has(u.id))
  else if (scope.type === 'user') candidates = users.filter(u => u.id === scope.targetId)
  const seen = new Set<string>()
  const preview: AudiencePreview = { eligible: [], excluded: [] }
  for (const u of candidates) {
    if (!u.id || seen.has(u.id)) continue
    seen.add(u.id)
    if (u.status === 'suspended' || u.status === 'offboarded') {
      preview.excluded.push({ user: u, reason: u.status })
    } else {
      preview.eligible.push(u)
    }
  }
  return preview
}

export interface ReachableRecipient extends AudienceUser {
  reachable: boolean
}

export interface Reachability {
  rows: ReachableRecipient[]
  online: number
  offline: number
}

// mergeReachability marks eligible recipients reachable when presence
// reports them online/away/busy; missing presence means offline harness.
export function mergeReachability(
  eligible: AudienceUser[],
  presence: Array<{ user_id: string; status?: string }>,
): Reachability {
  const statusBy = new Map(presence.map(p => [p.user_id, p.status || '']))
  const rows = eligible.map(u => ({
    ...u,
    reachable: ['online', 'away', 'busy'].includes(statusBy.get(u.id) || ''),
  }))
  const online = rows.filter(r => r.reachable).length
  return { rows, online, offline: rows.length - online }
}

// renderBroadcastText resolves the exact text a recipient sees with
// locale fallback: ko locales prefer *_ko fields, others prefer English,
// each falling back to the other when its side is missing.
export function renderBroadcastText(
  bc: { title?: string; title_ko?: string; body?: string; body_ko?: string },
  locale = 'ko-KR',
): { title: string; body: string } {
  const ko = (locale || '').startsWith('ko')
  return {
    title: ko ? (bc.title_ko || bc.title || '') : (bc.title || bc.title_ko || ''),
    body: ko ? (bc.body_ko || bc.body || '') : (bc.body || bc.body_ko || ''),
  }
}

// audienceSizeOf parses a broadcast's frozen audience snapshot and returns
// the eligible recipient count; null when the snapshot is absent (legacy
// broadcasts) or malformed. Call once when loading, not per render.
export function audienceSizeOf(broadcast: { audience?: string }): number | null {
  if (!broadcast.audience) return null
  try {
    const snap = JSON.parse(broadcast.audience)
    return Array.isArray(snap?.eligible_ids) ? snap.eligible_ids.length : null
  } catch {
    return null
  }
}

// exclusionReasonKo renders the inline Korean label for an exclusion reason.
export function exclusionReasonKo(reason: string): string {
  return reason === 'suspended' ? '정지' : '오프보딩'
}

// LARGE_AUDIENCE_THRESHOLD: sends above this need explicit confirmation.
export const LARGE_AUDIENCE_THRESHOLD = 100

export interface SendGateInput {
  title: string
  scope: BroadcastScope
  eligibleCount: number
  severity: string
  confirmReason: string
  allowEmpty: boolean
  confirmLarge: boolean
}

// broadcastSendBlockers lists every reason (Korean, user-visible) the
// send button must stay disabled. Empty array = send is allowed.
export function broadcastSendBlockers(input: SendGateInput): string[] {
  const blockers: string[] = []
  if (!input.title.trim()) blockers.push('제목이 필요합니다')
  if (!input.scope.type) blockers.push('수신 대상 범위를 선택하세요')
  else if (input.scope.type !== 'org' && !input.scope.targetId) blockers.push('대상을 선택하세요')
  if (input.eligibleCount === 0 && !input.allowEmpty) {
    blockers.push('수신 대상이 0명입니다 — 0명 전송을 명시적으로 확인해야 합니다')
  }
  if (input.eligibleCount > LARGE_AUDIENCE_THRESHOLD && !input.confirmLarge) {
    blockers.push(`대규모 대상(${input.eligibleCount}명)입니다 — 대규모 전송을 확인해야 합니다`)
  }
  if ((input.severity === 'critical' || input.severity === 'emergency') && !input.confirmReason.trim()) {
    blockers.push('심각/긴급 방송은 전송 사유가 필요합니다')
  }
  return blockers
}
