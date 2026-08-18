import { useState, useEffect, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import {
  isInProgressSession, isLiveSession, sessionStatusMeta, sessionLastActivity,
  formatSessionTime, relativeAge, streamFreshness,
} from '../sessionState'

type SessionActivity = {
  id: string
  rawSessionId: string
  harnessId: string
  title: string
  user: string
  userEmail: string
  userId: string
  model: string
  riskScore: number
  status: string
  lastActivity: string // ISO instant (last_activity_at || opened_at)
  tokenIn: number
  tokenOut: number
  messages: { time: string; text: string; type: 'user' | 'ai' | 'system' }[]
}

export default function LiveView() {
  const [sessions, setSessions] = useState<SessionActivity[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [connected, setConnected] = useState(false)
  const [lastEventAt, setLastEventAt] = useState<number | null>(null)
  const [sseSource, setSseSource] = useState<EventSource | null>(null)
  const [allSessions, setAllSessions] = useState<any[]>([])
  const [allUsers, setAllUsers] = useState<any[]>([])
  const [allHarnesses, setAllHarnesses] = useState<any[]>([])
  const [, setNowTick] = useState(0)
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
      sse.onopen = () => { setConnected(true); setLastEventAt(Date.now()) }
      // Do NOT close on error: EventSource auto-reconnects, and onopen
      // fires again — one transient blip must not strand the page in
      // polling mode (PAT-1496 reconnect edge case).
      sse.onerror = () => setConnected(false)
      sse.addEventListener('session.update', (e) => {
        try { JSON.parse(e.data); setLastEventAt(Date.now()); loadBaseData() } catch {}
      })
      // Named heartbeat (server sends every 30s): proves stream health
      // even when no session traffic is flowing.
      sse.addEventListener('heartbeat', () => { setConnected(true); setLastEventAt(Date.now()) })
      // web/21 B: live token chunks from the governed relay stream.
      sse.addEventListener('session.chunk', (e) => {
        try {
          const data = JSON.parse(e.data)
          const payload = data.payload || data
          const sessionId = payload.session_id
          if (!sessionId) return
          setSessions(prev => prev.map(c => {
            if (c.rawSessionId !== sessionId && c.id !== sessionId) return c
            const msgs = [...(c.messages || []), { time: new Date().toISOString(), text: payload.text, type: 'ai' as const }].slice(-200)
            return { ...c, messages: msgs, tokenOut: (c.tokenOut || 0) + (payload.text?.length || 0), lastActivity: new Date().toISOString() }
          }))
        } catch {}
      })
      setSseSource(sse)
    } catch {
      // SSE not available, use polling
    }

    // Always poll as backup (every 5 seconds for real data)
    pollRef.current = window.setInterval(() => {
      loadBaseData()
    }, 5000)
    // Relative ages and stream freshness stay current.
    const tick = window.setInterval(() => setNowTick(n => n + 1), 10_000)

    return () => {
      sse?.close()
      if (pollRef.current) clearInterval(pollRef.current)
      clearInterval(tick)
    }
  }, [])

  // Transform real sessions into live activity cards. Live = active only;
  // idle/paused stay visible as an explicitly-labeled in-progress group
  // (PAT-1496 — a paused session is never counted as active).
  useEffect(() => {
    const trackable = allSessions.filter(isInProgressSession)
    const cards: SessionActivity[] = trackable.map(s => {
      const user = allUsers.find(u => u.id === s.user_id)
      const harness = allHarnesses.find(h => h.harness_id === s.harness_id)
      return {
        id: s.id,
        rawSessionId: s.session_id || s.id,
        harnessId: s.harness_id || '',
        title: s.title || '제목 없음',
        user: user?.name_ko || user?.name || '알 수 없음',
        userEmail: user?.email || '-',
        userId: s.user_id || '',
        model: s.model_class || '-',
        riskScore: harness?.risk_state === 'high' ? 0.9 : harness?.risk_state === 'elevated' ? 0.6 : 0.1,
        status: s.status,
        lastActivity: sessionLastActivity(s),
        tokenIn: 0,
        tokenOut: 0,
        messages: [],
      }
    })
    setSessions(cards)
  }, [allSessions, allUsers, allHarnesses])

  const liveCount = sessions.filter(isLiveSession).length
  const inProgressOnly = sessions.length - liveCount
  const freshness = streamFreshness(connected ? (lastEventAt ? Date.now() - lastEventAt : null) : null)

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">실시간 뷰 <span className="text-gray-400 text-lg font-normal">Live Sessions</span></h1>
          <p className="text-xs text-gray-400 mt-1">
            활성 {liveCount}개
            {inProgressOnly > 0 && <> · 진행 중 {sessions.length}개 <span className="text-amber-600">(유휴/일시정지 {inProgressOnly}개 포함)</span></>}
          </p>
          <p className="text-[11px] text-gray-400 mt-0.5">
            {connected
              ? <>🟢 SSE 연결됨 · 스트림 {freshness.label}</>
              : <>🟡 폴링 모드 (5초) — 실시간 스트림 미연결</>}
            {' · '}세션 상태는 각 카드의 배지를 참조하세요
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
          {sessions.map(s => {
            const meta = sessionStatusMeta(s.status)
            return (
            <div key={s.id} className="card cursor-pointer hover:shadow-md transition-shadow"
              onClick={() => setSelectedId(s.id)}>
              {/* Header */}
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className={`w-2 h-2 rounded-full ${isLiveSession(s) ? 'bg-green-500 animate-pulse' : 'bg-yellow-500'}`} />
                  <Link to={`/sessions/${s.id}`} className="text-sm font-medium text-blue-600 hover:underline" onClick={e => e.stopPropagation()}
                    title="세션 상세 열기">
                    {s.title}
                  </Link>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${meta.badge}`}>{meta.ko}</span>
                </div>
                <span className={`text-xs ${s.riskScore >= 0.8 ? 'text-red-600' : s.riskScore >= 0.5 ? 'text-yellow-600' : 'text-green-600'}`}>
                  위험도 {(s.riskScore * 100).toFixed(0)}%
                </span>
              </div>
              {/* User info */}
              <div className="text-xs text-gray-500 mb-2">
                {s.userId
                  ? <Link to={`/users/${s.userId}`} className="text-blue-600 hover:underline" onClick={e => e.stopPropagation()} title="사용자 상세 열기">{s.user}</Link>
                  : <span>{s.user}</span>}
                {s.model && s.model !== '-'
                  ? <>{' · '}<Link to={`/models?class=${encodeURIComponent(s.model)}`} className="hover:underline" onClick={e => e.stopPropagation()} title="모델 클래스 필터로 이동">{s.model}</Link></>
                  : null}
              </div>
              {/* Activity bar */}
              <div className="bg-gray-900 rounded p-3 font-mono text-[11px] text-gray-300 min-h-[60px]">
                <div className={isLiveSession(s) ? 'text-green-400' : 'text-yellow-400'}>▸ 세션 {meta.ko} · {s.status}</div>
                <div className="text-gray-500 mt-1">마지막 활동: {formatSessionTime(s.lastActivity)} (KST) · {relativeAge(s.lastActivity)}</div>
                <div className="text-gray-500">모델: {s.model}</div>
              </div>
              {/* Footer */}
              <div className="flex items-center justify-between mt-2 text-xs">
                {s.harnessId
                  ? <Link to={`/fleet?harness=${encodeURIComponent(s.harnessId)}`} className="text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>플릿에서 관리 →</Link>
                  : <span className="text-gray-400">하네스 없음</span>}
                <span className="text-gray-400" title="마지막 활동 상대 경과">{relativeAge(s.lastActivity)}</span>
              </div>
            </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

function LiveDetail({ session, onBack }: { session: SessionActivity | undefined; onBack: () => void }) {
  if (!session) {
    // The session left the in-progress set while its detail was open
    // (e.g. closed via SSE) — offer an explicit way back, not a blank.
    return (
      <div>
        <button onClick={onBack} className="btn-secondary text-sm mb-4">← 목록으로</button>
        <div className="card text-center py-12">
          <div className="text-3xl mb-2">ℹ️</div>
          <p className="text-gray-500 text-sm">이 세션은 더 이상 진행 중이 아닙니다 (종료/강제종료)</p>
          <p className="text-xs text-gray-400 mt-1">세션 목록에서 이력을 확인할 수 있습니다</p>
        </div>
      </div>
    )
  }
  const meta = sessionStatusMeta(session.status)
  return (
    <div>
      <button onClick={onBack} className="btn-secondary text-sm mb-4">← 목록으로</button>
      <div className="card mb-4">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h2 className="text-lg font-bold">{session.title}</h2>
            <p className="text-xs text-gray-400">
              {session.userId
                ? <Link to={`/users/${session.userId}`} className="text-blue-600 hover:underline">{session.user}</Link>
                : <span>{session.user}</span>}
              {' · '}
              <Link to={`/sessions/${session.id}`} className="text-blue-600 hover:underline font-mono" title="세션 상세 열기">{session.rawSessionId || session.id.slice(0, 20)}</Link>
            </p>
          </div>
          <div className="flex items-center gap-3">
            <span className={`badge ${session.status === 'active' ? 'badge-green' : 'badge-yellow'}`}>{meta.ko} ({session.status})</span>
            <span className={`text-sm ${session.riskScore >= 0.8 ? 'text-red-600' : session.riskScore >= 0.5 ? 'text-yellow-600' : 'text-green-600'}`}>
              위험도 {(session.riskScore * 100).toFixed(0)}%
            </span>
          </div>
        </div>
        <div className="grid grid-cols-4 gap-4 text-sm">
          <div><span className="text-gray-500">사용자:</span> {session.userId
            ? <Link to={`/users/${session.userId}`} className="text-blue-600 hover:underline">{session.user}</Link>
            : <span>{session.user}</span>}</div>
          <div><span className="text-gray-500">모델:</span> {session.model && session.model !== '-'
            ? <Link to={`/models?class=${encodeURIComponent(session.model)}`} className="text-blue-600 hover:underline">{session.model}</Link>
            : <span>{session.model || '-'}</span>}</div>
          <div><span className="text-gray-500">마지막 활동:</span> {formatSessionTime(session.lastActivity)} (KST)</div>
          <div><span className="text-gray-500">상태:</span> {meta.ko} ({session.status})</div>
        </div>
      </div>
      <div className="card">
        <h3 className="text-sm font-semibold mb-3">세션 활동</h3>
        <div className="bg-gray-900 rounded p-4 font-mono text-xs text-gray-300 min-h-[300px] overflow-y-auto">
          <div className={isLiveSession(session) ? 'text-green-400' : 'text-yellow-400'}>▸ 세션 {meta.ko}</div>
          <div className="text-gray-500">이메일: {session.userEmail}</div>
          <div className="text-gray-500">모델: {session.model}</div>
          <div className="text-gray-500">마지막 활동: {formatSessionTime(session.lastActivity)} (KST) · {relativeAge(session.lastActivity)}</div>
          <div className="mt-3 text-gray-600 border-t border-gray-800 pt-3">
            실시간 활동 스트림은 SSE/WebSocket 연결을 통해 표시됩니다.
            <br />연결 상태를 확인하려면 페이지 상단을 참조하세요.
          </div>
        </div>
      </div>
      <div className="flex gap-2 mt-4">
        <Link to={`/sessions/${session.id}`} className="btn-secondary text-sm">세션 상세 →</Link>
        {session.harnessId
          ? <Link to={`/fleet?harness=${encodeURIComponent(session.harnessId)}`} className="btn-secondary text-sm">플릿 관리 →</Link>
          : <Link to="/fleet" className="btn-secondary text-sm">플릿 관리 →</Link>}
        <Link to="/audit" className="btn-secondary text-sm">감사 로그 →</Link>
      </div>
    </div>
  )
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
