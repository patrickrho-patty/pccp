import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import {
  isLiveSession, sessionStatusMeta, formatSessionTime, relativeAge, streamFreshness,
} from '../sessionState'

type LiveRow = {
  id: string
  session_id: string
  title: string
  status: string
  is_live: boolean
  user_name?: string
  user_email?: string
  harness_name?: string
  harness_risk?: string
  model_class?: string
  model_package_id?: string
  last_activity_at: string
  opened_at: string
  links: Record<string, string>
  messages: { time: string; text: string; type: 'ai' | 'system' }[]
}

type LiveSnapshot = {
  data: Omit<LiveRow, 'messages'>[]
  active_count: number
  in_progress_count: number
  limit: number
  truncated: boolean
  timezone: string
  server_time: string
}

export default function LiveView() {
	const [searchParams, setSearchParams] = useSearchParams()
	const searchKey = searchParams.toString()
	const filters = useMemo(() => {
		const params = new URLSearchParams(searchKey)
		return {
			user_id: params.get('user_id') || '',
			project_id: params.get('project_id') || '',
			model: params.get('model') || '',
			risk: params.get('risk') || '',
		}
	}, [searchKey])
	const setLiveFilter = useCallback((key: string, value: string) => {
		setSearchParams(previous => {
			const next = new URLSearchParams(previous)
			if (value) next.set(key, value)
			else next.delete(key)
			return next
		}, { replace: true })
	}, [setSearchParams])
  const [sessions, setSessions] = useState<LiveRow[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [connected, setConnected] = useState(false)
  const [lastEventAt, setLastEventAt] = useState<number | null>(null)
  const [activeCount, setActiveCount] = useState(0)
  const [inProgressCount, setInProgressCount] = useState(0)
  const [truncated, setTruncated] = useState(false)
  const [timezone, setTimezone] = useState('Asia/Seoul')
  const [clockSkewMs, setClockSkewMs] = useState(0)
  const [transcriptVisible, setTranscriptVisible] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [, setNowTick] = useState(0)
	const snapshotInFlight = useRef(false)
	const snapshotQueued = useRef(false)
	const snapshotGeneration = useRef(0)
	const filtersRef = useRef(filters)
	filtersRef.current = filters

  const loadSnapshot = useCallback(async () => {
		if (snapshotInFlight.current) {
			snapshotQueued.current = true
			return
		}
		snapshotInFlight.current = true
		try {
			do {
				snapshotQueued.current = false
				const generation = snapshotGeneration.current
				const requestFilters = filtersRef.current
				try {
					const snapshot: LiveSnapshot = await api.liveSessions(50, requestFilters)
					if (generation !== snapshotGeneration.current) {
						snapshotQueued.current = true
						continue
					}
					setSessions(previous => {
						const messages = new Map(previous.map(row => [row.session_id, row.messages]))
						return (snapshot.data || []).map(row => ({ ...row, messages: messages.get(row.session_id) || [] }))
					})
					setActiveCount(snapshot.active_count || 0)
					setInProgressCount(snapshot.in_progress_count || 0)
					setTruncated(Boolean(snapshot.truncated))
					setTimezone(snapshot.timezone || 'Asia/Seoul')
					const serverMs = Date.parse(snapshot.server_time)
					setClockSkewMs(Number.isFinite(serverMs) ? Date.now() - serverMs : 0)
					setError('')
				} catch (err: any) {
					setError(err?.message || '실시간 세션을 불러오지 못했습니다')
				} finally {
					setLoading(false)
				}
				if (snapshotQueued.current) {
					await new Promise(resolve => window.setTimeout(resolve, 1000))
				}
			} while (snapshotQueued.current)
		} finally {
			snapshotInFlight.current = false
		}
	}, [])

	useEffect(() => { setSelectedId(null) }, [searchKey])
	useEffect(() => {
		snapshotGeneration.current += 1
		const timer = window.setTimeout(loadSnapshot, 250)
		return () => clearTimeout(timer)
	}, [searchKey, loadSnapshot])

  useEffect(() => {
    let source: EventSource | null = null
    let reconnectTimer = 0
    let refreshTimer = 0
    let stopped = false
    let lastEventID = ''
    const markFrame = (event: Event) => {
      const id = (event as MessageEvent).lastEventId
      if (id) lastEventID = id
      setConnected(true)
      setLastEventAt(Date.now())
    }
		let snapshotScheduled = false
		let lastSnapshotScheduledAt = 0
    const scheduleSnapshot = () => {
			if (snapshotScheduled) return
			snapshotScheduled = true
			const delay = Math.max(0, 1000 - (Date.now() - lastSnapshotScheduledAt))
			refreshTimer = window.setTimeout(async () => {
				lastSnapshotScheduledAt = Date.now()
				await loadSnapshot()
				snapshotScheduled = false
			}, delay)
    }
    const connect = async () => {
      try {
        const ticket = await api.liveStreamTicket()
        if (stopped) return
        setTranscriptVisible(Boolean(ticket.transcript_visible))
        const cursor = lastEventID ? `&last_event_id=${encodeURIComponent(lastEventID)}` : ''
        source = new EventSource(ticket.stream_url + cursor)
        source.onopen = () => {
          setConnected(true)
          // Refresh only after the stream is registered, closing the gap between
          // the initial snapshot and events emitted while EventSource connects.
          scheduleSnapshot()
        }
        source.onerror = () => {
          source?.close()
          setConnected(false)
          if (!stopped) reconnectTimer = window.setTimeout(connect, 1500)
        }
				const refresh = (event: Event) => { markFrame(event); scheduleSnapshot() }
				const refreshSession = (event: Event) => {
					markFrame(event)
					try {
						const envelope = JSON.parse((event as MessageEvent).data)
						const payload = envelope.payload || envelope
						if (payload.session_id && typeof payload.status === 'string') {
							setSessions(previous => previous.map(row => row.session_id === payload.session_id
								? { ...row, status: payload.status, is_live: isLiveSession({ status: payload.status }), last_activity_at: envelope.time || row.last_activity_at }
								: row))
						}
					} catch {
						// The coalesced snapshot below remains authoritative.
					}
				}
				const refreshExchange = (event: Event) => {
					markFrame(event)
					try {
						const envelope = JSON.parse((event as MessageEvent).data)
						const payload = envelope.payload || envelope
						if (payload.session_id) {
							setSessions(previous => previous.map(row => row.session_id === payload.session_id
								? { ...row, last_activity_at: envelope.time || new Date().toISOString() }
								: row))
						}
					} catch {
						// The 30-second repair snapshot remains authoritative.
					}
				}
				source.addEventListener('heartbeat', markFrame)
				source.addEventListener('session.update', refreshSession)
				source.addEventListener('session.scope_update', refresh)
				source.addEventListener('exchange.update', refreshExchange)
        source.addEventListener('replay.required', refresh)
        source.addEventListener('session.chunk', (event) => {
          markFrame(event)
          try {
            const envelope = JSON.parse((event as MessageEvent).data)
            const payload = envelope.payload || envelope
            if (!payload.session_id || typeof payload.text !== 'string') return
            setSessions(previous => previous.map(row => row.session_id !== payload.session_id ? row : {
              ...row,
              last_activity_at: envelope.time || new Date().toISOString(),
              messages: [...row.messages, { time: envelope.time || new Date().toISOString(), text: payload.text, type: 'ai' as const }].slice(-200),
            }))
          } catch {
            // Invalid frames are ignored; the next bounded snapshot repairs state.
          }
        })
      } catch (err: any) {
        setConnected(false)
        setError(err?.message || '실시간 스트림 권한을 확인하지 못했습니다')
        if (!stopped) reconnectTimer = window.setTimeout(connect, 3000)
      }
    }
    connect()

    const poll = window.setInterval(loadSnapshot, 30_000)
    const tick = window.setInterval(() => setNowTick(value => value + 1), 10_000)
    return () => {
      stopped = true
      source?.close()
      clearTimeout(reconnectTimer)
      clearTimeout(refreshTimer)
      clearInterval(poll)
      clearInterval(tick)
    }
  }, [loadSnapshot])

  const selected = sessions.find(session => session.id === selectedId)
  const freshness = streamFreshness(lastEventAt === null ? null : Date.now() - lastEventAt, connected)
  const skewWarning = Math.abs(clockSkewMs) >= 60_000
  const serverNow = Date.now() - clockSkewMs

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">실시간 뷰 <span className="text-gray-400 text-lg font-normal">Live Sessions</span></h1>
          <p className="text-xs text-gray-500 mt-1">지금 실행 중 {activeCount}개 · 진행 상태 전체(대기·활성·유휴·일시정지) {inProgressCount}개</p>
          <p className={`text-[11px] mt-0.5 ${freshness.ok ? 'text-green-600' : 'text-amber-600'}`}>
            {freshness.ok ? '🟢' : '🟡'} {freshness.label} · 30초 복구 스냅샷
          </p>
          <p className="text-[11px] text-gray-500 mt-0.5">표시 범위: {transcriptVisible ? '대화 조각 및 운영 메타데이터' : '운영 메타데이터만'}</p>
          {skewWarning && <p className="text-[11px] text-red-600 mt-1">관리자 장치와 서버의 시간이 {Math.round(Math.abs(clockSkewMs) / 60000)}분 이상 다릅니다.</p>}
        </div>
        <Link to="/sessions" className="btn-secondary text-sm">전체 세션 →</Link>
      </div>

      {error && <div className="card mb-4 border-red-200 bg-red-50 text-sm text-red-700">{error} <button className="ml-2 underline" onClick={loadSnapshot}>다시 시도</button></div>}
	  <div className="card mb-4 grid grid-cols-1 md:grid-cols-4 gap-3">
		<label className="text-[11px] text-gray-500">사용자 ID
		  <input className="input mt-1 w-full" value={filters.user_id} onChange={event => setLiveFilter('user_id', event.target.value)} placeholder="정확한 사용자 ID" />
		</label>
		<label className="text-[11px] text-gray-500">프로젝트 ID
		  <input className="input mt-1 w-full" value={filters.project_id} onChange={event => setLiveFilter('project_id', event.target.value)} placeholder="정확한 프로젝트 ID" />
		</label>
		<label className="text-[11px] text-gray-500">모델 클래스/패키지
		  <input className="input mt-1 w-full" value={filters.model} onChange={event => setLiveFilter('model', event.target.value)} placeholder="예: code 또는 package ID" />
		</label>
		<label className="text-[11px] text-gray-500">하네스 위험도
		  <select className="input mt-1 w-full" value={filters.risk} onChange={event => setLiveFilter('risk', event.target.value)}>
			<option value="">전체</option><option value="normal">정상</option><option value="low">낮음</option><option value="elevated">주의</option><option value="high">높음</option><option value="critical">심각</option>
		  </select>
		</label>
	  </div>
      {truncated && <div className="card mb-4 border-amber-200 bg-amber-50 text-xs text-amber-800">진행 중 세션 {inProgressCount}개 중 최신 50개를 표시합니다. 전체 이력은 <Link className="underline" to="/sessions">세션 목록</Link>에서 확인하세요.</div>}

      {loading ? (
        <div className="card text-center py-16 text-gray-500">실시간 세션을 불러오는 중입니다…</div>
      ) : selectedId ? (
        <LiveDetail session={selected} timezone={timezone} serverNow={serverNow} transcriptVisible={transcriptVisible} onBack={() => setSelectedId(null)} />
      ) : sessions.length === 0 ? (
        <div className="card text-center py-16">
          <div className="text-4xl mb-3">📡</div>
          <p className="text-gray-500">현재 진행 중인 세션이 없습니다</p>
          <p className="text-xs text-gray-400 mt-1">하네스가 세션을 시작하면 대기·활성·유휴·일시정지 상태가 표시됩니다.</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 xl:grid-cols-3 gap-4">
          {sessions.map(session => {
            const meta = sessionStatusMeta(session.status)
            return (
              <div key={session.id} className="card cursor-pointer hover:shadow-md transition-shadow" onClick={() => setSelectedId(session.id)}>
                <div className="flex items-start justify-between gap-2 mb-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className={`w-2 h-2 shrink-0 rounded-full ${isLiveSession(session) ? 'bg-green-500 animate-pulse' : 'bg-yellow-500'}`} />
                    <EntityLink href={session.links.session} onClick>{session.title || session.session_id}</EntityLink>
                    <span className={`text-[10px] whitespace-nowrap px-2 py-0.5 rounded-full border ${meta.badge}`}>{meta.ko}</span>
                  </div>
                  <Risk value={session.harness_risk} />
                </div>
                <div className="text-xs text-gray-500 mb-2">
                  <EntityLink href={session.links.user} onClick>{session.user_name || '삭제됨/접근 불가'}</EntityLink>
                  {' · '}{session.links.model ? <EntityLink href={session.links.model} onClick>{session.model_class || session.model_package_id || '모델 보기'}</EntityLink> : (session.model_class || '모델 정보 없음')}
                </div>
                <div className="bg-gray-900 rounded p-3 font-mono text-[11px] text-gray-300 min-h-[60px]">
                  <div className={session.is_live ? 'text-green-400' : 'text-yellow-400'}>▸ 세션 {meta.ko} · {session.status}</div>
                  <div className="text-gray-500 mt-1">마지막 활동: {formatSessionTime(session.last_activity_at, timezone)} · {relativeAge(session.last_activity_at, serverNow)}</div>
                  <div className="text-gray-500">하네스: {session.harness_name || '삭제됨/접근 불가'}</div>
                </div>
                <div className="flex items-center justify-between mt-2 text-xs">
                  <EntityLink href={session.links.fleet} onClick>{session.links.fleet ? '플릿에서 관리 →' : '하네스 접근 불가'}</EntityLink>
                  <span className="text-gray-400 font-mono">{session.session_id.slice(0, 18)}</span>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

function EntityLink({ href, children, onClick = false }: { href?: string; children: React.ReactNode; onClick?: boolean }) {
  if (!href) return <span className="text-gray-400">{children}</span>
  return <Link to={href} className="text-blue-600 hover:underline truncate" onClick={onClick ? event => event.stopPropagation() : undefined}>{children}</Link>
}

function Risk({ value }: { value?: string }) {
  const label = value === 'critical' ? '심각' : value === 'high' ? '높음' : value === 'elevated' ? '주의' : value === 'low' ? '낮음' : value === 'normal' ? '정상' : '정보 없음'
  const color = value === 'critical' || value === 'high' ? 'text-red-600' : value === 'elevated' ? 'text-amber-600' : value === 'low' || value === 'normal' ? 'text-green-600' : 'text-gray-400'
  return <span className={`text-xs whitespace-nowrap ${color}`}>위험도 {label}</span>
}

function LiveDetail({ session, timezone, serverNow, transcriptVisible, onBack }: { session?: LiveRow; timezone: string; serverNow: number; transcriptVisible: boolean; onBack: () => void }) {
  if (!session) return (
    <div>
      <button onClick={onBack} className="btn-secondary text-sm mb-4">← 목록으로</button>
      <div className="card text-center py-12 text-gray-500">이 세션은 더 이상 진행 중이 아닙니다. 세션 목록에서 이력을 확인하세요.</div>
    </div>
  )
  const meta = sessionStatusMeta(session.status)
  return (
    <div>
      <button onClick={onBack} className="btn-secondary text-sm mb-4">← 목록으로</button>
      <div className="card mb-4">
        <div className="flex justify-between gap-4 mb-4">
          <div><h2 className="text-lg font-bold">{session.title || session.session_id}</h2><EntityLink href={session.links.session}>{session.session_id}</EntityLink></div>
          <span className={`text-xs px-2 py-1 h-fit rounded-full border ${meta.badge}`}>{meta.ko} ({session.status})</span>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4 text-sm">
          <div><span className="text-gray-500">사용자:</span> <EntityLink href={session.links.user}>{session.user_name || '삭제됨/접근 불가'}</EntityLink></div>
          <div><span className="text-gray-500">하네스:</span> <EntityLink href={session.links.harness}>{session.harness_name || '삭제됨/접근 불가'}</EntityLink></div>
          <div><span className="text-gray-500">모델:</span> <EntityLink href={session.links.model}>{session.model_class || session.model_package_id || '-'}</EntityLink></div>
          <div><span className="text-gray-500">마지막 활동:</span> {formatSessionTime(session.last_activity_at, timezone)} · {relativeAge(session.last_activity_at, serverNow)}</div>
        </div>
      </div>
      <div className="card">
        <h3 className="text-sm font-semibold mb-3">{transcriptVisible ? '실시간 활동 (최근 200개 조각)' : '실시간 운영 메타데이터'}</h3>
        <div className="bg-gray-900 rounded p-4 font-mono text-xs text-gray-300 min-h-[240px] max-h-[480px] overflow-y-auto">
          {session.messages.length === 0 ? <div className="text-gray-500">{transcriptVisible ? '수신된 대화 조각이 없습니다. 상태 및 최종 활동 시간은 위 스냅샷에서 계속 갱신됩니다.' : '이 운영자에게는 대화 내용 열람 권한이 없습니다. 세션 상태와 활동 시각만 표시됩니다.'}</div> : session.messages.map((message, index) => <div key={`${message.time}-${index}`} className="mb-1"><span className="text-gray-600">{formatSessionTime(message.time, timezone)}</span> {message.text}</div>)}
        </div>
      </div>
      <div className="flex gap-2 mt-4">
        <EntityLink href={session.links.session}>세션 상세 →</EntityLink>
        <EntityLink href={session.links.fleet}>플릿 관리 →</EntityLink>
        <Link to="/audit" className="btn-secondary text-sm">감사 로그 →</Link>
      </div>
    </div>
  )
}
