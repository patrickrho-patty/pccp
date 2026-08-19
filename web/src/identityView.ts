// identityView.ts — PAT-1512: one communication identity/message
// presentation contract.
//
// Communications surfaces used to render raw user IDs (`usr_demo_park`) as
// author names and presence rows, with edit/delete on every message and
// unexplainable read counts. This module is the single source of identity +
// message presentation: it resolves an actor/user/harness/session to a Korean
// label and exact authorized route (with tombstone fallback for
// deleted/inaccessible records), and it decides author-owned vs authorized
// moderation for edit/delete. Pages must not hand-roll raw-ID interpretation.
//
// Lives with the other shared presentation modules (evidenceView,
// approvalView, complianceView, securityLexicon) so chat, broadcasts, files,
// presence, and receipts all read the same.

export interface IdentityView {
  label: string       // Korean name or fallback (never the bare raw ID alone)
  raw: string         // original ID, preserved for technical/evidence use
  role?: string       // resolved role or service identity
  route?: string      // exact authorized destination ('' when not navigable)
  tombstone: boolean  // deleted/inaccessible → stable tombstone label
  kind: 'user' | 'harness' | 'system' | 'service' | 'unknown'
}

export interface IdentityContext {
  usersById: Record<string, any>
  harnessesById: Record<string, any>
}

/** Build a lookup context from API row arrays (users, harnesses). */
export function buildIdentityContext(users: any[] = [], harnesses: any[] = []): IdentityContext {
  const usersById: Record<string, any> = {}
  for (const u of users || []) usersById[u.id] = u
  const harnessesById: Record<string, any> = {}
  for (const h of harnesses || []) harnessesById[h.harness_id] = h
  return { usersById, harnessesById }
}

/** Korean display name for a user row (name_ko > name > email). */
export function userLabel(u?: any): string {
  return (u?.name_ko || u?.name || '').trim() || u?.email || ''
}

/** Resolve a user ID → IdentityView with exact route + tombstone fallback. */
export function resolveUser(id: string, ctx: IdentityContext, opts: { role?: string } = {}): IdentityView {
  if (!id) return { label: '알 수 없는 사용자', raw: '', kind: 'unknown', tombstone: true, role: opts.role }
  const u = ctx.usersById[id]
  if (!u) {
    return { label: '탈퇴/삭제된 사용자', raw: id, kind: 'user', tombstone: true, role: opts.role }
  }
  const label = userLabel(u) || (u.email ? u.email : id)
  return {
    label, raw: id, kind: 'user', tombstone: false,
    route: `/users/${encodeURIComponent(id)}`,
    role: u.role || u.worker_type || opts.role || '사용자',
  }
}

/** Resolve a harness/peer ID → IdentityView. */
export function resolveHarness(id: string, ctx: IdentityContext): IdentityView {
  if (!id) return { label: '알 수 없는 하네스', raw: '', kind: 'unknown', tombstone: true }
  const h = ctx.harnessesById[id]
  if (!h) {
    return { label: '삭제/오프라인 하네스', raw: id, kind: 'harness', tombstone: true }
  }
  return {
    label: h.name || h.harness_id, raw: id, kind: 'harness', tombstone: false,
    route: `/fleet?harness_id=${encodeURIComponent(id)}`, role: '하네스',
  }
}

/** Resolve a message actor (user id or system/service/harness sender). */
export function resolveActor(
  senderId: string | undefined,
  senderType: string | undefined,
  ctx: IdentityContext,
): IdentityView {
  const type = (senderType || '').toLowerCase()
  if (type === 'system') return { label: '시스템', raw: senderId || '', kind: 'system', tombstone: false, role: '시스템' }
  if (type === 'service') return { label: '서비스', raw: senderId || '', kind: 'service', tombstone: false, role: '서비스' }
  if (type === 'harness' || (senderId || '').startsWith('hrn_')) return resolveHarness(senderId || '', ctx)
  // default: a user id
  return resolveUser(senderId || '', ctx, { role: '사용자' })
}

/** Relative freshness label for a timestamp. */
export function freshnessLabel(ts?: string): string {
  if (!ts) return '시각 미기록'
  const t = new Date(ts).getTime()
  if (Number.isNaN(t)) return '시각 미기록'
  const s = Math.floor((Date.now() - t) / 1000)
  if (s < 0) return '방금 전'
  if (s < 60) return `${s}초 전`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}분 전`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}시간 전`
  const d = Math.floor(h / 24)
  return `${d}일 전`
}

/** Author-owned vs authorized moderation for edit/delete (PAT-1512). */
export interface EditDeleteDecision {
  canEdit: boolean
  canDelete: boolean
  reason: string // Korean explanation for the operator
  moderation: boolean // true when the acting operator is NOT the author
}

export function editDeleteDecision(
  msg: { sender_id?: string; sender_type?: string; edited?: boolean; deleted_by?: boolean },
  actor: { id?: string; role?: string; isAdmin?: boolean },
): EditDeleteDecision {
  const type = (msg.sender_type || '').toLowerCase()
  const isSystem = type === 'system' || type === 'service'
  const isAuthor = actor.id && actor.id === msg.sender_id
  // Authors can edit/delete their own; admins/operators can delete any
  // user's message (moderation) — never delete system/service records.
  const canDelete = isSystem ? false : (isAuthor ? true : !!actor.isAdmin)
  const canEdit = isSystem ? false : isAuthor
  if (isSystem) {
    return { canEdit: false, canDelete: false, moderation: false, reason: '시스템/서비스 메시지는 수정·삭제할 수 없습니다' }
  }
  if (isAuthor) {
    return { canEdit: true, canDelete: true, moderation: false, reason: '본인 작성 메시지입니다' }
  }
  if (!!actor.isAdmin) {
    return { canEdit: false, canDelete: true, moderation: true, reason: '관리자 권한으로 삭제(중재) — 사유·확인·감사 기록 필요' }
  }
  return { canEdit: false, canDelete: false, moderation: false, reason: '작성자 또는 관리자만 수정·삭제할 수 있습니다' }
}

/** Explainable read receipt, e.g. "읽음 2 (김민서 외 1명)". */
export function readReceiptLabel(readBy: string[], ctx: IdentityContext): string {
  if (!readBy || readBy.length === 0) return ''
  const names = readBy.slice(0, 2).map(id => {
    const v = resolveUser(id, ctx)
    return v.tombstone ? id : v.label
  })
  const extra = readBy.length > 2 ? ` 외 ${readBy.length - 2}명` : ''
  return `읽음 ${readBy.length} (${names.join(', ')}${extra})`
}
