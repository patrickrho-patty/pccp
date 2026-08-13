import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { SeatWidget } from '../components/SeatWidget'

export default function Dashboard() {
  const [data, setData] = useState<any>(null)
  const [brief, setBrief] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()
  const [findingCount, setFindingCount] = useState(0)

  useEffect(() => {
    Promise.all([
      fetch('/api/dashboard', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/korean/governance-brief', { headers: authHeaders() }).then(r => r.json()).catch(() => null),
      fetch('/api/security/findings', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
    ]).then(([dash, brf, findings]) => {
      setData(dash); setBrief(brf)
      setFindingCount(Array.isArray(findings) ? findings.filter((f: any) => f.status === 'open').length : 0)
      setLoading(false)
    })
  }, [])

  if (loading) return <div className="text-gray-500">로딩 중...</div>
  if (!data) return <div>데이터 없음</div>

  const stats = [
    { label: '사용자', labelEn: 'Users', value: data?.users || 0, color: 'bg-blue-500', route: '/users' },
    { label: '하네스', labelEn: 'Harnesses', value: data?.harnesses || 0, color: 'bg-green-500', route: '/harnesses' },
    { label: '활성 세션', labelEn: 'Active Sessions', value: data?.active_sessions?.length || 0, color: 'bg-purple-500', route: '/sessions' },
    { label: '엔드포인트', labelEn: 'Endpoints', value: data?.endpoints || 0, color: 'bg-orange-500', route: '/models' },
  ]

  return (
    <div>
      <SeatWidget />

      {/* Demo data seed */}
      {stats.length === 0 || (data?.users === 0) ? (
        <div className="card mb-6 border-l-4 border-l-blue-400">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-sm font-semibold">데모 데이터가 없습니다 · No Demo Data</h3>
              <p className="text-xs text-gray-400 mt-1">모든 페이지에 표시할 샘플 데이터를 생성하려면 아래 버튼을 클릭하세요</p>
            </div>
            <button onClick={async () => {
              try {
                await fetch('/api/enterprise/features/seed', { method: 'POST', headers: authHeaders() })
                await fetch('/api/tools/seed-defaults', { method: 'POST', headers: authHeaders() })
                await fetch('/api/enterprise/demo-seed', { method: 'POST', headers: authHeaders() })
                window.location.reload()
              } catch { alert('시드 실패') }
            }} className="btn-primary text-sm">📊 데모 데이터 생성</button>
          </div>
        </div>
      ) : null}

      {/* Stat cards */}
      <div className="grid grid-cols-4 gap-3 mb-6">
        {stats.map(s => (
          <div key={s.labelEn} className="card py-4 px-5 cursor-pointer hover:shadow-md transition-shadow" onClick={() => navigate(s.route)}>
            <div className={`w-2 h-2 rounded-full ${s.color} mb-2`} />
            <div className="text-3xl font-bold">{s.value}</div>
            <div className="text-sm text-gray-500">{s.label}</div>
            <div className="text-xs text-gray-400">{s.labelEn}</div>
          </div>
        ))}
      </div>

      {/* Two column layout */}
      <div className="grid grid-cols-3 gap-4">
        {/* Recent activity */}
        <div className="card col-span-2">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold">최근 활동 · Recent Activity</h3>
            <Link to="/audit" className="text-xs text-blue-600 hover:underline">전체 보기 →</Link>
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
                    <span className="text-xs text-gray-400">{a.occurred_at?.slice(11, 19) || ''}</span>
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
              <Link to="/security" className="text-xs text-blue-600 hover:underline">상세 →</Link>
            </div>
            <div className="space-y-2">
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">미해결 발견</span>
                <span className={`font-bold ${findingCount > 0 ? 'text-red-600' : 'text-green-600'}`}>{findingCount}</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">활성 하네스</span>
                <Link to="/harnesses" className="font-bold text-blue-600 hover:underline">{data?.harnesses || 0}</Link>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">활성 세션</span>
                <Link to="/sessions" className="font-bold text-blue-600 hover:underline">{data?.active_sessions?.length || 0}</Link>
              </div>
            </div>
          </div>

          {/* Governance brief */}
          {brief && (
            <div className="card">
              <h3 className="text-sm font-semibold mb-2">거버넌스 브리프</h3>
              <div className="text-xs text-gray-500 space-y-1">
                {brief.ai_adoption_rate !== undefined && (
                  <div className="flex justify-between"><span>AI 채택률</span><span className="font-medium">{(brief.ai_adoption_rate * 100).toFixed(0)}%</span></div>
                )}
                {brief.total_developers !== undefined && (
                  <div className="flex justify-between"><span>개발자 수</span><span className="font-medium">{brief.total_developers}</span></div>
                )}
                {brief.active_projects !== undefined && (
                  <div className="flex justify-between"><span>활성 프로젝트</span><span className="font-medium">{brief.active_projects}</span></div>
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

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
