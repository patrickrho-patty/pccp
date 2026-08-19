import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { EntitySelect } from '../components/EntitySelect'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import {
  LARGE_AUDIENCE_THRESHOLD,
  audienceSizeOf,
  broadcastSendBlockers,
  exclusionReasonKo,
  mergeReachability,
  renderBroadcastText,
  resolveAudiencePreview,
} from '../broadcastAudience'
import type { BroadcastScopeType } from '../broadcastAudience'
import { formatShortTime, formatBytes } from '../utils/format'
import {
  buildIdentityContext, resolveActor, readReceiptLabel, editDeleteDecision,
  freshnessLabel, IdentityContext,
} from '../identityView'

// Communications hub (web/13 plan): real-time SSE (A1), threading/
// mentions/reactions/read receipts (B1/B2), AI-context linking (B4),
// broadcast ack dashboard (B5), 1:1 DM from user search (C1), system
// commands (C3), real file transfer upload/scan/download (A3/C4).

const SEVERITY_KO: Record<string, string> = { info: '안내', warning: '경고', critical: '심각', emergency: '긴급' }
const SEVERITY_BADGE: Record<string, string> = {
  info: 'bg-blue-50 text-blue-700 border-blue-200',
  warning: 'bg-yellow-50 text-yellow-700 border-yellow-200',
  critical: 'bg-orange-50 text-orange-700 border-orange-200',
  emergency: 'bg-red-50 text-red-700 border-red-200',
}

// PAT-1510: governed broadcast composer — explicit audience scope,
// preview, confirmation gates. Scope types map to backend target types.
const SCOPE_KO: Record<string, string> = { org: '조직 전체', project: '프로젝트', user: '특정 사용자' }
// Presence states render with their actual Korean label (away/busy are
// still reachable per mergeReachability, not offline).
const PRESENCE_KO: Record<string, string> = { online: '온라인', away: '자리비움', busy: '바쁨', offline: '오프라인' }
// crypto.randomUUID throws in non-secure contexts — shared idempotency-key
// helper lives in web/src/utils/id.ts (handles the fallback uniformly so
// broadcasts and fleet bulk actions cannot drift apart).
import { newIdempotencyKey } from '../utils/id'
const newClientToken = (): string => newIdempotencyKey()
const BC_FORM_INIT = {
  severity: 'info', title: '', title_ko: '', body: '', body_ko: '', requires_ack: false,
  scope_type: '' as BroadcastScopeType, target_id: '', expires_at: '',
  confirm_reason: '', allow_empty: false, confirm_large: false,
}

const TABS = [
  { id: 'chat', label: '채팅' },
  { id: 'broadcast', label: '방송' },
  { id: 'files', label: '파일' },
  { id: 'presence', label: '접속 현황' },
]

export default function Communications() {
  const { favorites, sortPinnedFirst } = useFavorites('conversations')
  const [tab, setTab] = useState('chat')
  const [conversations, setConversations] = useState<any[]>([])
  const [activeConv, setActiveConv] = useState<any>(null)
  const [messages, setMessages] = useState<any[]>([])
  const [broadcasts, setBroadcasts] = useState<any[]>([])
  const [transfers, setTransfers] = useState<any[]>([])
  const [presence, setPresence] = useState<any[]>([])
  const [text, setText] = useState('')
  const [replyTo, setReplyTo] = useState<any>(null)
  const [newConvOpen, setNewConvOpen] = useState(false)
  const [dmUser, setDmUser] = useState('')
  const [newConvTitle, setNewConvTitle] = useState('')
  const [broadcastOpen, setBroadcastOpen] = useState(false)
  const [bcForm, setBcForm] = useState(BC_FORM_INIT)
  // Audience preview inputs: org users, project roster, idempotency token.
  const [bcUsers, setBcUsers] = useState<any[]>([])
  const [bcMemberIds, setBcMemberIds] = useState<Set<string> | null>(null)
  const [bcClientToken, setBcClientToken] = useState('')
  const [bcSending, setBcSending] = useState(false)
  const [ackTarget, setAckTarget] = useState<any>(null)
  const [ackDash, setAckDash] = useState<any>(null)
  const [transferOpen, setTransferOpen] = useState(false)
  const [transferForm, setTransferForm] = useState({ recipient_id: '', file_name: '', file_type: 'text', classification: 'internal', expires_at: '' })
  const [transferFile, setTransferFile] = useState<File | null>(null)
  const [transferPreview, setTransferPreview] = useState('')
  const [uploadTarget, setUploadTarget] = useState<any>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const [linkTarget, setLinkTarget] = useState<any>(null)
  const [linkSessionId, setLinkSessionId] = useState('')
  const [editingMsg, setEditingMsg] = useState<any>(null)
  const [editText, setEditText] = useState('')
  const [unread, setUnread] = useState<Set<string>>(new Set())
  // PAT-1512: user/harness identity context for resolving authors, presence,
  // read receipts, and edit/delete ownership across all comm surfaces.
  const [users, setUsers] = useState<any[]>([])
  const [identityCtx, setIdentityCtx] = useState<IdentityContext>({ usersById: {}, harnessesById: {} })
  useEffect(() => { setIdentityCtx(buildIdentityContext(users)) }, [users])

  const loadAll = () => {
    api.listConversations().then((d: any[]) => setConversations(Array.isArray(d) ? d : [])).catch(() => {})
    // Parse the frozen audience snapshot once here, not per row per render.
    api.listBroadcasts().then((d: any[]) =>
      setBroadcasts((Array.isArray(d) ? d : []).map((b: any) => ({ ...b, audience_size: audienceSizeOf(b) })))
    ).catch(() => {})
    api.listFileTransfers().then((d: any[]) => setTransfers(Array.isArray(d) ? d : [])).catch(() => {})
    api.getPresence().then((d: any[]) => setPresence(Array.isArray(d) ? d : [])).catch(() => {})
    api.listUsers().then((d: any[]) => { const arr = Array.isArray(d) ? d : []; setUsers(arr); setBcUsers(arr) }).catch(() => {})
  }
  useEffect(() => { loadAll() }, [])

  const loadMessages = (conv: any) => {
    setActiveConv(conv)
    api.listMessages(conv.id).then((d: any[]) => setMessages((Array.isArray(d) ? d : []).map(enrichMessage))).catch(() => setMessages([]))
  }

  // SSE (A1): live fan-out for messages/broadcasts/transfers/presence.
  useEffect(() => {
		let sse: EventSource | null = null
		let reconnectTimer = 0
		let stopped = false
		const onMessage = (ev: MessageEvent) => {
      try {
        const event = JSON.parse(ev.data)
        switch (event.type) {
          case 'comms.message':
            if (activeConv && event.payload?.conversation_id === activeConv.id) {
              setMessages(prev => {
                const exists = prev.some(m => m.id === event.payload?.message?.id)
                return exists ? prev : [...prev, enrichMessage(event.payload.message)]
              })
            }
            setUnread(prev => { const n = new Set(prev); n.add(event.payload?.conversation_id); return n })
            break
          case 'comms.broadcast':
          case 'comms.transfer':
          case 'comms.presence':
            loadAll()
            break
          default:
            break
        }
      } catch { /* ignore malformed */ }
    }
		const connect = async () => {
			try {
				const ticket = await api.liveStreamTicket()
				if (stopped) return
				sse = new EventSource(ticket.stream_url)
				sse.onmessage = onMessage
				sse.onerror = () => {
					sse?.close()
					if (!stopped) reconnectTimer = window.setTimeout(connect, 1500)
				}
			} catch {
				if (!stopped) reconnectTimer = window.setTimeout(connect, 1500)
			}
		}
		connect()
		return () => {
			stopped = true
			window.clearTimeout(reconnectTimer)
			sse?.close()
		}
  }, [activeConv?.id])

  const send = async () => {
    if (!activeConv || !text.trim()) return
    try {
      await api.sendMessage(activeConv.id, {
        sender_id: 'operator',
        content: text,
        parent_id: replyTo?.id || '',
      })
      setText('')
      setReplyTo(null)
    } catch (e: any) { showToast(e?.message || '전송 실패', 'error') }
  }

  const react = async (msg: any, emoji: string) => {
    try {
      await api.reactMessage(msg.id, emoji, 'operator')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const markRead = async (msg: any) => {
    try { await api.readMessage(msg.id, 'operator') } catch { /* noop */ }
  }

  const saveEdit = async () => {
    if (!editingMsg) return
    try {
      await api.editMessage(editingMsg.id, editText)
      setEditingMsg(null)
      setEditText('')
      if (activeConv) loadMessages(activeConv)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const delMessage = async (msg: any) => {
    try {
      await api.deleteMessage(msg.id, 'operator')
      if (activeConv) loadMessages(activeConv)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  // PAT-1512: moderation delete requires a reason + confirm (audited).
  const [delTarget, setDelTarget] = useState<any>(null)
  const [delReason, setDelReason] = useState('')
  const confirmDelete = async () => {
    if (!delTarget) return
    if (!delReason.trim()) { showToast('중재 삭제 사유가 필요합니다', 'error'); return }
    try {
      await api.deleteMessage(delTarget.id, 'operator', delReason.trim())
      showToast('삭제 완료 (감사 기록)', 'success')
      setDelTarget(null); setDelReason('')
      if (activeConv) loadMessages(activeConv)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const linkContext = async (msg: any, sessionId: string) => {
    try {
      await api.linkMessage(msg.id, sessionId, '')
      showToast('AI 컨텍스트 연결 완료', 'success')
      if (activeConv) loadMessages(activeConv)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const openDM = async () => {
    if (!dmUser) return
    try {
      const conv = await api.openDM(dmUser)
      setNewConvOpen(false)
      setDmUser('')
      loadAll()
      loadMessages(conv)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const createChannel = async () => {
    if (!newConvTitle.trim()) return
    try {
      await api.createConversation({ type: 'channel', title: newConvTitle, participants: ['operator'] })
      setNewConvOpen(false)
      setNewConvTitle('')
      loadAll()
      showToast('채널 생성 완료', 'success')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  // PAT-1510: open the composer with a fresh idempotency token and load
  // the audience data (users; project roster loads on scope selection).
  const openBroadcast = () => {
    setBcForm(BC_FORM_INIT)
    setBcMemberIds(null)
    setBcClientToken(newClientToken())
    api.listUsers().then((d: any[]) => setBcUsers(Array.isArray(d) ? d : [])).catch(() => setBcUsers([]))
    setBroadcastOpen(true)
  }

  // Project scope needs the roster to resolve the audience client-side.
  useEffect(() => {
    if (bcForm.scope_type !== 'project' || !bcForm.target_id) { setBcMemberIds(null); return }
    api.listProjectMembers(bcForm.target_id)
      .then((d: any[]) => setBcMemberIds(new Set((Array.isArray(d) ? d : []).map((m: any) => m.user_id).filter(Boolean))))
      .catch(() => setBcMemberIds(new Set()))
  }, [bcForm.scope_type, bcForm.target_id])

  const bcScope = { type: bcForm.scope_type, targetId: bcForm.target_id }
  // Memoized: composer keystrokes must not re-run O(users) filtering.
  const bcPreview = useMemo(
    () => resolveAudiencePreview(bcUsers, { type: bcForm.scope_type, targetId: bcForm.target_id }, bcMemberIds),
    [bcUsers, bcForm.scope_type, bcForm.target_id, bcMemberIds],
  )
  const bcReach = useMemo(() => mergeReachability(bcPreview.eligible, presence), [bcPreview, presence])
  const bcBlockers = broadcastSendBlockers({
    title: bcForm.title, scope: bcScope, eligibleCount: bcPreview.eligible.length,
    severity: bcForm.severity, confirmReason: bcForm.confirm_reason,
    allowEmpty: bcForm.allow_empty, confirmLarge: bcForm.confirm_large,
  })

  const sendBroadcast = async () => {
    const blockers = bcBlockers
    if (blockers.length > 0) {
      showToast(blockers[0], 'error')
      return
    }
    if (bcSending) return
    setBcSending(true)
    try {
      await api.sendBroadcastGoverned({
        severity: bcForm.severity, title: bcForm.title, title_ko: bcForm.title_ko,
        body: bcForm.body, body_ko: bcForm.body_ko, requires_ack: bcForm.requires_ack,
        target_type: bcForm.scope_type, target_id: bcForm.target_id,
        expires_at: bcForm.expires_at ? new Date(bcForm.expires_at).toISOString() : '',
        confirm_reason: bcForm.confirm_reason, allow_empty: bcForm.allow_empty,
        client_token: bcClientToken,
      })
      setBroadcastOpen(false)
      setBcForm(BC_FORM_INIT)
      loadAll()
      showToast('방송 전송 완료', 'success')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
    finally { setBcSending(false) }
  }

  const showAcks = async (bc: any) => {
    setAckTarget(bc)
    try {
      const d = await api.broadcastAcks(bc.id)
      setAckDash(d)
    } catch (e: any) { setAckDash(null); showToast(e?.message || '실패', 'error') }
  }

  const createTransfer = async () => {
    if (!transferForm.file_name.trim() || !transferForm.recipient_id) {
      showToast('받는 사람과 파일명이 필요합니다', 'error')
      return
    }
    if (!transferForm.expires_at) {
      showToast('만료시각(retention deadline)을 입력하세요', 'error')
      return
    }
    try {
      const tr = await api.createFileTransfer({
        sender_id: 'operator', recipient_id: transferForm.recipient_id,
        file_name: transferForm.file_name,
        file_size: transferFile?.size || 0,
        file_type: transferForm.file_type,
        classification: transferForm.classification,
        expires_at: new Date(transferForm.expires_at).toISOString(),
      })
      setTransferOpen(false)
      setUploadTarget(tr)
      setTransferFile(null)
      setTransferPreview('')
      loadAll()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  // PAT-1511: client-side hash + preview so the sender sees the real
  // content fingerprint before deciding to upload. Falls back to a
  // JS-side hash when SubtleCrypto is unavailable (plain-http / older
  // browsers) so the preview still tells the sender what they're about
  // to send.
  const onTransferFilePicked = async (file: File | null) => {
    setTransferFile(file)
    if (!file) { setTransferPreview(''); return }
    const buf = await file.slice(0, Math.min(file.size, 64 * 1024)).arrayBuffer()
    let hash = ''
    if (typeof crypto !== 'undefined' && typeof crypto.subtle?.digest === 'function') {
      try {
        const hashBuf = await crypto.subtle.digest('SHA-256', buf)
        hash = Array.from(new Uint8Array(hashBuf)).map(b => b.toString(16).padStart(2, '0')).join('')
      } catch { /* secure-context failure — fall through */ }
    }
    if (!hash) {
      // JS FNV-1a 32 over the first 64KB — a fingerprint for display only;
      // the server hashes the full payload with sha256 at upload time.
      let h = 0x811c9dc5
      const view = new Uint8Array(buf)
      for (let i = 0; i < view.length; i++) {
        h ^= view[i]
        h = Math.imul(h, 0x01000193) >>> 0
      }
      hash = h.toString(16).padStart(8, '0').repeat(8) // 64-char placeholder
    }
    let preview = ''
    if (file.type.startsWith('text/') || /\.(md|txt|json|ya?ml|csv|log)$/i.test(file.name)) {
      try { preview = new TextDecoder().decode(buf).slice(0, 1024) } catch {}
    }
    setTransferPreview(`해시: ${hash.slice(0, 16)}…\n크기: ${formatBytes(file.size)}\n유형: ${file.type || 'unknown'}\n미리보기(최대 1KB):\n${preview || '(텍스트 미리보기 불가 — 바이너리 파일)'}`)
  }

  const uploadContent = async () => {
    const file = fileRef.current?.files?.[0]
    if (!file || !uploadTarget) {
      showToast('파일을 선택하세요', 'error')
      return
    }
    try {
      const res = await api.uploadFileTransfer(uploadTarget.id, file)
      if (res.scan_status === 'blocked') {
        showToast('보안 검사 차단 — 전송이 거부되었습니다', 'error')
      } else {
        showToast('업로드 완료 — 검사 통과', 'success')
      }
      setUploadTarget(null)
      if (fileRef.current) fileRef.current.value = ''
      loadAll()
    } catch (e: any) { showToast(e?.message || '업로드 실패', 'error') }
  }

  const downloadTransfer = async (tr: any) => {
    const token = sessionStorage.getItem('pccp_token')
    try {
      const resp = await fetch(`/api/communications/file-transfers/${tr.id}/download`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({}))
        throw new Error(err.error || 'download failed')
      }
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = tr.file_name
      a.click()
      URL.revokeObjectURL(url)
    } catch (e: any) { showToast(e?.message || '다운로드 실패', 'error') }
  }

  const transitionTransfer = async (tr: any, action: string) => {
    try {
      await api.transitionFileTransfer(tr.id, action)
      loadAll()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const parseReactions = (msg: any): Record<string, string[]> => {
    if (!msg.reactions) return {}
    try { return JSON.parse(msg.reactions) } catch { return {} }
  }
  const parseMentions = (msg: any): string[] => {
    if (!msg.mentions) return []
    try { return JSON.parse(msg.mentions) } catch { return [] }
  }
  const parseReadBy = (msg: any): string[] => {
    if (!msg.read_by) return []
    try { return JSON.parse(msg.read_by) } catch { return [] }
  }
  // Parse the JSON columns once when messages are set, not per row per
  // render (same enrichment pattern as audienceSizeOf in loadAll).
  const enrichMessage = (m: any) => ({
    ...m,
    _reactions: parseReactions(m),
    _mentions: parseMentions(m),
    _readBy: parseReadBy(m),
  })

  const sortedConvs = sortPinnedFirst(conversations, c => c.id)

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">커뮤니케이션 허브 · Communications</h2>
          <p className="text-[11px] text-gray-400">SSE 실시간 · 사용자 1:1 · 방송 확인 · 파일 전송 (검사 포함)</p>
        </div>
        <div className="flex gap-2 shrink-0 flex-wrap">
          <button className="btn-sm btn-primary" onClick={() => setNewConvOpen(true)}>+ 새 대화</button>
          <button className="btn-sm btn-secondary" onClick={openBroadcast}>방송 보내기</button>
          <button className="btn-sm btn-secondary" onClick={() => setTransferOpen(true)}>파일 전송</button>
        </div>
      </div>

      <div className="flex gap-1 border-b border-gray-200">
        {TABS.map(t => (
          <button key={t.id} onClick={() => setTab(t.id)}
            className={`px-3 py-2 text-xs ${tab === t.id ? 'border-b-2 border-blue-600 text-blue-600 font-semibold' : 'text-gray-500'}`}>
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'chat' && (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <div className="card p-2 space-y-1">
            {sortedConvs.length === 0 && <p className="text-[11px] text-gray-400 p-3">대화 없음 — "새 대화"로 시작하세요</p>}
            {sortedConvs.map((c: any) => (
              <button key={c.id} onClick={() => { setUnread(prev => { const n = new Set(prev); n.delete(c.id); return n }); loadMessages(c) }}
                className={`w-full text-left px-2 py-2 rounded flex items-center gap-2 text-xs ${activeConv?.id === c.id ? 'bg-blue-50' : 'hover:bg-gray-50'}`}>
                <FavoriteStar entity="conversations" id={c.id} />
                <span className="flex-1 truncate">{c.title || (c.type === 'direct' ? '1:1 채팅' : c.id.slice(0, 8))}</span>
                {unread.has(c.id) && <span className="w-2 h-2 rounded-full bg-blue-600" />}
              </button>
            ))}
          </div>
          <div className="card p-3 md:col-span-2 flex flex-col max-h-[560px]">
            {!activeConv ? (
              <div className="flex-1 flex items-center justify-center text-xs text-gray-400">대화를 선택하세요</div>
            ) : (
              <>
                <div className="flex-1 overflow-auto space-y-2 pr-1">
                  {messages.filter(m => !m.deleted_by).map((m: any) => {
                    const reactions: Record<string, string[]> = m._reactions || {}
                    const mentions: string[] = m._mentions || []
                    const readBy: string[] = m._readBy || []
                    const isCommand = m.content_type === 'command'
                    const author = resolveActor(m.sender_id, m.sender_type, identityCtx)
                    const decision = editDeleteDecision(m, { id: 'operator', isAdmin: true })
                    return (
                      <div key={m.id} className={`text-xs ${isCommand ? 'border-l-2 border-red-400 bg-red-50/50 p-2 rounded' : ''}`}
                        onClick={() => markRead(m)}>
                        {replyTo?.id !== m.id && m.parent_message_id && (
                          <div className="text-[10px] text-gray-400 ml-4">↳ 답글</div>
                        )}
                        <div className="flex items-start gap-2">
                          {author.route ? (
                            <Link to={author.route} className="font-semibold text-gray-700 hover:text-blue-600 hover:underline" title={author.raw}>{author.label}</Link>
                          ) : (
                            <span className={`font-semibold ${author.tombstone ? 'text-gray-400 line-through' : 'text-gray-700'}`}>{author.label}</span>
                          )}
                          {author.role && author.role !== '사용자' && <span className="text-[9px] px-1 rounded bg-gray-100 text-gray-500">{author.role}</span>}
                          <span className="text-gray-500 flex-1 whitespace-pre-wrap">{m.content}</span>
                          {m.edited && <span className="text-[9px] text-gray-400" title={decision.reason}>(수정됨)</span>}
                          {readBy.length > 0 && <span className="text-[9px] text-gray-400" title={readReceiptLabel(readBy, identityCtx)}>읽음 {readBy.length}</span>}
                        </div>
                        {mentions.length > 0 && <div className="text-[10px] text-blue-600">@{mentions.join(', @')}</div>}
                        {m.linked_session_id && (
                          <div className="text-[10px] text-purple-600">
                            🔗 <Link className="hover:underline" to={`/sessions/${m.linked_session_id}`}>{m.linked_session_id.slice(0, 12)}</Link>
                          </div>
                        )}
                        <div className="flex gap-1 mt-0.5 text-[10px]">
                          {Object.entries(reactions).map(([emoji, users]) => (
                            <button key={emoji} className="px-1 rounded bg-gray-100 hover:bg-gray-200" onClick={() => react(m, emoji)}>
                              {emoji} {users.length}
                            </button>
                          ))}
                          <button className="px-1 rounded hover:bg-gray-100 text-gray-500" onClick={() => react(m, '👍')}>👍</button>
                          <button className="px-1 rounded hover:bg-gray-100 text-gray-500" onClick={() => { setReplyTo(m); setText('') }}>답글</button>
                          {decision.canEdit && <button className="px-1 rounded hover:bg-gray-100 text-gray-500" onClick={() => { setEditingMsg(m); setEditText(m.content) }}>수정</button>}
                          {decision.canDelete && <button className="px-1 rounded hover:bg-gray-100 text-gray-500" title={decision.moderation ? '관리자 중재 — 사유·확인·감사 필요' : ''} onClick={() => { if (decision.moderation) { setDelTarget(m); setDelReason('') } else delMessage(m) }}>삭제</button>}
                          <button className="px-1 rounded hover:bg-gray-100 text-gray-500" onClick={() => { setLinkTarget(m); setLinkSessionId('') }}>링크</button>
                        </div>
                      </div>
                    )
                  })}
                  {messages.length === 0 && <EmptyState icon="💬" title="메시지가 없습니다" message="첫 메시지를 보내보세요." />}
                </div>
                {replyTo && (
                  <div className="text-[10px] text-gray-500 bg-gray-50 rounded px-2 py-1 flex justify-between">
                    <span>답글 대상: {replyTo.content.slice(0, 40)}</span>
                    <button onClick={() => setReplyTo(null)}>✕</button>
                  </div>
                )}
                <div className="flex gap-2 mt-2">
                  <input className="input text-xs flex-1" placeholder="메시지 입력 (Enter 전송)"
                    value={text}
                    onChange={e => setText(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() } }} />
                  <button className="btn-sm btn-primary" onClick={send}>전송</button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {tab === 'broadcast' && (
        <div className="card p-4 space-y-2">
          {broadcasts.length === 0 && <p className="text-[11px] text-gray-400">방송 없음</p>}
          {broadcasts.map((b: any) => {
            // Audience size comes pre-parsed from loadAll (snapshot frozen
            // at send time, PAT-1510); null for legacy broadcasts.
            const audienceCount: number | null = b.audience_size ?? null
            const expired = b.status === 'expired' || (b.expires_at && new Date(b.expires_at).getTime() < Date.now())
            return (
            <div key={b.id} className="border rounded-lg p-2 flex items-start justify-between gap-2">
              <div>
                <span className={`text-[10px] px-2 py-0.5 rounded-full border ${SEVERITY_BADGE[b.severity] || ''}`}>{SEVERITY_KO[b.severity] || b.severity}</span>
                {expired && <span className="text-[10px] px-2 py-0.5 rounded-full border bg-gray-50 text-gray-500 border-gray-200 ml-1">만료됨</span>}
                <div className="text-xs font-semibold mt-1">{renderBroadcastText(b, 'ko-KR').title}</div>
                <div className="text-[11px] text-gray-500">{renderBroadcastText(b, 'ko-KR').body}</div>
                <div className="text-[10px] text-gray-400 mt-0.5">
                  대상: {SCOPE_KO[b.target_type] || b.target_type || '전체'}
                  {audienceCount !== null && ` · 수신 ${audienceCount}명`}
                  {b.expires_at && ` · 만료 ${formatShortTime(b.expires_at)}`}
                </div>
                {b.requires_ack && <div className="text-[10px] text-amber-600 mt-0.5">확인 필요 · ack {b.ack_count || 0}</div>}
              </div>
              <button className="btn-xs-secondary" onClick={() => showAcks(b)}>확인 현황</button>
            </div>
            )
          })}
        </div>
      )}

      {tab === 'files' && (
        <div className="card p-4 space-y-2">
          {transfers.length === 0 && <p className="text-[11px] text-gray-400">파일 전송 없음</p>}
          {transfers.map((t: any) => (
            <div key={t.id} className="border rounded-lg p-2 flex items-center justify-between gap-2 text-[11px]">
              <div className="min-w-0">
                <div className="font-semibold truncate">{t.file_name}</div>
                <div className="text-gray-400">
                  {t.scan_status} · {t.status} · {(t.file_size || 0)}B
                  {t.scan_findings && <span className="text-red-500"> (검사 발견)</span>}
                </div>
              </div>
              <div className="flex gap-1 shrink-0">
                {t.status === 'pending' && <button className="text-[10px] px-2 py-1 rounded bg-blue-50 text-blue-600" onClick={() => setUploadTarget(t)}>업로드</button>}
                {t.status === 'ready' && (
                  <>
                    <button className="text-[10px] px-2 py-1 rounded bg-green-50 text-green-600" onClick={() => transitionTransfer(t, 'accept')}>수락</button>
                    <button className="text-[10px] px-2 py-1 rounded bg-red-50 text-red-600" onClick={() => transitionTransfer(t, 'decline')}>거절</button>
                  </>
                )}
                {t.scan_status === 'clean' && <button className="text-[10px] px-2 py-1 rounded bg-gray-100" onClick={() => downloadTransfer(t)}>다운로드</button>}
                {t.status === 'downloading' && <button className="text-[10px] px-2 py-1 rounded bg-gray-100" onClick={() => transitionTransfer(t, 'complete')}>완료</button>}
              </div>
            </div>
          ))}
        </div>
      )}

      {tab === 'presence' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-1">접속 상태는 신선도와 소스 기준으로 표시됩니다 — 삭제/오프라인 사용자는 톰스톤 처리됩니다.</p>
          {presence.length === 0 && <p className="text-[11px] text-gray-400">접속 기록 없음</p>}
          {presence.map((p: any) => {
            const who = resolveActor(p.user_id, p.actor_type, identityCtx)
            return (
              <div key={p.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
                <span className="text-gray-700">
                  <span className={`inline-block w-2 h-2 rounded-full mr-1 ${p.status === 'online' ? 'bg-green-500' : p.status === 'away' ? 'bg-yellow-400' : 'bg-gray-300'}`} />
                  {who.route ? <Link to={who.route} className="hover:text-blue-600 hover:underline">{who.label}</Link> : <span className={who.tombstone ? 'text-gray-400 line-through' : ''}>{who.label}</span>}
                  {who.role && who.role !== '사용자' && <span className="text-[9px] px-1 ml-1 rounded bg-gray-100 text-gray-500">{who.role}</span>}
                </span>
                <span className="text-gray-400">{p.activity || p.status} · {freshnessLabel(p.last_active_at)} ({(p.last_active_at || '').slice(0, 16)})</span>
              </div>
            )
          })}
        </div>
      )}

      {/* New conversation / DM (C1) */}
      <Modal open={newConvOpen} title="새 대화" onClose={() => setNewConvOpen(false)}
        footer={<div className="flex gap-2 justify-end">
          <button className="btn-sm btn-secondary" onClick={createChannel}>채널 생성</button>
          <button className="btn-sm btn-primary" onClick={openDM}>1:1 시작</button>
        </div>}>
        <div className="space-y-2">
          <div>
            <label className="text-[10px] text-gray-500">사용자 선택 (1:1 채팅)</label>
            <EntitySelect entity="user" value={dmUser} onChange={setDmUser} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">채널 제목</label>
            <input className="input text-xs w-full" value={newConvTitle} onChange={e => setNewConvTitle(e.target.value)} />
          </div>
        </div>
      </Modal>

      {/* Broadcast composer (PAT-1510): explicit audience scope + impact
          preview + confirmation gates before send. */}
      <Modal open={broadcastOpen} title="방송 보내기" onClose={() => setBroadcastOpen(false)} size="lg"
        footer={<ModalFooter onCancel={() => setBroadcastOpen(false)} onConfirm={sendBroadcast} confirmLabel="전송" disabled={bcBlockers.length > 0 || bcSending} />}>
        <div className="space-y-2">
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label htmlFor="bc-severity" className="text-[10px] text-gray-500">심각도</label>
              <select id="bc-severity" className="input text-xs w-full" value={bcForm.severity} onChange={e => setBcForm({ ...bcForm, severity: e.target.value })}>
                {Object.entries(SEVERITY_KO).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
              </select>
            </div>
            <div>
              <label htmlFor="bc-scope" className="text-[10px] text-gray-500">수신 대상 범위 (필수)</label>
              <select id="bc-scope" className="input text-xs w-full" value={bcForm.scope_type}
                onChange={e => setBcForm({ ...bcForm, scope_type: e.target.value as BroadcastScopeType, target_id: '' })}>
                <option value="">선택...</option>
                {Object.entries(SCOPE_KO).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
              </select>
            </div>
          </div>
          {bcForm.scope_type === 'project' && (
            <div>
              <label className="text-[10px] text-gray-500">프로젝트</label>
              <EntitySelect entity="project" value={bcForm.target_id} onChange={v => setBcForm({ ...bcForm, target_id: v })} />
            </div>
          )}
          {bcForm.scope_type === 'user' && (
            <div>
              <label className="text-[10px] text-gray-500">사용자</label>
              <EntitySelect entity="user" value={bcForm.target_id} onChange={v => setBcForm({ ...bcForm, target_id: v })} />
            </div>
          )}
          <div>
            <label htmlFor="bc-title" className="text-[10px] text-gray-500">제목</label>
            <input id="bc-title" className="input text-xs w-full" value={bcForm.title} onChange={e => setBcForm({ ...bcForm, title: e.target.value })} />
          </div>
          <div>
            <label htmlFor="bc-title-ko" className="text-[10px] text-gray-500">제목 (KO)</label>
            <input id="bc-title-ko" className="input text-xs w-full" value={bcForm.title_ko} onChange={e => setBcForm({ ...bcForm, title_ko: e.target.value })} />
          </div>
          <div>
            <label htmlFor="bc-body" className="text-[10px] text-gray-500">본문</label>
            <textarea id="bc-body" className="input text-xs w-full" rows={2} value={bcForm.body} onChange={e => setBcForm({ ...bcForm, body: e.target.value })} />
          </div>
          <div>
            <label htmlFor="bc-body-ko" className="text-[10px] text-gray-500">본문 (KO)</label>
            <textarea id="bc-body-ko" className="input text-xs w-full" rows={2} value={bcForm.body_ko} onChange={e => setBcForm({ ...bcForm, body_ko: e.target.value })} />
          </div>
          <div>
            <label htmlFor="bc-expires" className="text-[10px] text-gray-500">만료 시각 (선택)</label>
            <input id="bc-expires" type="datetime-local" className="input text-xs w-full" value={bcForm.expires_at} onChange={e => setBcForm({ ...bcForm, expires_at: e.target.value })} />
          </div>
          <label className="flex items-center gap-2 text-xs text-gray-600">
            <input type="checkbox" checked={bcForm.requires_ack} onChange={e => setBcForm({ ...bcForm, requires_ack: e.target.checked })} />
            확인(ack) 필수
          </label>
          {(bcForm.severity === 'critical' || bcForm.severity === 'emergency') && (
            <div>
              <label htmlFor="bc-confirm-reason" className="text-[10px] text-red-500">심각/긴급 전송 사유 (필수, 감사 로그 기록)</label>
              <input id="bc-confirm-reason" className="input text-xs w-full" placeholder="예: 긴급 보안 패치 적용" value={bcForm.confirm_reason} onChange={e => setBcForm({ ...bcForm, confirm_reason: e.target.value })} />
            </div>
          )}

          {/* Impact preview: who receives it, through which channel, and
              how the message renders — frozen server-side at send time. */}
          <div className="border rounded-lg p-2 bg-gray-50 space-y-1">
            <div className="text-[10px] font-semibold text-gray-500">전송 미리보기</div>
            {!bcForm.scope_type ? (
              <p className="text-[11px] text-gray-400">수신 대상 범위를 선택하면 대상이 계산됩니다</p>
            ) : (
              <>
                <div className="text-[11px] text-gray-700">
                  수신 <span className="font-semibold">{bcPreview.eligible.length}명</span>
                  {bcPreview.excluded.length > 0 && <span className="text-gray-400"> · 제외 {bcPreview.excluded.length}명 (정지/퇴사)</span>}
                  <span className="text-gray-400"> · 온라인 {bcReach.online} · 오프라인 {bcReach.offline}</span>
                </div>
                {bcPreview.eligible.length > 0 && (
                  <div className="text-[10px] text-gray-500">
                    수신자: {bcPreview.eligible.slice(0, 5).map(u => u.name_ko || u.name || u.email).join(', ')}
                    {bcPreview.eligible.length > 5 && ` 외 ${bcPreview.eligible.length - 5}명`}
                  </div>
                )}
                {bcPreview.excluded.length > 0 && (
                  <div className="text-[10px] text-gray-400">
                    제외: {bcPreview.excluded.slice(0, 5).map(e => `${e.user.name_ko || e.user.name || e.user.email} ({exclusionReasonKo(e.reason)})`).join(', ')}
                    {bcPreview.excluded.length > 5 && ` 외 ${bcPreview.excluded.length - 5}명`}
                  </div>
                )}
                <div className="text-[10px] text-gray-500 border-t border-gray-200 pt-1 mt-1">
                  수신자 화면: <span className={`px-1.5 py-0.5 rounded-full border ${SEVERITY_BADGE[bcForm.severity] || ''}`}>{SEVERITY_KO[bcForm.severity]}</span>
                  {' '}<span className="font-semibold">{renderBroadcastText(bcForm, 'ko-KR').title || '(제목 없음)'}</span>
                  {' — '}{renderBroadcastText(bcForm, 'ko-KR').body || '(본문 없음)'}
                  {bcForm.requires_ack && <span className="text-amber-600"> · 확인(ack) 필수</span>}
                  {bcForm.expires_at && <span> · 만료 {bcForm.expires_at.replace('T', ' ')}</span>}
                </div>
                {bcPreview.eligible.length === 0 && (
                  <label className="flex items-center gap-2 text-[11px] text-red-600">
                    <input type="checkbox" checked={bcForm.allow_empty} onChange={e => setBcForm({ ...bcForm, allow_empty: e.target.checked })} />
                    수신 대상 0명 — 그래도 전송함을 확인합니다
                  </label>
                )}
                {bcPreview.eligible.length > LARGE_AUDIENCE_THRESHOLD && (
                  <label className="flex items-center gap-2 text-[11px] text-amber-700">
                    <input type="checkbox" checked={bcForm.confirm_large} onChange={e => setBcForm({ ...bcForm, confirm_large: e.target.checked })} />
                    대규모 대상({bcPreview.eligible.length}명) 전송을 확인합니다
                  </label>
                )}
              </>
            )}
          </div>
          {bcBlockers.length > 0 && (
            <ul className="text-[10px] text-red-600 list-disc pl-4 space-y-0.5">
              {bcBlockers.map(b => <li key={b}>{b}</li>)}
            </ul>
          )}
        </div>
      </Modal>

      {/* Delivery/ack dashboard (B5, PAT-1510): exact frozen recipients
          with ack + reachability state, exclusions, expired flag. */}
      <Modal open={!!ackTarget} title={`확인 현황 — ${ackTarget?.title_ko || ackTarget?.title || ''}`}
        onClose={() => setAckTarget(null)}
        footer={<ModalFooter onCancel={() => setAckTarget(null)} onConfirm={() => setAckTarget(null)} confirmLabel="닫기" />}>
        {ackDash ? (
          <div className="space-y-2 text-xs">
            {ackDash.expired && <div className="text-[10px] text-gray-500 bg-gray-50 rounded px-2 py-1">만료된 방송입니다</div>}
            <div>확인률: {Math.round((ackDash.ack_rate || 0) * 100)}% ({ackDash.acked}/{ackDash.total_users})</div>
            <div className="max-h-56 overflow-auto space-y-1">
              {(ackDash.recipients || []).map((p: any) => (
                <div key={p.user_id} className="flex items-center justify-between text-gray-600 border-b border-gray-50 py-0.5">
                  <span>{p.name_ko || p.name} ({p.email})</span>
                  <span className="flex items-center gap-1 text-[10px]">
                    <span className={p.presence === 'online' ? 'text-green-600' : p.presence === 'away' || p.presence === 'busy' ? 'text-yellow-600' : 'text-gray-400'}>
                      {PRESENCE_KO[p.presence] || PRESENCE_KO.offline}
                    </span>
                    <span className={p.acked ? 'text-blue-600 font-semibold' : 'text-amber-600'}>
                      {p.acked ? '확인 완료' : '확인 대기'}
                    </span>
                  </span>
                </div>
              ))}
              {(ackDash.recipients || []).length === 0 && <p className="text-gray-400">수신 대상 없음</p>}
            </div>
            {(ackDash.excluded || []).length > 0 && (
              <div className="text-[10px] text-gray-400">
                제외됨: {ackDash.excluded.map((e: any) => `${e.name_ko || e.name || e.email} ({exclusionReasonKo(e.reason)})`).join(', ')}
              </div>
            )}
          </div>
        ) : <p className="text-xs text-gray-400">로딩...</p>}
      </Modal>

      {/* Transfer composer (PAT-1511): file picker computes client-side hash
          + size + preview before any upload; retention deadline is required */}
      <Modal open={transferOpen} title="파일 전송" onClose={() => setTransferOpen(false)}
        footer={<ModalFooter onCancel={() => setTransferOpen(false)} onConfirm={createTransfer} confirmLabel="생성 + 업로드" />}>
        <div className="space-y-2">
          <div>
            <label className="text-[10px] text-gray-500">받는 사용자</label>
            <EntitySelect entity="user" value={transferForm.recipient_id} onChange={v => setTransferForm({ ...transferForm, recipient_id: v })} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">실제 파일 (검사 포함 — PAT-1511)</label>
            <input type="file" className="text-xs w-full" onChange={e => onTransferFilePicked(e.target.files?.[0] || null)} />
          </div>
          {transferPreview && (
            <pre className="text-[10px] bg-gray-50 border border-gray-200 rounded p-2 whitespace-pre-wrap max-h-32 overflow-auto">{transferPreview}</pre>
          )}
          <input className="input text-xs w-full" placeholder="파일명 (수신자 표기용)" value={transferForm.file_name} onChange={e => setTransferForm({ ...transferForm, file_name: e.target.value })} />
          <select className="input text-xs w-full" value={transferForm.classification} onChange={e => setTransferForm({ ...transferForm, classification: e.target.value })}>
            <option value="internal">internal</option>
            <option value="confidential">confidential</option>
            <option value="public">public</option>
          </select>
          <label className="text-[10px] text-gray-500">만료시각 (retention deadline) — 필수</label>
          <input type="datetime-local" className="input text-xs w-full" value={transferForm.expires_at} onChange={e => setTransferForm({ ...transferForm, expires_at: e.target.value })} />
          {transferFile && <p className="text-[10px] text-gray-500">선택한 파일: {transferFile.name} · {formatBytes(transferFile.size)}</p>}
        </div>
      </Modal>

      {/* Upload content (A3) */}
      <Modal open={!!uploadTarget} title={`업로드 — ${uploadTarget?.file_name || ''}`}
        onClose={() => setUploadTarget(null)}
        footer={<ModalFooter onCancel={() => setUploadTarget(null)} onConfirm={uploadContent} confirmLabel="업로드 + 검사" />}>
        <div className="space-y-2">
          <p className="text-[11px] text-gray-500">업로드 시 보안 콘텐츠 검사(비밀/민감 정보)가 실행되고, 발견 시 차단됩니다.</p>
          <input ref={fileRef} type="file" className="text-xs" />
        </div>
      </Modal>

      {/* AI-context link (B4) */}
      <Modal open={!!linkTarget} title="AI 컨텍스트 연결 (§21.6)"
        onClose={() => setLinkTarget(null)}
        footer={<ModalFooter onCancel={() => setLinkTarget(null)} onConfirm={() => { linkContext(linkTarget, linkSessionId); setLinkTarget(null) }} confirmLabel="연결" />}>
        <div className="space-y-2">
          <p className="text-[11px] text-gray-500">메시지를 세션/익스체인지 증거에 연결합니다.</p>
          <input className="input text-xs w-full" placeholder="세션 ID" value={linkSessionId} onChange={e => setLinkSessionId(e.target.value)} />
        </div>
      </Modal>

      {/* Edit message */}
      <Modal open={!!editingMsg} title="메시지 수정"
        onClose={() => setEditingMsg(null)}
        footer={<ModalFooter onCancel={() => setEditingMsg(null)} onConfirm={saveEdit} confirmLabel="저장" />}>
        <textarea className="input text-xs w-full" rows={3} value={editText} onChange={e => setEditText(e.target.value)} />
      </Modal>

      {/* Moderation delete confirm (PAT-1512): reason required, audited */}
      <Modal open={!!delTarget} title="메시지 중재 삭제" size="sm"
        onClose={() => { setDelTarget(null); setDelReason('') }}
        footer={<ModalFooter onCancel={() => { setDelTarget(null); setDelReason('') }} onConfirm={confirmDelete} confirmLabel="삭제 확정" danger />}>
        <div className="space-y-2 text-xs">
          <p className="text-gray-500">작성자 메시지가 아닌 중재 삭제입니다. 사유를 입력하면 감사 로그에 기록됩니다.</p>
          <textarea className="input text-xs w-full" rows={2} placeholder="중재 삭제 사유 (필수)" value={delReason} onChange={e => setDelReason(e.target.value)} />
        </div>
      </Modal>
    </div>
  )
}
