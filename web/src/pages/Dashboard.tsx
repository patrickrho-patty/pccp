import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api'
import { formatRelative } from '../utils/format'
import { formatUsageStateInteger, formatUsageAmount, UsageReportData } from '../components/UsageReport'

export default function Dashboard() {
  const [data, setData] = useState<any>(null)
  const [brief, setBrief] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()
  const [findingCount, setFindingCount] = useState(0)
	const [usage, setUsage] = useState<UsageReportData | null>(null)
  const [usageError, setUsageError] = useState(false)

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

      {/* Incidents + gaps (A5) */}
      <div className="grid grid-cols-2 gap-3 mb-4">
        <Link to="/security" className="card py-3 px-5 hover:shadow-md transition-shadow">
          <div className="text-sm font-semibold text-red-600">{data?.open_critical_findings ?? findingCount}</div>
          <div className="text-xs text-gray-500">미해결 심각/높음 보안 발견 · Open Critical Findings</div>
        </Link>
        <Link to="/compliance" className="card py-3 px-5 hover:shadow-md transition-shadow">
          <div className="text-sm font-semibold text-amber-600">{data?.open_remediations ?? 0}</div>
          <div className="text-xs text-gray-500">진행 중 컴플라이언스 개선 과제 · Open Remediations</div>
        </Link>
      </div>

      {/* Recents hub (A7) */}
      {(data?.recent_users?.length > 0 || data?.recent_projects?.length > 0) && (
        <div className="card p-4 mb-4">
          <h3 className="text-xs font-bold mb-2">최근 항목 · Recents</h3>
          <div className="flex flex-wrap gap-2">
            {(data?.recent_users || []).map((u: any) => (
              <Link key={u.id} to={`/users/${u.id}`} className="text-[11px] px-2 py-1 rounded bg-gray-50 hover:bg-blue-50 text-gray-600">
                👤 {u.name_ko || u.name}
              </Link>
            ))}
            {(data?.recent_projects || []).map((p: any) => (
              <Link key={p.id} to={`/projects/${p.id}`} className="text-[11px] px-2 py-1 rounded bg-gray-50 hover:bg-blue-50 text-gray-600">
                📁 {p.name_ko || p.name}
              </Link>
            ))}
          </div>
        </div>
      )}

      {/* Two column layout */}
      <div className="grid grid-cols-3 gap-4">
        {/* Recent activity */}
        <div className="card col-span-2">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold">최근 활동 · Recent Activity</h3>
            <Link to="/audit" className="btn-link">전체 보기 →</Link>
          </div>
          {data?.recent_activity && data.recent_activity.length > 0 ? (
            <div className="space-y-1">
              {data.recent_activity.slice(0, 12).map((a: any, i: number) => {
                const icon = a.action?.includes('security') || a.action?.includes('denied') ? '🔴'
                  : a.action?.includes('enroll') || a.action?.includes('create') ? '🟢'
                  : a.action?.includes('revoke') || a.action?.includes('recall') ? '🟡' : '🔵'
                return (
                  <div key={i} className="flex items-center gap-3 text-sm py-2 border-b border-gray-50 last:border-0">
                    <span>{icon}</span>
                    <span className="font-medium w-40 truncate">{a.action || a.event_type}</span>
                    <span className="text-xs text-gray-400 truncate flex-1">{a.resource_type || a.details?.slice(0, 40)}</span>
                    <span className="text-xs text-gray-400">{formatRelative(a.occurred_at)}</span>
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
                <span className="text-gray-500">미해결 발견</span>
                <Link to="/security" className={`font-bold hover:underline ${findingCount > 0 ? 'text-red-600' : 'text-green-600'}`}>{findingCount}</Link>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">활성 하네스</span>
                <Link to="/harnesses" className="font-bold text-blue-600 hover:underline">{data?.harnesses || 0}</Link>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">활성 세션</span>
				<Link to="/sessions" className="font-bold text-blue-600 hover:underline">{data?.active_session_count ?? 0}</Link>
              </div>
            </div>
          </div>

          {/* Governance brief — fields match /api/korean/governance-brief */}
          {brief && (
            <div className="card">
              <h3 className="text-sm font-semibold mb-2">거버넌스 브리프</h3>
              <div className="text-xs text-gray-500 space-y-1">
                {brief.total_sessions !== undefined && (
                  <div className="flex justify-between"><span>총 세션</span><span className="font-medium">{brief.total_sessions}</span></div>
                )}
                {brief.active_harnesses !== undefined && (
                  <div className="flex justify-between"><span>활성 하네스</span><span className="font-medium">{brief.active_harnesses}</span></div>
                )}
                {brief.security_findings !== undefined && (
                  <div className="flex justify-between"><span>보안 발견</span><span className={`font-medium ${(brief.security_findings || 0) > 0 ? 'text-red-600' : 'text-green-600'}`}>{brief.security_findings}</span></div>
                )}
                {brief.model_invocations !== undefined && (
                  <div className="flex justify-between"><span>AI 추론</span><span className="font-medium">{brief.model_invocations}</span></div>
                )}
                {brief.code_changes !== undefined && (
                  <div className="flex justify-between"><span>코드 변경</span><span className="font-medium">{brief.code_changes}</span></div>
                )}
                {brief.approval_rate !== undefined && (
                  <div className="flex justify-between"><span>승인율</span><span className="font-medium">{(brief.approval_rate * 100).toFixed(0)}%</span></div>
                )}
                {brief.compliance_status && (
                  <div className="flex justify-between"><span>컴플라이언스</span><span className="font-medium">{brief.compliance_status}</span></div>
                )}
              </div>
            </div>
          )}

          {/* Quick links */}
          <div className="card">
            <h3 className="text-sm font-semibold mb-2">빠른 이동</h3>
            <div className="grid grid-cols-2 gap-2">
              <Link to="/sessions" className="text-xs text-center p-2 bg-gray-50 rounded hover:bg-blue-50">세션</Link>
              <Link to="/harnesses" className="text-xs text-center p-2 bg-gray-50 rounded hover:bg-blue-50">하네스</Link>
              <Link to="/security" className="text-xs text-center p-2 bg-gray-50 rounded hover:bg-blue-50">보안</Link>
              <Link to="/audit" className="text-xs text-center p-2 bg-gray-50 rounded hover:bg-blue-50">감사</Link>
              <Link to="/analytics" className="text-xs text-center p-2 bg-gray-50 rounded hover:bg-blue-50">분석</Link>
              <Link to="/explorer" className="text-xs text-center p-2 bg-gray-50 rounded hover:bg-blue-50">프로바이던스</Link>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
