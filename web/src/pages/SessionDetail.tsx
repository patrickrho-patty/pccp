import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { FavoriteStar } from '../hooks/useFavorites'
import { formatUsageAmount, UsageReport, UsageReportData } from '../components/UsageReport'

const STATUS_META: Record<string, { ko: string; badge: string }> = {
  pending: { ko: '대기', badge: 'bg-gray-100 text-gray-600 border-gray-200' },
  active: { ko: '활성', badge: 'bg-green-50 text-green-700 border-green-200' },
  idle: { ko: '유휴', badge: 'bg-yellow-50 text-yellow-700 border-yellow-200' },
  paused: { ko: '일시정지', badge: 'bg-amber-50 text-amber-700 border-amber-200' },
  closed: { ko: '종료', badge: 'bg-gray-100 text-gray-500 border-gray-200' },
  terminated: { ko: '강제종료', badge: 'bg-red-50 text-red-700 border-red-200' },
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
  const [tab, setTab] = useState('timeline')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    Promise.all([
      api.getSessionDetail(id),
      api.getSessionDecisions(id),
      api.getSessionReplay(id),
      api.getSessionVisibility(id),
      api.getSessionUsage(id),
    ]).then(([d, dec, rep, vis, usage]) => {
      setDetail(d)
      setDecisions(dec)
      setReplay(rep)
      setVisibility(vis)
      setUsageReport(usage)
    }).catch(() => {}).finally(() => setLoading(false))
  }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!detail?.session) return (
    <div>
      <Link to="/sessions" className="btn-link">← 세션 목록</Link>
      <p className="text-gray-400 p-8 text-center">세션을 찾을 수 없습니다</p>
    </div>
  )

  const sess = detail.session
  const meta = STATUS_META[sess.status] || STATUS_META.pending
  const totalTokens = usageReport?.total_tokens
  const totalCost = usageReport?.display_total

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
            {sess.status === 'active' && <button className="btn-sm btn-secondary" onClick={() => act('pause')}>일시정지</button>}
            {(sess.status === 'paused' || sess.status === 'idle') && <button className="btn-sm btn-secondary" onClick={() => act('resume')}>재개</button>}
            {sess.status !== 'closed' && sess.status !== 'terminated' && <button className="btn-sm btn-danger" onClick={() => act('close')}>종료</button>}
          </div>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mt-4">
          <StatCard label="최근 30일 토큰" value={totalTokens == null ? '—' : totalTokens.toLocaleString()} accent="blue" to={`/sessions/${sess.session_id || sess.id}`} query="#session-usage-ledger" sub={`원장 ${usageReport?.record_count ?? '—'}건`} />
          <StatCard label={`최근 30일 비용 (${usageReport?.display_currency || '통화 미확인'})`} value={totalCost ? formatUsageAmount(totalCost.amount_micros, totalCost.currency) : '—'} accent="green" to={`/sessions/${sess.session_id || sess.id}`} query="#session-usage-ledger" sub={usageReport?.reconciled ? '원장 대사 완료' : '대사 확인 필요'} />
          <StatCard label="익스체인지" value={(detail.exchanges || []).length} accent="purple" />
          <StatCard label="변경셋" value={(detail.change_sets || []).length} accent="orange" />
        </div>
      </div>

      <UsageReport report={usageReport} id="session-usage-ledger" title="세션 사용량 및 비용 원장" />

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
          {(detail.actions || []).length === 0 && <p className="text-[11px] text-gray-400">액션 기록 없음</p>}
          {(detail.actions || []).slice(0, 100).map((a: any, i: number) => (
            <div key={i} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700">{a.action_type || a.type || a.kind} — {(a.description || a.summary || '').slice(0, 80)}</span>
              <span className="text-gray-400">{(a.occurred_at || '').slice(0, 16)}</span>
            </div>
          ))}
        </div>
      )}

      {tab === 'decisions' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-2">익스체인지별 정책 판정 로그 (B2) — 판정 근거는 epoch 정책 + 보안 파이프라인에서 산출됩니다.</p>
          {(decisions?.decisions || []).length === 0 && <p className="text-[11px] text-gray-400">판정 로그 없음</p>}
          {(decisions?.decisions || []).map((d: any) => (
            <div key={d.exchange_id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700 font-mono">{d.exchange_id?.slice(0, 14)}</span>
              <span className={d.verdict === 'allowed' ? 'text-green-600' : d.verdict === 'denied' ? 'text-red-600' : 'text-gray-400'}>
                {d.verdict}
              </span>
              <span className="text-gray-400">{d.input_tokens}/{d.output_tokens} tok · {d.at?.slice(0, 16)}</span>
            </div>
          ))}
        </div>
      )}

      {tab === 'changes' && (
        <div className="card p-4 space-y-1">
          {(detail.change_sets || []).length === 0 && <p className="text-[11px] text-gray-400">변경셋 없음</p>}
          {(detail.change_sets || []).map((c: any) => (
            <div key={c.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700">{c.summary || c.message || '변경'}</span>
              <span className="text-gray-400">{c.attribution_state || ''}</span>
            </div>
          ))}
        </div>
      )}

      {tab === 'findings' && (
        <div className="card p-4 space-y-1">
          {(detail.findings || []).length === 0 && <p className="text-[11px] text-gray-400">보안 발견 없음</p>}
          {(detail.findings || []).map((f: any) => (
            <div key={f.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700">{f.title || f.finding_type || '발견'}</span>
              <span className="text-gray-400">{f.severity}</span>
            </div>
          ))}
        </div>
      )}

      {tab === 'replay' && (
        <div className="card p-4 space-y-1">
          <p className="text-[10px] text-gray-400 mb-2">
            리플레이 (B6) — 베이스라인 {replay?.baseline_id ? replay.baseline_id.slice(0, 10) : '없음'}에서 시작하는 통치 이벤트 시퀀스.
          </p>
          {(replay?.events || []).length === 0 && <p className="text-[11px] text-gray-400">리플레이 이벤트 없음</p>}
          {(replay?.events || []).map((ev: any, i: number) => (
            <div key={i} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700">
                <span className="text-gray-400 font-mono">{ev.at?.slice(11, 19)}</span> {ev.kind}
              </span>
              <span className="text-gray-400">{ev.payload?.id?.slice(0, 10) || ''}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
