import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Communications() {
  const [tab, setTab] = useState<'chat' | 'broadcast' | 'files'>('chat')
  const [conversations, setConversations] = useState<any[]>([])
  const [messages, setMessages] = useState<any[]>([])
  const [selectedConv, setSelectedConv] = useState<string | null>(null)
  const [newMessage, setNewMessage] = useState('')
  const [broadcasts, setBroadcasts] = useState<any[]>([])
  const [showBroadcast, setShowBroadcast] = useState(false)
  const [bcForm, setBcForm] = useState({ severity: 'info', title: '', title_ko: '', body: '', body_ko: '', target_type: 'all' })
  const [users, setUsers] = useState<any[]>([])

  const load = () => {
    fetch('/api/communications/conversations', { headers: authHeaders() })
      .then(r => r.json()).then(data => setConversations(Array.isArray(data) ? data : [])).catch(() => {})
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
  }

  useEffect(() => { load() }, [])

  const loadMessages = (convId: string) => {
    setSelectedConv(convId)
    fetch(`/api/communications/conversations/${convId}/messages`, { headers: authHeaders() })
      .then(r => r.json()).then(data => setMessages(Array.isArray(data) ? data : [])).catch(() => setMessages([]))
  }

  const sendMessage = async () => {
    if (!newMessage.trim() || !selectedConv) return
    try {
      await fetch(`/api/communications/conversations/${selectedConv}/messages`, {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ sender_id: 'admin', sender_type: 'user', content: newMessage }),
      })
      setNewMessage('')
      loadMessages(selectedConv)
    } catch {}
  }

  const createConversation = async () => {
    try {
      const res = await fetch('/api/communications/conversations', {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'direct', title: '새 대화', participants: users.map(u => u.id) }),
      })
      if (res.ok) { load() }
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
      setBcForm({ severity: 'info', title: '', title_ko: '', body: '', body_ko: '', target_type: 'all' })
    } catch {}
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">커뮤니케이션 허브 <span className="text-gray-400 text-lg font-normal">Communications Hub</span></h1>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'chat', label: '채팅', labelEn: 'Chat' },
          { id: 'broadcast', label: '방송', labelEn: 'Broadcast' },
          { id: 'files', label: '파일 전송', labelEn: 'File Transfer' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              tab === t.id ? 'border-blue-600 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} <span className="text-xs text-gray-400">{t.labelEn}</span>
          </button>
        ))}
      </div>

      {/* Chat Tab */}
      {tab === 'chat' && (
        <div className="flex gap-4 h-[600px]">
          {/* Conversation List */}
          <div className="w-64 card overflow-y-auto main-scroll">
            <div className="flex justify-between items-center mb-3">
              <h3 className="text-sm font-semibold">대화 목록</h3>
              <button onClick={createConversation} className="text-blue-600 text-xs hover:underline">+ 새 대화</button>
            </div>
            {conversations.length === 0 ? (
              <p className="text-xs text-gray-400 text-center py-4">대화가 없습니다</p>
            ) : conversations.map(c => (
              <div key={c.id} onClick={() => loadMessages(c.id)}
                className={`p-2 rounded cursor-pointer mb-1 ${selectedConv === c.id ? 'bg-blue-50' : 'hover:bg-gray-50'}`}>
                <div className="text-sm font-medium">{c.title || '제목 없음'}</div>
                <div className="text-xs text-gray-400">{c.type}</div>
              </div>
            ))}
          </div>

          {/* Message Area */}
          <div className="flex-1 card flex flex-col">
            {selectedConv ? (
              <>
                <div className="flex-1 overflow-y-auto main-scroll space-y-2 mb-3">
                  {messages.length === 0 ? (
                    <p className="text-sm text-gray-400 text-center py-8">메시지가 없습니다</p>
                  ) : messages.map(m => (
                    <div key={m.id} className={`flex ${m.sender_type === 'user' ? 'justify-end' : 'justify-start'}`}>
                      <div className={`max-w-[70%] rounded-lg px-3 py-2 text-sm ${
                        m.sender_type === 'user' ? 'bg-blue-600 text-white' : 'bg-gray-100 text-gray-800'}`}>
                        <div className="text-xs opacity-70 mb-0.5">{m.sender_type}</div>
                        <div>{m.content}</div>
                      </div>
                    </div>
                  ))}
                </div>
                <div className="flex gap-2">
                  <input
                    className="input flex-1"
                    placeholder="메시지 입력..."
                    value={newMessage}
                    onChange={e => setNewMessage(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage() } }}
                  />
                  <button onClick={sendMessage} className="btn-primary">전송</button>
                </div>
              </>
            ) : (
              <div className="flex items-center justify-center h-full text-gray-400 text-sm">
                왼쪽에서 대화를 선택하세요
              </div>
            )}
          </div>
        </div>
      )}

      {/* Broadcast Tab */}
      {tab === 'broadcast' && (
        <div>
          <div className="flex justify-between mb-4">
            <h3 className="text-sm font-semibold">방송 관리 · Broadcast Management</h3>
            <button onClick={() => setShowBroadcast(!showBroadcast)} className="btn-primary text-sm">
              {showBroadcast ? '취소' : '+ 방송 생성'}
            </button>
          </div>

          {showBroadcast && (
            <form onSubmit={sendBroadcast} className="card mb-6 space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="label">중요도 · Severity</label>
                  <select className="input" value={bcForm.severity} onChange={e => setBcForm({ ...bcForm, severity: e.target.value })}>
                    <option value="info">정보 · Info</option>
                    <option value="warning">경고 · Warning</option>
                    <option value="critical">중요 · Critical</option>
                    <option value="emergency">긴급 · Emergency</option>
                  </select>
                </div>
                <div>
                  <label className="label">대상 · Target</label>
                  <select className="input" value={bcForm.target_type} onChange={e => setBcForm({ ...bcForm, target_type: e.target.value })}>
                    <option value="all">전체 · All</option>
                    <option value="org">조직 · Organization</option>
                    <option value="project">프로젝트 · Project</option>
                    <option value="user">사용자 · User</option>
                  </select>
                </div>
                <div>
                  <label className="label">제목 · Title</label>
                  <input className="input" value={bcForm.title} onChange={e => setBcForm({ ...bcForm, title: e.target.value })} required />
                </div>
                <div>
                  <label className="label">한글 제목</label>
                  <input className="input" value={bcForm.title_ko} onChange={e => setBcForm({ ...bcForm, title_ko: e.target.value })} placeholder="시스템 점검 안내" />
                </div>
                <div className="col-span-2">
                  <label className="label">내용 · Body</label>
                  <textarea className="input" rows={3} value={bcForm.body} onChange={e => setBcForm({ ...bcForm, body: e.target.value })} required />
                </div>
                <div className="col-span-2">
                  <label className="label">한글 내용</label>
                  <textarea className="input" rows={3} value={bcForm.body_ko} onChange={e => setBcForm({ ...bcForm, body_ko: e.target.value })} placeholder="30분 후 점검이 시작됩니다." />
                </div>
              </div>
              <button type="submit" className="btn-primary">방송 전송 · Send Broadcast</button>
            </form>
          )}

          <div className="card">
            {broadcasts.length === 0 ? (
              <div className="text-center py-8">
                <p className="text-gray-400 mb-2">전송된 방송이 없습니다</p>
                <p className="text-sm text-gray-400">긴급 공지나 시스템 점검 안내를 전송할 수 있습니다.</p>
              </div>
            ) : (
              <table className="w-full">
                <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                  <th className="pb-3">중요도</th><th className="pb-3">제목</th><th className="pb-3">대상</th><th className="pb-3">시간</th>
                </tr></thead>
                <tbody>
                  {broadcasts.map(b => (
                    <tr key={b.id} className="border-b border-gray-100 last:border-0">
                      <td className="py-3"><span className={b.severity === 'emergency' ? 'badge-red' : b.severity === 'critical' ? 'badge-yellow' : 'badge-blue'}>{b.severity}</span></td>
                      <td className="py-3 text-sm">{b.title_ko || b.title}</td>
                      <td className="py-3 text-sm">{b.target_type}</td>
                      <td className="py-3 text-xs text-gray-400">{b.created_at?.slice(0, 19)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}

      {/* Files Tab */}
      {tab === 'files' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">파일 전송 · File Transfer</h3>
          <p className="text-sm text-gray-500 mb-4">
            보안 스캔이 포함된 관리 파일 전송 (§23). DLP 검사, 바이러스 스캔, 분류 태깅 후 전송됩니다.
          </p>
          <div className="border-2 border-dashed border-gray-200 rounded-lg p-8 text-center">
            <p className="text-gray-400 text-sm">파일을 드래그하거나 클릭하여 업로드</p>
            <p className="text-xs text-gray-400 mt-1">최대 100MB · 보안 스씬 후 전송</p>
          </div>
        </div>
      )}
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }
