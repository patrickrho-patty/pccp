import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api'
import { formatRelative } from '../utils/format'
import { formatUsageStateInteger, formatUsageAmount, UsageReportData } from '../components/UsageReport'
import { auditEventView, groupAuditBursts, outcomeMeta, OutcomeKind } from '../evidenceView'

// safeParse best-effort parses JSON payload strings for the technical-detail area.
function safeParse(v: unknown): unknown {
  if (typeof v !== 'string') return v
  try { return JSON.parse(v) } catch { return v }
}

export default function Dashboard() {
  const [data, setData] = useState<any>(null)
  const [brief, setBrief] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()
  const [findingCount, setFindingCount] = useState(0)
	const [usage, setUsage] = useState<UsageReportData | null>(null)
  const [usageError, setUsageError] = useState(false)
  const [feedExpanded, setFeedExpanded] = useState<Set<number>>(new Set()) // burst rows opened
  const [feedRaw, setFeedRaw] = useState<Set<string>>(new Set()) // events with raw technical detail open

  useEffect(() => {
    let active = true
    const controller = new AbortController()
    Promise.all([
	  api.dashboard().catch(() => ({})),
	  api.governanceBrief().catch(() => null),
	  api.securityFindings().catch(() => []),
	  api.usageExtended('30d', '', controller.signal, true).catch(error => { if (error?.name !== 'AbortError') setUsageError(true); return null }),
	]).then(([dash, brf, findings, usageReport]) => {
      if (!active) return
      setData(dash); setBrief(brf)
	  setUsage(usageReport)
      setFindingCount(Array.isArray(findings) ? findings.filter((f: any) => f.status === 'open').length : 0)
      setLoading(false)
    })
    return () => { active = false; controller.abort() }
  }, [])

  if (loading) return <div className="text-gray-500">로딩 중...</div>
  if (!data) return <div>데이터 없음</div>

  const stats = [
    { label: '사용자', labelEn: 'Users', value: data?.users || 0, color: 'bg-blue-500', route: '/users' },
    { label: '하네스', labelEn: 'Harnesses', value: data?.harnesses || 0, color: 'bg-green-500', route: '/harnesses' },
	{ label: '활성 세션', labelEn: 'Active Sessions', value: data?.active_session_count ?? 0, color: 'bg-purple-500', route: '/sessions?status=active' },
    { label: '엔드포인트', labelEn: 'Endpoints', value: data?.endpoints || 0, color: 'bg-orange-500', route: '/models' },
  ]

  return (
    <div>
            {/* Empty state (no demo-data fabrication in enterprise) */}
      {(data?.users === 0) ? (
        <div className="card mb-6 border-l-4 border-l-gray-300">
          <h3 className="text-sm font-semibold text-gray-500">데이터가 없습니다 · No Data Yet</h3>
          <p className="text-xs text-gray-400 mt-1">데이터는 관리되는 세션이 실행되면 표시됩니다</p>
        </div>
      ) : null}

      {/* Dashboard freshness + stale state (PAT-1487): one last-updated time
          for repeated metrics; when stale, the panel says so instead of
          implying contradictory fresh numbers. */}
      <div className="flex items-center gap-2 mb-3 text-[10px] text-gray-400">
        <span className="inline-block w-1.5 h-1.5 rounded-full bg-green-500" aria-hidden="true" />
        마지막 갱신 {data?.dashboard_last_updated ? ` · ${data.dashboard_last_updated.slice(0, 19).replace('T', ' ')}` : '(미수집)'}
        {data?.dashboard_stale && <span className="text-red-500">· 지연됨 (stale)</span>}
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-4 gap-3 mb-6">
        {stats.map(s => (
          <Link key={s.labelEn} to={s.route} className="card py-4 px-5 cursor-pointer hover:shadow-md transition-shadow block">
            <div className={`w-2 h-2 rounded-full ${s.color} mb-2`} />
            <div className="text-3xl font-bold">{s.value}</div>
            <div className="text-sm text-gray-500">{s.label}</div>
            <div className="text-xs text-gray-400">{s.labelEn}</div>
          </Link>
        ))}
      </div>

	  <Link to="/analytics#usage-ledger" className="card p-4 mb-6 grid grid-cols-2 md:grid-cols-4 gap-4 hover:shadow-md transition-shadow">
	    <div><div className="text-[10px] text-gray-400">최근 30일 총 토큰</div><div className="text-lg font-bold text-blue-700">{usageError ? '조회 오류' : formatUsageStateInteger(usage?.total_tokens, usage?.total_tokens_state)}</div></div>
	    <div><div className="text-[10px] text-gray-400">입력 / 출력</div><div className="text-sm font-semibold">{usageError ? '조회 오류' : `${formatUsageStateInteger(usage?.input_tokens, usage?.input_tokens_state)} / ${formatUsageStateInteger(usage?.output_tokens, usage?.output_tokens_state)}`}</div></div>
	    <div><div className="text-[10px] text-gray-400">비용 ({usage?.display_currency || '통화 미확인'})</div><div className="text-sm font-semibold text-orange-700">{usage?.display_total?.state === 'recorded' || usage?.display_total?.state === 'zero' ? formatUsageAmount(usage.display_total.amount_micros, usage.display_total.currency) : usage?.display_total?.state === 'error' ? '집계 오류' : '미수집'}</div></div>
	    <div><div className="text-[10px] text-gray-400">원장 상태</div><div className={`text-sm font-semibold ${usageError ? 'text-red-700' : !usage || usage.record_count === 0 ? 'text-gray-500' : usage.reconciled ? 'text-green-700' : 'text-red-700'}`}>{usageError ? '권한 또는 조회 오류' : !usage ? '확인 불가' : usage.record_count === 0 ? '수집 내역 없음' : usage.reconciled ? `대사 완료 · ${usage.record_count.toLocaleString()}건` : `확인 필요 · ${usage.record_count.toLocaleString()}건`}</div><div className="text-[10px] text-blue-600 mt-1">원장 상세 보기 →</div></div>
	  </Link>

      {/* Incidents + gaps (A5) — PAT-1484: each KPI opens the exact pre-filtered
          work queue its label implies. Counts come from the shared backend
          scope contract so the destination list reconciles with the card. */}
      <div className="grid grid-cols-2 gap-3 mb-4">
        <Link to="/security?tab=findings&severity=critical,high&status=unresolved" className="card py-3 px-5 hover:shadow-md transition-shadow focus:outline-none focus:ring-2 focus:ring-red-400" aria-label="미해결 심각·높음 보안 발견 목록 열기 (열린 목록으로 이동)">
          <div className="text-sm font-semibold text-red-600">{data?.open_critical_findings ?? findingCount}</div>
          <div className="text-xs text-gray-500">미해결 심각·높음 보안 발견</div>
        </Link>
        <Link to="/compliance?tab=remediation&status=unresolved" className="card py-3 px-5 hover:shadow-md transition-shadow focus:outline-none focus:ring-2 focus:ring-amber-400" aria-label="진행 중 컴플라이언스 개선 과제 목록 열기 (열린 과제로 이동)">
          <div className="text-sm font-semibold text-amber-600">{data?.open_remediations ?? 0}</div>
          <div className="text-xs text-gray-500">진행 중 컴플라이언스 개선 과제</div>
        </Link>
      </div>

      {/* Recents removed per PAT-1486: ambiguous mixed-entity recents strip removed. Space reserved for actionable operational state. */}

      {/* Two column layout */}
      <div className="grid grid-cols-3 gap-4">
        {/* Recent activity */}
        <div className="card col-span-2">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold">최근 활동 · Recent Activity</h3>
            <Link to="/audit" className="btn-link">전체 보기 →</Link>
          </div>
          <div className="flex flex-wrap gap-2 mb-2 text-[10px] text-gray-500" role="list" aria-label="활동 상태 범례">
            {(['success', 'warning', 'danger', 'info', 'unknown'] as OutcomeKind[]).map(k => {
              const m = outcomeMeta(k)
              return <span key={k} role="listitem" className="inline-flex items-center gap-1"><span className={m.color}>{m.icon}</span> {k === 'success' ? '성공' : k === 'warning' ? '경고' : k === 'danger' ? '위험/거부' : k === 'info' ? '정보' : '미기록'}</span>
            })}
          </div>
          {data?.recent_activity && data.recent_activity.length > 0 ? (
            <div className="space-y-1">
              {((() => {
                const { rows } = groupAuditBursts(data.recent_activity || [])
                return rows.slice(0, 12)
              })()).map((g: any, i: number) => {
                const v = auditEventView(g)
                const isBurst = (g.count || 1) > 1
                const burstOpen = feedExpanded.has(i)
                const rawKey = g.id || String(g.chain_seq) || `${g.event_type}-${g.occurred_at}`
                const rawOpen = feedRaw.has(rawKey)
                return (
                  <div key={rawKey} className="text-sm py-2 border-b border-gray-50 last:border-0">
                    <div className="flex items-center gap-3">
                      <span className="text-base shrink-0">{v.icon}</span>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className={`inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded-full border ${v.color}`}>{v.icon} <span>{v.outcome}</span></span>
                          <span className="text-xs text-gray-500">{v.categoryLabelKo}</span>
                        </div>
                        <p className="font-medium truncate text-[13px] text-gray-800">{v.title}</p>
                        <p className="text-[10px] text-gray-400">
                          {v.actorLabel}{v.resourceLabel ? ` · ${v.resourceLabel}` : ''}
                          {g.occurred_at ? ` · ${g.occurred_at.slice(0, 16).replace('T', ' ')} (${formatRelative(g.occurred_at)})` : ''}
                        </p>
                      </div>
                      <span className="flex flex-col items-end gap-1 shrink-0">
                        <button className="text-[10px] text-gray-400 hover:text-gray-600" onClick={() => setFeedRaw(prev => { const n = new Set(prev); if (n.has(rawKey)) n.delete(rawKey); else n.add(rawKey); return n })}>
                          {rawOpen ? '기술 상세 ▲' : '기술 상세 ▼'}
                        </button>
                        {isBurst && (
                          <button className="text-[10px] px-1.5 py-0.5 rounded-full bg-gray-100 text-gray-600" onClick={() => setFeedExpanded(prev => { const n = new Set(prev); if (n.has(i)) n.delete(i); else n.add(i); return n })} aria-expanded={burstOpen}>
                            {burstOpen ? `접기 (${g.count})` : `× ${g.count}`}
                          </button>
                        )}
                        {v.route && (
                          <Link to={v.route} className="text-[10px] text-blue-600 hover:underline">관련 기록 →</Link>
                        )}
                      </span>
                    </div>
                    {rawOpen && (
                      <pre className="mt-1 ml-7 text-[10px] font-mono bg-gray-50 p-2 rounded overflow-x-auto whitespace-pre-wrap">
                        {JSON.stringify({ id: g.id, event_type: g.event_type, actor_type: g.actor_type, actor_id: g.actor_id, action: g.action, resource_type: g.resource_type, resource_id: g.resource_id, result: g.result, digest: g.event_digest, chain_seq: g.chain_seq, details: safeParse(g.details) }, null, 2)}
                      </pre>
                    )}
                    {isBurst && burstOpen && (
                      <div className="mt-1 ml-7 space-y-0.5 border-l-2 border-gray-100 pl-3">
                        {g.items.map((ev: any, j: number) => {
                          const sub = auditEventView(ev)
                          const subKey = ev.id || `${ev.event_type}-${j}`
                          const subRawOpen = feedRaw.has(subKey)
                          return (
                            <div key={subKey} className="flex items-center gap-2 text-[10px] text-gray-500">
                              <span className="text-gray-400">{ev.occurred_at?.slice(0, 16).replace('T', ' ') || ''}</span>
                              <span className="flex-1 truncate text-gray-600">{sub.title}</span>
                              {sub.route ? <Link to={sub.route} className="text-blue-500 hover:underline">→</Link> :
                                <button className="text-gray-400" onClick={() => setFeedRaw(prev => { const n = new Set(prev); if (n.has(subKey)) n.delete(subKey); else n.add(subKey); return n })}>{subRawOpen ? '▲' : '▼'}</button>}
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          ) : (
            <p className="text-gray-400 text-center py-8 text-sm">활동 내역이 없습니다</p>
          )}
        </div>

        {/* Side panel */}
        <div className="space-y-4">
          {/* Security summary */}
          <div className="card">
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-semibold">보안 현황</h3>
              <Link to="/security" className="btn-link">상세 →</Link>
            </div>
            <div className="space-y-2">
              <div className="flex justify-between text-sm">
                <div>
                  <span className="text-gray-500">미해결 발견</span>
                  <span className="block text-[10px] text-gray-400">모든 심각도 · 해결 제외</span>
                </div>
                <Link to="/security?tab=findings&status=unresolved" className={`font-bold hover:underline ${(data?.unresolved_findings ?? findingCount) > 0 ? 'text-red-600' : 'text-green-600'}`}>{data?.unresolved_findings ?? findingCount}</Link>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">활성 하네스</span>
                <Link to="/harnesses" className="font-bold text-blue-600 hover:underline">{data?.harnesses || 0}</Link>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">활성 세션</span>
				<Link to="/sessions?status=active" className="font-bold text-blue-600 hover:underline">{data?.active_session_count ?? 0}</Link>
              </div>
            </div>
          </div>

          {/* Admin Action Center (PAT-1488) — ranked work queue, replaces governance brief */}
          <div className="card border-l-4 border-l-amber-500">
            <h3 className="text-sm font-semibold mb-2">조치 필요 · Action Center</h3>
            <p className="text-[11px] text-gray-400 mb-2">우선순위와 영향도에 따라 정렬된 조치 큐 — 각 항목은 정확한 필터된 대기열로 연결됩니다.</p>
            <div className="space-y-2">
              <Link to="/security?tab=findings&severity=critical,high&status=unresolved" className="flex justify-between items-center p-2 bg-red-50 rounded hover:bg-red-100 border border-red-100">
                <span className="text-xs font-medium text-red-700">미해결 심각·높음 보안 발견</span>
                <span className="text-sm font-bold text-red-600">{data?.open_critical_findings ?? findingCount}건 →</span>
              </Link>
              <Link to="/compliance?tab=remediation&status=unresolved" className="flex justify-between items-center p-2 bg-amber-50 rounded hover:bg-amber-100 border border-amber-100">
                <span className="text-xs font-medium text-amber-700">진행 중 컴플라이언스 개선 과제</span>
                <span className="text-sm font-bold text-amber-600">{data?.open_remediations ?? 0}건 →</span>
              </Link>
              <Link to="/fleet" className="flex justify-between items-center p-2 bg-gray-50 rounded hover:bg-gray-100 border border-gray-100">
                <span className="text-xs text-gray-600">격리/비정상 하네스</span>
                <span className="text-xs text-gray-500">{data?.quarantined_harnesses ?? 0}건 →</span>
              </Link>
              <Link to="/tools?tab=approvals" className="flex justify-between items-center p-2 bg-gray-50 rounded hover:bg-gray-100 border border-gray-100">
                <span className="text-xs text-gray-600">대기 중 승인 요청</span>
                <span className="text-xs text-gray-500">{data?.pending_approvals ?? 0}건 →</span>
              </Link>
            </div>
            {((data?.open_critical_findings ?? 0) === 0 && (data?.open_remediations ?? 0) === 0 && (data?.quarantined_harnesses ?? 0) === 0 && (data?.pending_approvals ?? 0) === 0) && (
              <p className="text-xs text-green-600 mt-3">✅ 조치 대기 항목이 없습니다 — {data?.dashboard_last_updated ? `마지막 확인 ${data.dashboard_last_updated.slice(0,16).replace('T',' ')}` : '최근 확인됨'}</p>
            )}
          </div>

          {/* Operational Health — secondary, after Action Center (PAT-1488) */}
          <div className="card">
            <h3 className="text-sm font-semibold mb-2">운영 상태 · Operational Health</h3>
            <div className="text-xs text-gray-500 space-y-1">
              <div className="flex justify-between"><span>활성 하네스</span><span className="font-medium">{brief?.active_harnesses ?? data?.harnesses ?? 0}</span></div>
              <div className="flex justify-between"><span>활성 세션</span><span className="font-medium">{data?.active_session_count ?? 0}</span></div>
              <div className="flex justify-between"><span>미해결 보안 발견</span><span className="font-medium text-red-600">{findingCount}</span></div>
              <div className="text-[10px] text-gray-400 mt-1">지속 탐색은 좌측 내비게이션을 사용하세요 — 대시보드는 조치 큐에 집중합니다.</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
