import { useState, useEffect, useRef } from 'react'
import EmptyState from '../components/EmptyState'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'

export default function Communications() {
  const [tab, setTab] = useState<'chat' | 'broadcast' | 'files' | 'presence'>('chat')
  const [conversations, setConversations] = useState<any[]>([])
  const [messages, setMessages] = useState<any[]>([])
  const [selectedConv, setSelectedConv] = useState<string | null>(null)
  const [newMessage, setNewMessage] = useState('')
  const [broadcasts, setBroadcasts] = useState<any[]>([])
  const [showBroadcast, setShowBroadcast] = useState(false)
  const [showNewConv, setShowNewConv] = useState(false)
  const [bcForm, setBcForm] = useState({ severity: 'info', title: '', title_ko: '', body: '', body_ko: '', target_type: 'all', requires_ack: false })
  const [convForm, setConvForm] = useState({ type: 'group', title: '', participantIds: [] as string[] })
  const [users, setUsers] = useState<any[]>([])
  const [presence, setPresence] = useState<any[]>([])
  const [files, setFiles] = useState<any[]>([])
  const [showFileTransfer, setShowFileTransfer] = useState(false)
  const [fileForm, setFileForm] = useState({ recipient_id: '', file_name: '', classification: 'internal' })
  const msgEndRef = useRef<HTMLDivElement>(null)

  const authHeaders = () => { const t = localStorage.getItem('pccp_token'); return t ? { Authorization: `Bearer ${t}` } : {} }

  const load = () => {
    fetch('/api/communications/conversations', { headers: authHeaders() })
      .then(r => r.json()).then(data => setConversations(Array.isArray(data) ? data : [])).catch(() => {})
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
    fetch('/api/communications/presence', { headers: authHeaders() })
      .then(r => r.json()).then(data => setPresence(Array.isArray(data) ? data : [])).catch(() => {})
    fetch('/api/communications/broadcasts', { headers: authHeaders() })
      .then(r => r.json()).then(data => setBroadcasts(Array.isArray(data) ? data : [])).catch(() => {})
  }

  useEffect(() => {
    load()
    const interval = setInterval(load, 5000) // Poll for new messages/broadcasts
    return () => clearInterval(interval)
  }, [])

  // Auto-scroll to bottom on new messages
  useEffect(() => { msgEndRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [messages])

  const loadMessages = (convId: string) => {
    setSelectedConv(convId)
    fetch(`/api/communications/conversations/${convId}/messages`, { headers: authHeaders() })
      .then(r => r.json()).then(data => setMessages(Array.isArray(data) ? data : [])).catch(() => setMessages([]))
  }

  // Reload messages periodically when a conversation is selected
  useEffect(() => {
    if (!selectedConv) return
    loadMessages(selectedConv)
    const interval = setInterval(() => loadMessages(selectedConv), 3000)
    return () => clearInterval(interval)
  }, [selectedConv])

  const sendMessage = async () => {
    if (!newMessage.trim() || !selectedConv) return
    try {
      await fetch(`/api/communications/conversations/${selectedConv}/messages`, {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ sender_id: 'admin', sender_type: 'user', content_type: 'text', content: newMessage }),
      })
      setNewMessage('')
      showToast('메시지 전송됨', 'success')
      loadMessages(selectedConv)
    } catch {}
  }

  const createConversation = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!convForm.title || convForm.participantIds.length === 0) { showToast('제목과 참여자를 입력하세요'); return }
    try {
      const res = await fetch('/api/communications/conversations', {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: convForm.type, title: convForm.title, participants: convForm.participantIds }),
      })
      if (res.ok) { setShowNewConv(false); setConvForm({ type: 'group', title: '', participantIds: [] }); load() }
    } catch {}
  }

  const sendBroadcast = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await fetch('/api/communications/broadcasts', {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify(bcForm),
      })
      setShowBroadcast(false)
      setBcForm({ severity: 'info', title: '', title_ko: '', body: '', body_ko: '', target_type: 'all', requires_ack: false })
      load()
    } catch {}
  }

  const createFileTransfer = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await fetch('/api/communications/file-transfers', {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...fileForm, sender_id: 'admin' }),
      })
      setShowFileTransfer(false)
      setFileForm({ recipient_id: '', file_name: '', classification: 'internal' })
      load()
    } catch {}
  }

  const getUserName = (id: string) => users.find(u => u.id === id)?.name_ko || users.find(u => u.id === id)?.name || id?.slice(0, 8)

  const sevBadge = (s: string) => s === 'critical' || s === 'emergency' ? 'badge-red' : s === 'warning' ? 'badge-yellow' : 'badge-blue'
  const sevLabel = (s: string) => s === 'critical' || s === 'emergency' ? '🔴 긴급' : s === 'warning' ? '⚠️ 경고' : 'ℹ️ 정보'

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">커뮤니케이션 허브 <span className="text-gray-400 text-lg font-normal">Communications Hub</span></h1>
      <p className="text-xs text-gray-400 mb-6">실시간 채팅 · 방송 · 파일 전송 · 프레전스 · PRD §21-22</p>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'chat', label: '채팅', en: 'Chat', count: conversations.length },
          { id: 'broadcast', label: '방송', en: 'Broadcast', count: broadcasts.length },
          { id: 'files', label: '파일 전송', en: 'File Transfer' },
          { id: 'presence', label: '프레전스', en: 'Presence', count: presence.length },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && <span className="text-xs text-gray-400">({t.count})</span>}
          </button>
        ))}
      </div>

      {/* CHAT TAB */}
      {tab === 'chat' && (
        <div className="flex gap-4" style={{ height: 'calc(100vh - 250px)' }}>
          {/* Conversation list */}
          <div className="w-64 flex flex-col">
            <button onClick={() => setShowNewConv(!showNewConv)} className="btn-primary text-sm mb-3">+ 새 대화</button>

            {showNewConv && (
              <form onSubmit={createConversation} className="card mb-3 p-3">
                <input className="input text-sm mb-2" placeholder="대화 제목" value={convForm.title} onChange={e => setConvForm({ ...convForm, title: e.target.value })} />
                <select className="input text-sm mb-2" value={convForm.type} onChange={e => setConvForm({ ...convForm, type: e.target.value })}>
                  <option value="direct">1:1</option>
                  <option value="group">그룹</option>
                  <option value="channel">채널</option>
                </select>
                <div className="max-h-32 overflow-y-auto mb-2">
                  {users.map(u => (
                    <label key={u.id} className="flex items-center gap-2 text-xs py-1">
                      <input type="checkbox" checked={convForm.participantIds.includes(u.id)}
                        onChange={e => setConvForm({ ...convForm, participantIds: e.target.checked ? [...convForm.participantIds, u.id] : convForm.participantIds.filter(id => id !== u.id) })} />
                      {u.name_ko || u.name}
                    </label>
                  ))}
                </div>
                <button type="submit" className="btn-primary text-xs w-full">생성</button>
              </form>
            )}

            <div className="flex-1 overflow-y-auto">
              {conversations.map(c => (
                <div key={c.id} onClick={() => loadMessages(c.id)}
                  className={`p-3 rounded cursor-pointer mb-1 ${selectedConv === c.id ? 'bg-blue-50 border-l-2 border-blue-400' : 'hover:bg-gray-50'}`}>
                  <div className="flex items-center gap-2">
                    <span className={`w-2 h-2 rounded-full ${c.type === 'direct' ? 'bg-blue-400' : c.type === 'channel' ? 'bg-purple-400' : 'bg-green-400'}`} />
                    <span className="text-sm font-medium truncate">{c.title || '제목 없음'}</span>
                  </div>
                  <div className="text-xs text-gray-400 ml-4">{c.type}</div>
                </div>
              ))}
              {conversations.length === 0 && <p className="text-xs text-gray-400 text-center py-4">대화가 없습니다</p>}
            </div>
          </div>

          {/* Message area */}
          <div className="flex-1 flex flex-col card">
            {selectedConv ? (
              <>
                <div className="p-3 border-b border-gray-100">
                  <h3 className="text-sm font-semibold">{conversations.find(c => c.id === selectedConv)?.title || '대화'}</h3>
                </div>
                <div className="flex-1 overflow-y-auto p-4 space-y-2">
                  {messages.map(m => (
                    <div key={m.id} className={`flex ${m.sender_type === 'user' ? 'justify-end' : 'justify-start'}`}>
                      <div className={`max-w-[70%] rounded-lg px-3 py-2 ${m.sender_type === 'user' ? 'bg-blue-600 text-white' : 'bg-gray-100'}`}>
                        <div className="text-xs opacity-70 mb-0.5">{getUserName(m.sender_id)} · {m.sender_type}</div>
                        <div className="text-sm">{m.content}</div>
                        <div className={`text-[10px] mt-0.5 ${m.sender_type === 'user' ? 'text-blue-200' : 'text-gray-400'}`}>
                          {m.created_at?.slice(11, 16)}
                          {m.session_id && <Link to="/sessions" className="ml-2 underline">AI 세션 연결</Link>}
                        </div>
                      </div>
                    </div>
                  ))}
                  {messages.length === 0 && <p className="text-center text-gray-400 text-sm py-8">메시지가 없습니다</p>}
                  <div ref={msgEndRef} />
                </div>
                <div className="p-3 border-t border-gray-100 flex gap-2">
                  <input className="input flex-1 text-sm" value={newMessage} onChange={e => setNewMessage(e.target.value)}
                    onKeyDown={e => e.key === 'Enter' && sendMessage()} placeholder="메시지 입력..." />
                  <button onClick={sendMessage} className="btn-primary text-sm">전송</button>
                </div>
              </>
            ) : (
              <div className="flex-1 flex items-center justify-center">
                <p className="text-gray-400">대화를 선택하세요</p>
              </div>
            )}
          </div>
        </div>
      )}

      {/* BROADCAST TAB */}
      {tab === 'broadcast' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <p className="text-xs text-gray-400">조직 전체 방송 · 긴급 공지 · 유지보수 안내 · PRD §22</p>
            <button onClick={() => setShowBroadcast(!showBroadcast)} className="btn-primary text-sm">+ 방송 보내기</button>
          </div>

          {showBroadcast && (
            <form onSubmit={sendBroadcast} className="card mb-4 p-4">
              <div className="grid grid-cols-3 gap-3">
                <div><label className="label">심각도</label>
                  <select className="input" value={bcForm.severity} onChange={e => setBcForm({ ...bcForm, severity: e.target.value })}>
                    <option value="info">ℹ️ 정보</option><option value="warning">⚠️ 경고</option><option value="critical">🔴 긴급</option><option value="emergency">🔴 비상</option>
                  </select>
                </div>
                <div><label className="label">대상</label>
                  <select className="input" value={bcForm.target_type} onChange={e => setBcForm({ ...bcForm, target_type: e.target.value })}>
                    <option value="all">전체</option><option value="project">특정 프로젝트</option><option value="team">특정 팀</option>
                  </select>
                </div>
                <div className="flex items-end"><label className="flex items-center gap-2 text-sm cursor-pointer pb-2">
                  <input type="checkbox" checked={bcForm.requires_ack} onChange={e => setBcForm({ ...bcForm, requires_ack: e.target.checked })} className="w-4 h-4" /> 확인 필요</label>
                </div>
              </div>
              <input className="input mt-3" placeholder="제목 · Title" value={bcForm.title} onChange={e => setBcForm({ ...bcForm, title: e.target.value })} required />
              <input className="input mt-2" placeholder="한글 제목 · Korean Title" value={bcForm.title_ko} onChange={e => setBcForm({ ...bcForm, title_ko: e.target.value })} />
              <textarea className="input mt-2" rows={3} placeholder="내용 · Body" value={bcForm.body} onChange={e => setBcForm({ ...bcForm, body: e.target.value })} required />
              <textarea className="input mt-2" rows={2} placeholder="한글 내용 · Korean Body" value={bcForm.body_ko} onChange={e => setBcForm({ ...bcForm, body_ko: e.target.value })} />
              <button type="submit" className="btn-primary text-sm mt-3">방송 전송</button>
            </form>
          )}

          <div className="space-y-2">
            {broadcasts.map(bc => (
              <div key={bc.id} className={`card border-l-4 ${bc.severity === 'critical' || bc.severity === 'emergency' ? 'border-l-red-500' : bc.severity === 'warning' ? 'border-l-yellow-500' : 'border-l-blue-500'}`}>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <span className={sevBadge(bc.severity)}>{sevLabel(bc.severity)}</span>
                    <div>
                      <div className="text-sm font-medium">{bc.title_ko || bc.title}</div>
                      <div className="text-xs text-gray-400">{bc.body_ko || bc.body}</div>
                    </div>
                  </div>
                  <div className="text-xs text-gray-400">
                    {bc.requires_ack && <span className="badge-yellow mr-2">확인 필요</span>}
                    {bc.created_at?.slice(0, 16)}
                  </div>
                </div>
              </div>
            ))}
            {broadcasts.length === 0 && <EmptyState icon="📢" title="방송 내역이 없습니다" message="방송을 보내면 여기에 표시됩니다" />}
          </div>
        </div>
      )}

      {/* FILES TAB */}
      {tab === 'files' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <p className="text-xs text-gray-400">보안 파일 전송 · 분류별 권한 관리 · PRD §23</p>
            <button onClick={() => setShowFileTransfer(!showFileTransfer)} className="btn-primary text-sm">+ 파일 전송</button>
          </div>

          {showFileTransfer && (
            <form onSubmit={createFileTransfer} className="card mb-4 p-4">
              <div className="grid grid-cols-3 gap-3">
                <div><label className="label">받는 사람</label>
                  <select className="input" value={fileForm.recipient_id} onChange={e => setFileForm({ ...fileForm, recipient_id: e.target.value })} required>
                    <option value="">선택...</option>
                    {users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name}</option>)}
                  </select>
                </div>
                <div><label className="label">파일명</label>
                  <input className="input" value={fileForm.file_name} onChange={e => setFileForm({ ...fileForm, file_name: e.target.value })} placeholder="report.pdf" required />
                </div>
                <div><label className="label">분류</label>
                  <select className="input" value={fileForm.classification} onChange={e => setFileForm({ ...fileForm, classification: e.target.value })}>
                    <option value="public">공개</option><option value="internal">내부</option>
                    <option value="confidential">기밀</option><option value="restricted">제한</option>
                  </select>
                </div>
              </div>
              <button type="submit" className="btn-primary text-sm mt-3">전송</button>
            </form>
          )}

          <div className="card">
            {files.length === 0 ? <p className="text-gray-400 text-center py-8">파일 전송 내역이 없습니다</p> : (
              <table className="w-full">
                <thead><tr className="border-b text-left text-xs text-gray-500">
                  <th className="pb-2">파일</th><th className="pb-2">보낸 사람</th><th className="pb-2">받는 사람</th>
                  <th className="pb-2">분류</th><th className="pb-2">상태</th><th className="pb-2">시간</th>
                </tr></thead>
                <tbody>
                  {files.map(f => (
                    <tr key={f.id} className="border-b border-gray-100 last:border-0">
                      <td className="py-2 text-sm">{f.file_name}</td>
                      <td className="py-2 text-xs">{getUserName(f.sender_id)}</td>
                      <td className="py-2 text-xs">{getUserName(f.recipient_id)}</td>
                      <td className="py-2"><span className="badge-gray">{f.classification}</span></td>
                      <td className="py-2"><span className={f.status === 'completed' ? 'badge-green' : 'badge-yellow'}>{f.status}</span></td>
                      <td className="py-2 text-xs text-gray-400">{f.created_at?.slice(0, 16)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}

      {/* PRESENCE TAB */}
      {tab === 'presence' && (
        <div>
          <p className="text-xs text-gray-400 mb-4">사용자 프레전스 · 온라인 상태 · 현재 활동 · PRD §21.3</p>
          <div className="grid grid-cols-4 gap-3">
            {presence.map(p => (
              <div key={p.id || p.user_id} className="card">
                <div className="flex items-center gap-3">
                  <span className={`w-3 h-3 rounded-full ${p.status === 'online' ? 'bg-green-500' : p.status === 'away' ? 'bg-yellow-500' : 'bg-gray-400'}`} />
                  <div>
                    <Link to="/users" className="text-sm font-medium text-blue-600 hover:underline">{getUserName(p.user_id)}</Link>
                    <div className="text-xs text-gray-400">{p.status || 'offline'}</div>
                  </div>
                </div>
                {p.activity && <div className="text-xs text-gray-500 mt-1">📍 {p.activity}</div>}
                {p.harness_id && <Link to="/harnesses" className="text-xs text-blue-600 hover:underline mt-1 block">하네스: {p.harness_id.slice(0, 15)}</Link>}
              </div>
            ))}
            {presence.length === 0 && <div className="col-span-4 text-center py-12 text-gray-400">프레전스 정보가 없습니다</div>}
          </div>
        </div>
      )}
    </div>
  )
}
