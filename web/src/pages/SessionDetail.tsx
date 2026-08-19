import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { FavoriteStar } from '../hooks/useFavorites'
import { formatUsageStateInteger, formatUsageAmount, UsageReport, UsageReportData } from '../components/UsageReport'
import { allowedSessionActions, formatSessionTime, SESSION_STATUS_META } from '../sessionState'
import { sessionActionView, changeSetView, findingView, decisionView, replayEventView } from '../evidenceView'

// Canonical session state vocabulary (PAT-1496) — badge-only view of the
// shared table used by Sessions and Live.
const STATUS_META: Record<string, { ko: string; badge: string }> = Object.fromEntries(
  Object.entries(SESSION_STATUS_META).map(([k, v]) => [k, { ko: v.ko, badge: v.badge }])
)

// safeParse best-effort parses JSON payload strings for raw-evidence display.
function safeParse(v: unknown): unknown {
  if (typeof v !== 'string') return v
  try { return JSON.parse(v) } catch { return v }
}

// SessionDetail (web/02 B5) — deep-linkable inspector built on the
// consolidated /detail endpoint (UX6), with the per-exchange decision
// log (B2), replay timeline (B6), cost rollup (B7), and visibility
// badge (B8).
export default function SessionDetail() {
  const { id } = useParams<{ id: string }>()
  const [detail, setDetail] = useState<any>(null)
  const [decisions, setDecisions] = useState<any>(null)
  const [replay, setReplay] = useState<any>(null)
  const [visibility, setVisibility] = useState<any>(null)
  const [usageReport, setUsageReport] = useState<UsageReportData | null>(null)
  const [usageLoading, setUsageLoading] = useState(true)
  const [usageError, setUsageError] = useState(false)
  const [tab, setTab] = useState('timeline')
  const [expandedRaw, setExpandedRaw] = useState<string | null>(null) // evidence key expanded to raw/technical detail (PAT-1498)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
	let active = true
    const controller = new AbortController()
    setLoading(true)
    setUsageReport(null)
    setUsageLoading(true)
		Promise.all([
			api.getSessionDetail(id),
			api.getSessionVisibility(id),
		]).then(([d, vis]) => {
	  if (!active) return
      setDetail(d)
      setVisibility(vis)
	}).catch(() => {
	  if (active) setDetail(null)
	}).finally(() => { if (active) setLoading(false) })
	setUsageError(false)
	api.getSessionUsage(id, '30d', '', controller.signal).then(usage => {
	  if (active) setUsageReport(usage)
	}).catch((error: any) => { if (active && error?.name !== 'AbortError') { setUsageReport(null); setUsageError(true) } }).finally(() => { if (active) setUsageLoading(false) })
	return () => { active = false; controller.abort() }
  }, [id])

	useEffect(() => {
		if (!id || tab !== 'decisions' || decisions) return
		api.getSessionDecisions(id).then(setDecisions).catch(() => setDecisions({ decisions: [] }))
	}, [id, tab, decisions])

	useEffect(() => {
		if (!id || tab !== 'replay' || replay) return
		api.getSessionReplay(id).then(setReplay).catch(() => setReplay({ events: [] }))
	}, [id, tab, replay])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!detail?.session) return (
    <div>
      <Link to="/sessions" className="btn-link">← 세션 목록</Link>
      <p className="text-gray-400 p-8 text-center">세션을 찾을 수 없습니다</p>
    </div>
  )

  const sess = detail.session
	const timezone = detail.timezone || 'Asia/Seoul'
  const meta = STATUS_META[sess.status] || STATUS_META.pending
  const totalTokens = usageReport?.total_tokens
  const totalCost = usageReport?.display_total
  const hasUsageLedger = Boolean(usageReport?.record_count)
  const delayedUsageMeters = (usageReport?.meters || []).filter(meter => meter.state === 'delayed').length

  const act = async (action: string) => {
    await api.sessionAction(id!, action).catch(() => {})
    const fresh = await api.getSessionDetail(id!)
    setDetail(fresh)
  }

  return (
    <div className="p-6 space-y-4 page-enter">
      <Link to="/sessions" className="btn-link">← 세션 목록</Link>

      <div className="card p-5">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h1 className="text-lg font-bold flex items-center gap-2">
              {sess.title || '제목 없음'} <FavoriteStar entity="sessions" id={sess.id} />
            </h1>
            <p className="text-[11px] text-gray-400 font-mono">{sess.session_id}</p>
            <p className="text-[11px] text-gray-500 mt-1">
              하네스 {sess.harness_id} · {sess.model_class || '모델 미지정'} · 보호 {sess.protection_profile || 'P0'}
              {sess.lease_id ? <span className="text-green-600"> · lease ✓</span> : <span className="text-red-500"> · lease ✗</span>}
              {sess.policy_epoch_id && <span className="text-gray-400"> · epoch {sess.policy_epoch_id.slice(0, 10)}</span>}
            </p>
          </div>
          <div className="flex gap-2 items-center shrink-0">
            <span className={`text-[10px] px-2 py-0.5 rounded-full border ${meta.badge}`}>{meta.ko}</span>
            {visibility && (
              <span className="text-[10px] px-2 py-0.5 rounded-full border bg-blue-50 text-blue-700 border-blue-200" title={visibility.label}>
                열람 {visibility.level}
              </span>
            )}
            {allowedSessionActions(sess.status).includes('pause') && <button className="btn-sm btn-secondary" onClick={() => act('pause')}>일시정지</button>}
            {allowedSessionActions(sess.status).includes('resume') && <button className="btn-sm btn-secondary" onClick={() => act('resume')}>재개</button>}
            {allowedSessionActions(sess.status).includes('close') && <button className="btn-sm btn-danger" onClick={() => act('close')}>종료</button>}
          </div>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mt-4">
          <StatCard label="최근 30일 토큰" value={usageLoading ? '불러오는 중' : usageError ? '조회 오류' : formatUsageStateInteger(totalTokens, usageReport?.total_tokens_state)} accent="blue" to={`/sessions/${sess.session_id || sess.id}`} query="#session-usage-ledger" sub={usageLoading ? '원장 조회 중' : usageError ? '권한 또는 조회 오류' : `원장 ${usageReport?.record_count ?? '—'}건`} />
          <StatCard label={`최근 30일 비용 (${usageReport?.display_currency || '통화 미확인'})`} value={usageLoading ? '불러오는 중' : totalCost?.state === 'recorded' || totalCost?.state === 'zero' ? formatUsageAmount(totalCost.amount_micros, totalCost.currency) : totalCost?.state === 'error' ? '집계 오류' : '미수집'} accent="green" to={`/sessions/${sess.session_id || sess.id}`} query="#session-usage-ledger" sub={usageLoading ? '원장 조회 중' : !hasUsageLedger ? '원장 기록 없음' : `${usageReport?.reconciled ? '원장 대사 완료' : '대사 확인 필요'}${delayedUsageMeters ? ` · 지연 ${delayedUsageMeters}` : ''}`} />
          <StatCard label="익스체인지" value={(detail.exchanges || []).length} accent="purple" />
          <StatCard label="변경셋" value={(detail.change_sets || []).length} accent="orange" />
        </div>
      </div>

      {usageLoading ? <div className="card p-4 text-[11px] text-gray-400">사용량 원장을 불러오는 중입니다.</div> : usageError ? <div className="card p-4 text-[11px] text-red-600">사용량 원장을 조회할 권한이 없거나 조회 중 오류가 발생했습니다.</div> : <UsageReport report={usageReport} id="session-usage-ledger" title="세션 사용량 및 비용 원장" loadMore={(cursor, signal) => api.getSessionUsage(id!, '30d', cursor, signal)} />}

      <div className="flex gap-1 border-b border-gray-200">
        {[
          { id: 'timeline', label: '타임라인' },
          { id: 'decisions', label: '정책 결정' },
          { id: 'changes', label: '변경셋' },
          { id: 'findings', label: '보안 발견' },
          { id: 'replay', label: '리플레이' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id)}
            className={`px-3 py-2 text-xs ${tab === t.id ? 'border-b-2 border-blue-600 text-blue-600 font-semibold' : 'text-gray-500 hover:text-gray-700'}`}>
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'timeline' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-2">세션 타임라인 — 액션을 한국어로 요약하며, 각 항목을 펼치면 원시/기술 증거를 확인할 수 있습니다.</p>
          {(detail.actions || []).length === 0 && <p className="text-[11px] text-gray-400">액션 기록 없음</p>}
          {(detail.actions || []).slice(0, 100).map((a: any, i: number) => {
            const v = sessionActionView(a)
            const key = 'a' + a.action_id + i
            return (
              <div key={key} className="border-b border-gray-50 py-1">
                <button className="w-full flex items-center gap-2 text-left text-[11px] hover:bg-gray-50 rounded px-1"
                  onClick={() => setExpandedRaw(expandedRaw === key ? null : key)}
                  aria-expanded={expandedRaw === key}>
                  <span onClick={e => { e.stopPropagation(); if (v.route) window.location.href = v.route }} className="cursor-pointer">{v.icon}</span>
                  <span className="text-gray-700">{v.title}</span>
                  <span className="text-gray-400 ml-auto">{formatSessionTime(a.occurred_at, timezone)}</span>
                  <span className="text-gray-400 text-[10px]">{expandedRaw === key ? '▲' : '▼'}</span>
                </button>
                {expandedRaw === key && (
                  <pre className="text-[10px] font-mono bg-gray-50 p-2 rounded mt-1 overflow-x-auto whitespace-pre-wrap">
                    {JSON.stringify({ action_id: a.action_id, action_type: a.action_type, exchange_id: a.exchange_id, verdict_result: a.verdict_result, occurred_at: a.occurred_at, payload: safeParse(a.action_payload) }, null, 2)}
                  </pre>
                )}
              </div>
            )
          })}
        </div>
      )}

      {tab === 'decisions' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-2">익스체인지별 정책 판정 — 판정·epoch·모델을 한국어로 요약합니다.</p>
          {(decisions?.decisions || []).length === 0 && <p className="text-[11px] text-gray-400">판정 로그 없음</p>}
          {(decisions?.decisions || []).map((d: any) => {
            const v = decisionView(d)
            const key = 'd' + d.exchange_id
            return (
              <div key={key} className="border-b border-gray-50 py-1">
                <button className="w-full flex items-center gap-2 text-left text-[11px] hover:bg-gray-50 rounded px-1"
                  onClick={() => setExpandedRaw(expandedRaw === key ? null : key)}
                  aria-expanded={expandedRaw === key}>
                  <span>{v.icon}</span>
                  <span className="text-gray-700">{v.title}</span>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${v.color}"`}>{v.outcome}</span>
                  <span className="text-gray-400 ml-auto">{formatSessionTime(d.at, timezone)}</span>
                  <span className="text-gray-400 text-[10px]">{expandedRaw === key ? '▲' : '▼'}</span>
                </button>
                {expandedRaw === key && (
                  <pre className="text-[10px] font-mono bg-gray-50 p-2 rounded mt-1 overflow-x-auto whitespace-pre-wrap">
                    {JSON.stringify({ exchange_id: d.exchange_id, policy_epoch_id: d.policy_epoch_id, verdict: d.verdict, model_package_id: d.model_package_id, input_tokens: d.input_tokens, output_tokens: d.output_tokens }, null, 2)}
                  </pre>
                )}
              </div>
            )
          })}
        </div>
      )}

      {tab === 'changes' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-2">변경셋 — 파일·디프·기여 구분을 한국어로 요약합니다.</p>
          {(detail.change_sets || []).length === 0 && <p className="text-[11px] text-gray-400">변경셋 없음</p>}
          {(detail.change_sets || []).map((c: any) => {
            const v = changeSetView(c)
            const key = 'c' + c.id
            return (
              <div key={key} className="border-b border-gray-50 py-1">
                <button className="w-full flex items-center gap-2 text-left text-[11px] hover:bg-gray-50 rounded px-1"
                  onClick={() => setExpandedRaw(expandedRaw === key ? null : key)}
                  aria-expanded={expandedRaw === key}>
                  <span>{v.icon}</span>
                  <span className="text-gray-700">{v.title}</span>
                  <span className="text-gray-400">{v.target}</span>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${v.color}"`}>{v.outcome}</span>
                  <span className="text-gray-400 ml-auto">{formatSessionTime(c.created_at, timezone)}</span>
                  <span className="text-gray-400 text-[10px]">{expandedRaw === key ? '▲' : '▼'}</span>
                </button>
                {expandedRaw === key && (
                  <pre className="text-[10px] font-mono bg-gray-50 p-2 rounded mt-1 overflow-x-auto whitespace-pre-wrap">
                    {JSON.stringify({ id: c.id, files_changed: safeParse(c.files_changed), diff_summary: c.diff_summary, diff_digest: c.diff_digest, attribution_state: c.attribution_state, lines_added: c.lines_added, lines_removed: c.lines_removed, change_set_digest: c.change_set_digest }, null, 2)}
                  </pre>
                )}
              </div>
            )
          })}
        </div>
      )}

      {tab === 'findings' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-2">보안 발견 — 정확한 발견 상세로 이동합니다.</p>
          {(detail.findings || []).length === 0 && <p className="text-[11px] text-gray-400">보안 발견 없음</p>}
          {(detail.findings || []).map((f: any) => {
            const v = findingView(f)
            const key = 'f' + f.id
            return (
              <div key={key} className="border-b border-gray-50 py-1">
                <div className="w-full flex items-center gap-2 text-left text-[11px] hover:bg-gray-50 rounded px-1">
                  <span>{v.icon}</span>
                  {v.route ? <Link className="text-blue-600 hover:underline" to={v.route}>{v.title}</Link> : <span className="text-gray-700">{v.title}</span>}
                  <span className="text-gray-400">{v.target}</span>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full border ${v.color}"`}>{v.severity}</span>
                  <span className="text-gray-400 ml-auto">{f.occurred_at ? formatSessionTime(f.occurred_at, timezone) : ''}</span>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {tab === 'replay' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-2">
            리플레이 — 베이스라인 {replay?.baseline_id ? replay.baseline_id.slice(0, 10) : '없음'}에서 시작하는 통치 이벤트 시퀀스.
          </p>
          {(replay?.events || []).length === 0 && <p className="text-[11px] text-gray-400">리플레이 이벤트 없음</p>}
          {(replay?.events || []).map((ev: any, i: number) => {
            const v = replayEventView(ev)
            const key = 'r' + i
            return (
              <div key={key} className="border-b border-gray-50 py-1">
                <button className="w-full flex items-center gap-2 text-left text-[11px] hover:bg-gray-50 rounded px-1"
                  onClick={() => setExpandedRaw(expandedRaw === key ? null : key)}
                  aria-expanded={expandedRaw === key}>
                  <span className="text-gray-400">{formatSessionTime(ev.at, timezone)}</span>
                  <span className="text-gray-700">{v.title}</span>
                  <span className="text-gray-400">{v.target}</span>
                  <span className="text-gray-400 ml-auto">{expandedRaw === key ? '▲' : '▼'}</span>
                </button>
                {expandedRaw === key && (
                  <pre className="text-[10px] font-mono bg-gray-50 p-2 rounded mt-1 overflow-x-auto whitespace-pre-wrap">
                    {JSON.stringify(ev.payload || ev, null, 2)}
                  </pre>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
