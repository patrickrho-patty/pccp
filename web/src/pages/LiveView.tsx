import { useState, useEffect, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'

type SessionActivity = {
  id: string
  title: string
  user: string
  userEmail: string
  userId: string
  model: string
  riskScore: number
  status: string
  lastActivity: string
  tokenIn: number
  tokenOut: number
  messages: { time: string; text: string; type: 'user' | 'ai' | 'system' }[]
}

export default function LiveView() {
  const [sessions, setSessions] = useState<SessionActivity[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [connected, setConnected] = useState(false)
  const [sseSource, setSseSource] = useState<EventSource | null>(null)
  const [allSessions, setAllSessions] = useState<any[]>([])
  const [allUsers, setAllUsers] = useState<any[]>([])
  const [allHarnesses, setAllHarnesses] = useState<any[]>([])
  const pollRef = useRef<number | null>(null)

  // Load base data
  const loadBaseData = useCallback(() => {
    const headers = authHeaders()
    Promise.all([
      fetch('/api/sessions', { headers }).then(r => r.json()).catch(() => []),
      fetch('/api/users', { headers }).then(r => r.json()).catch(() => []),
      fetch('/api/harnesses', { headers }).then(r => r.json()).catch(() => []),
    ]).then(([sess, users, harnesses]) => {
      setAllSessions(Array.isArray(sess) ? sess : [])
      setAllUsers(Array.isArray(users) ? users : [])
      setAllHarnesses(Array.isArray(harnesses) ? harnesses : [])
    })
  }, [])

  // Try SSE first, fall back to polling
  useEffect(() => {
    loadBaseData()

    const token = localStorage.getItem('pccp_token')
    let sse: EventSource | null = null

    try {
      sse = new EventSource(`/api/realtime/sse?token=${encodeURIComponent(token || '')}`)
      sse.onopen = () => setConnected(true)
      sse.onerror = () => { setConnected(false); sse?.close() }
      sse.addEventListener('session.update', (e) => {
        try { const data = JSON.parse(e.data); loadBaseData() } catch {}
      })
      setSseSource(sse)
    } catch {
      // SSE not available, use polling
    }

    // Always poll as backup (every 5 seconds for real data)
    pollRef.current = window.setInterval(() => {
      loadBaseData()
    }, 5000)

    return () => {
      sse?.close()
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  // Transform real sessions into live activity cards
  useEffect(() => {
    const active = allSessions.filter(s => s.status === 'active' || s.status === 'paused')
    const cards: SessionActivity[] = active.map(s => {
      const user = allUsers.find(u => u.id === s.user_id)
      const harness = allHarnesses.find(h => h.harness_id === s.harness_id)
      return {
        id: s.id,
        title: s.title || '제목 없음',
        user: user?.name_ko || user?.name || '알 수 없음',
        userEmail: user?.email || '-',
        userId: s.user_id || '',
        model: s.model_class || '-',
        riskScore: harness?.risk_state === 'high' ? 0.9 : harness?.risk_state === 'elevated' ? 0.6 : 0.1,
        status: s.status,
        lastActivity: s.last_activity || s.opened_at || '',
        tokenIn: 0,
        tokenOut: 0,
        messages: [],
      }
    })
    setSessions(cards)
  }, [allSessions, allUsers, allHarnesses])

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">실시간 뷰 <span className="text-gray-400 text-lg font-normal">Live Sessions</span></h1>
          <p className="text-xs text-gray-400 mt-1">
            활성 AI 세션 실시간 모니터링 · {connected ? '🟢 SSE 연결됨' : '🟡 폴링 모드 (5초)'} · {sessions.length}개 활성 세션
          </p>
        </div>
        <Link to="/sessions" className="btn-secondary text-sm">전체 세션 →</Link>
      </div>

      {sessions.length === 0 ? (
        <div className="card text-center py-16">
          <div className="text-4xl mb-3">📡</div>
          <p className="text-gray-500">현재 활성 세션이 없습니다</p>
          <p className="text-xs text-gray-400 mt-1">하네스가 세션을 시작하면 여기에 실시간으로 표시됩니다</p>
        </div>
      ) : selectedId ? (
        <LiveDetail
          session={sessions.find(s => s.id === selectedId)}
          onBack={() => setSelectedId(null)}
        />
      ) : (
        <div className="grid grid-cols-3 gap-4">
          {sessions.map(s => (
            <div key={s.id} className="card cursor-pointer hover:shadow-md transition-shadow"
              onClick={() => setSelectedId(s.id)}>
              {/* Header */}
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className={`w-2 h-2 rounded-full ${s.status === 'active' ? 'bg-green-500 animate-pulse' : 'bg-yellow-500'}`} />
                  <Link to="/sessions" className="text-sm font-medium text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>
                    {s.title}
                  </Link>
                </div>
                <span className={`text-xs ${s.riskScore >= 0.8 ? 'text-red-600' : s.riskScore >= 0.5 ? 'text-yellow-600' : 'text-green-600'}`}>
                  위험도 {(s.riskScore * 100).toFixed(0)}%
                </span>
              </div>
              {/* User info */}
              <div className="text-xs text-gray-500 mb-2">
                <Link to="/users" className="text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>
                  {s.user}
                </Link>
                <span className="text-gray-400 ml-1">· {s.model}</span>
              </div>
              {/* Activity bar */}
              <div className="bg-gray-900 rounded p-3 font-mono text-[11px] text-gray-300 min-h-[60px]">
                <div className="text-green-400">▸ 세션 활성 · {s.status}</div>
                <div className="text-gray-500 mt-1">마지막 활동: {s.lastActivity?.slice(11, 19) || 'N/A'}</div>
                <div className="text-gray-500">모델: {s.model}</div>
              </div>
              {/* Footer */}
              <div className="flex items-center justify-between mt-2 text-xs">
                <Link to="/fleet" className="text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>플릿에서 관리 →</Link>
                <span className="text-gray-400">{new Date(s.lastActivity || Date.now()).toLocaleTimeString('ko-KR')}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function LiveDetail({ session, onBack }: { session: SessionActivity | undefined; onBack: () => void }) {
  if (!session) return null
  return (
    <div>
      <button onClick={onBack} className="btn-secondary text-sm mb-4">← 목록으로</button>
      <div className="card mb-4">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h2 className="text-lg font-bold">{session.title}</h2>
            <p className="text-xs text-gray-400">
              <Link to="/users" className="text-blue-600 hover:underline">{session.user}</Link>
              {' · '}
              <Link to="/sessions" className="text-blue-600 hover:underline">{session.id.slice(0, 20)}</Link>
            </p>
          </div>
          <div className="flex items-center gap-3">
            <span className={`badge ${session.status === 'active' ? 'badge-green' : 'badge-yellow'}`}>{session.status}</span>
            <span className={`text-sm ${session.riskScore >= 0.8 ? 'text-red-600' : session.riskScore >= 0.5 ? 'text-yellow-600' : 'text-green-600'}`}>
              위험도 {(session.riskScore * 100).toFixed(0)}%
            </span>
          </div>
        </div>
        <div className="grid grid-cols-4 gap-4 text-sm">
          <div><span className="text-gray-500">사용자:</span> <Link to="/users" className="text-blue-600 hover:underline">{session.user}</Link></div>
          <div><span className="text-gray-500">모델:</span> {session.model}</div>
          <div><span className="text-gray-500">마지막 활동:</span> {session.lastActivity?.slice(0, 19) || '-'}</div>
          <div><span className="text-gray-500">상태:</span> {session.status}</div>
        </div>
      </div>
      <div className="card">
        <h3 className="text-sm font-semibold mb-3">세션 활동</h3>
        <div className="bg-gray-900 rounded p-4 font-mono text-xs text-gray-300 min-h-[300px] overflow-y-auto">
          <div className="text-green-400 mb-2">▸ 세션 {session.status === 'active' ? '실행 중' : '일시정지'}</div>
          <div className="text-gray-500">이메일: {session.userEmail}</div>
          <div className="text-gray-500">모델: {session.model}</div>
          <div className="text-gray-500">마지막 활동: {session.lastActivity?.slice(0, 19) || 'N/A'}</div>
          <div className="mt-3 text-gray-600 border-t border-gray-800 pt-3">
            실시간 활동 스트림은 SSE/WebSocket 연결을 통해 표시됩니다.
            <br />연결 상태를 확인하려면 페이지 상단을 참조하세요.
          </div>
        </div>
      </div>
      <div className="flex gap-2 mt-4">
        <Link to="/sessions" className="btn-secondary text-sm">세션 검사기 →</Link>
        <Link to="/fleet" className="btn-secondary text-sm">플릿 관리 →</Link>
        <Link to="/audit" className="btn-secondary text-sm">감사 로그 →</Link>
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
