import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

export default function Dashboard() {
  const [data, setData] = useState<any>(null)
  const [brief, setBrief] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()

  useEffect(() => {
    Promise.all([
      fetch('/api/dashboard', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/korean/governance-brief', { headers: authHeaders() }).then(r => r.json()).catch(() => null),
    ]).then(([dash, brf]) => { setData(dash); setBrief(brf); setLoading(false) })
  }, [])

  if (loading) return <div className="text-gray-500">로딩 중...</div>
  if (!data) return <div>데이터 없음</div>

  const stats = [
    { label: '사용자', labelEn: 'Users', value: data?.users || 0, color: 'bg-blue-500', route: '/users' },
    { label: '하네스', labelEn: 'Harnesses', value: data?.harnesses || 0, color: 'bg-green-500', route: '/harnesses' },
    { label: '활성 세션', labelEn: 'Active Sessions', value: data?.active_sessions?.length || 0, color: 'bg-purple-500', route: '/sessions' },
    { label: '엔드포인트', labelEn: 'Endpoints', value: data?.endpoints || 0, color: 'bg-orange-500', route: '/endpoints' },
  ]

  const activityIcon = (action: string) => {
    if (action.includes('security') || action.includes('denied')) return '🔴'
    if (action.includes('enroll') || action.includes('create')) return '🟢'
    if (action.includes('revoke') || action.includes('recall')) return '🟡'
    return '🔵'
  }

  const formatTime = (ts: string) => {
    if (!ts) return '-'
    const d = new Date(ts)
    const diff = Date.now() - d.getTime()
    const mins = Math.floor(diff / 60000)
    if (mins < 1) return '방금 전'
    if (mins < 60) return `${mins}분 전`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}시간 전`
    return d.toLocaleDateString('ko-KR')
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">대시보드 <span className="text-gray-400 text-lg font-normal">Enterprise Command Center</span></h1>

      {/* Clickable stat cards */}
      <div className="grid grid-cols-4 gap-4 mb-6">
        {stats.map(stat => (
          <div key={stat.labelEn} onClick={() => navigate(stat.route)}
            className="card cursor-pointer hover:border-blue-300 hover:shadow-md transition-all">
            <div className={`w-3 h-3 rounded-full ${stat.color} mb-3`} />
            <div className="text-3xl font-bold">{stat.value}</div>
            <div className="text-sm text-gray-500">{stat.label} · {stat.labelEn} →</div>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-3 gap-6 mb-6">
        {/* Governance Brief */}
        <div className="card col-span-2">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">주간 AI 거버넌스 브리프 <span className="text-gray-400 text-sm font-normal">Weekly Governance Brief</span></h2>
            {brief?.compliance_status && (
              <span className={brief.compliance_status.includes('양호') ? 'badge-green' : 'badge-yellow'}>
                {brief.compliance_status}
              </span>
            )}
          </div>
          {brief ? (
            <div className="grid grid-cols-4 gap-4 mb-4">
              <div className="text-center p-3 bg-gray-50 rounded-lg">
                <div className="text-xl font-bold">{brief.total_sessions || 0}</div>
                <div className="text-xs text-gray-500">세션</div>
              </div>
              <div className="text-center p-3 bg-gray-50 rounded-lg">
                <div className="text-xl font-bold">{brief.active_harnesses || 0}</div>
                <div className="text-xs text-gray-500">하네스</div>
              </div>
              <div className="text-center p-3 bg-gray-50 rounded-lg">
                <div className="text-xl font-bold">{brief.model_invocations || 0}</div>
                <div className="text-xs text-gray-500">AI 추론</div>
              </div>
              <div className="text-center p-3 bg-gray-50 rounded-lg">
                <div className={`text-xl font-bold ${(brief.security_findings || 0) > 0 ? 'text-red-600' : 'text-green-600'}`}>
                  {brief.security_findings || 0}
                </div>
                <div className="text-xs text-gray-500">보안 발견</div>
              </div>
            </div>
          ) : (
            <p className="text-gray-400 text-sm">거버넌스 브리프 데이터가 없습니다.</p>
          )}
          {brief?.recommendations?.map((r: string, i: number) => (
            <div key={i} className="text-sm text-gray-600 flex items-start gap-2 py-1 border-t border-gray-100">
              <span className="text-blue-500">▸</span>
              <span>{r}</span>
            </div>
          ))}
        </div>

        {/* Quick Actions */}
        <div className="card">
          <h2 className="text-lg font-semibold mb-3">빠른 작업 <span className="text-gray-400 text-sm font-normal">Quick Actions</span></h2>
          <div className="space-y-2">
            <button onClick={() => navigate('/sessions')} className="w-full text-left p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">새 세션 시작</div>
              <div className="text-xs text-gray-500">개발자 AI 코딩 세션 열기 →</div>
            </button>
            <button onClick={() => navigate('/security')} className="w-full text-left p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">보안 검사</div>
              <div className="text-xs text-gray-500">DLP/PII/시크릿 스캔 →</div>
            </button>
            <button onClick={() => navigate('/compliance')} className="w-full text-left p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">컴플라이언스 평가</div>
              <div className="text-xs text-gray-500">ISMS-P, CSAP 평가 실행 →</div>
            </button>
            <button onClick={() => navigate('/analytics')} className="w-full text-left p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">사용량 분석</div>
              <div className="text-xs text-gray-500">토큰 사용량 및 비용 →</div>
            </button>
            <button onClick={() => navigate('/audit')} className="w-full text-left p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">감사 로그</div>
              <div className="text-xs text-gray-500">최근 활동 추적 →</div>
            </button>
          </div>
        </div>
      </div>

      {/* Active Sessions & Recent Activity */}
      <div className="grid grid-cols-2 gap-6">
        <div className="card">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-lg font-semibold">활성 세션 <span className="text-gray-400 text-sm font-normal">Active Sessions</span></h2>
            <button onClick={() => navigate('/sessions')} className="text-xs text-blue-600 hover:underline">전체 보기 →</button>
          </div>
          {data?.active_sessions?.length > 0 ? (
            <div className="space-y-2">
              {data.active_sessions.map((s: any) => (
                <div key={s.id} onClick={() => navigate('/sessions')}
                  className="flex justify-between items-center py-2 border-b border-gray-100 last:border-0 cursor-pointer hover:bg-blue-50/30 rounded px-2 -mx-2">
                  <div>
                    <div className="font-medium text-sm">{s.title || s.session_id?.slice(0, 30)}</div>
                    <div className="text-xs text-gray-500">{s.task_purpose} · {formatTime(s.opened_at)}</div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-gray-400">{s.model_class}</span>
                    <span className="badge-green">{s.status}</span>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-gray-400 text-sm">활성 세션이 없습니다</p>
          )}
        </div>

        <div className="card">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-lg font-semibold">최근 활동 <span className="text-gray-400 text-sm font-normal">Recent Activity</span></h2>
            <button onClick={() => navigate('/audit')} className="text-xs text-blue-600 hover:underline">전체 보기 →</button>
          </div>
          {data?.recent_events?.length > 0 ? (
            <div className="space-y-2">
              {data.recent_events.slice(0, 8).map((e: any) => (
                <div key={e.id} onClick={() => navigate('/audit')}
                  className="flex justify-between items-center py-2 border-b border-gray-100 last:border-0 cursor-pointer hover:bg-blue-50/30 rounded px-2 -mx-2">
                  <div className="flex items-center gap-2">
                    <span>{activityIcon(e.action || e.event_type || '')}</span>
                    <div>
                      <div className="font-medium text-sm">{e.action || e.event_type}</div>
                      <div className="text-xs text-gray-400">{e.resource_type} · {formatTime(e.occurred_at)}</div>
                    </div>
                  </div>
                  <span className={e.result === 'success' ? 'badge-green' : 'badge-red'}>{e.result}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-gray-400 text-sm">최근 활동이 없습니다</p>
          )}
        </div>
      </div>
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }
