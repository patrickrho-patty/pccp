import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Dashboard() {
  const [data, setData] = useState<any>(null)
  const [brief, setBrief] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      fetch('/api/dashboard', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/korean/governance-brief', { headers: authHeaders() }).then(r => r.json()).catch(() => null),
    ]).then(([dash, brf]) => {
      setData(dash)
      setBrief(brf)
      setLoading(false)
    })
  }, [])

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  const stats = [
    { label: '사용자', labelEn: 'Users', value: data?.users || 0, icon: '◉', color: 'bg-blue-500' },
    { label: '하네스', labelEn: 'Harnesses', value: data?.harnesses || 0, icon: '⬡', color: 'bg-green-500' },
    { label: '활성 세션', labelEn: 'Active Sessions', value: data?.active_sessions?.length || 0, icon: '◐', color: 'bg-purple-500' },
    { label: '엔드포인트', labelEn: 'Endpoints', value: data?.endpoints || 0, icon: '◇', color: 'bg-orange-500' },
  ]

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">대시보드 <span className="text-gray-400 text-lg font-normal">Enterprise Command Center</span></h1>

      {/* Stat Cards */}
      <div className="grid grid-cols-4 gap-4 mb-6">
        {stats.map(stat => (
          <div key={stat.labelEn} className="card">
            <div className={`w-3 h-3 rounded-full ${stat.color} mb-3`} />
            <div className="text-3xl font-bold">{stat.value}</div>
            <div className="text-sm text-gray-500">{stat.label} · {stat.labelEn}</div>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-3 gap-6 mb-6">
        {/* Governance Brief */}
        <div className="card col-span-2">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">주간 AI 거버넌스 브리프 <span className="text-gray-400 text-sm font-normal">Weekly Governance Brief</span></h2>
            {brief?.compliance_status && (
              <span className={brief.compliance_status.includes('양호') ? 'badge-green' : brief.compliance_status.includes('주의') ? 'badge-red' : 'badge-yellow'}>
                {brief.compliance_status}
              </span>
            )}
          </div>
          {brief ? (
            <div className="grid grid-cols-4 gap-4 mb-4">
              <div className="text-center p-2 bg-gray-50 rounded">
                <div className="text-xl font-bold">{brief.total_sessions || 0}</div>
                <div className="text-xs text-gray-500">세션</div>
              </div>
              <div className="text-center p-2 bg-gray-50 rounded">
                <div className="text-xl font-bold">{brief.active_harnesses || 0}</div>
                <div className="text-xs text-gray-500">하네스</div>
              </div>
              <div className="text-center p-2 bg-gray-50 rounded">
                <div className="text-xl font-bold">{brief.model_invocations || 0}</div>
                <div className="text-xs text-gray-500">AI 추론</div>
              </div>
              <div className="text-center p-2 bg-gray-50 rounded">
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
              <span className="text-patty-500">▸</span>
              <span>{r}</span>
            </div>
          ))}
        </div>

        {/* Quick Actions */}
        <div className="card">
          <h2 className="text-lg font-semibold mb-3">빠른 작업 <span className="text-gray-400 text-sm font-normal">Quick Actions</span></h2>
          <div className="space-y-2">
            <a href="/sessions" className="block p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">새 세션 시작</div>
              <div className="text-xs text-gray-500">개발자 AI 코딩 세션 열기</div>
            </a>
            <a href="/security" className="block p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">보안 검사</div>
              <div className="text-xs text-gray-500">DLP/PII/시크릿 스캔</div>
            </a>
            <a href="/compliance" className="block p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">컴플라이언스 평가</div>
              <div className="text-xs text-gray-500">ISMS-P, CSAP 평가 실행</div>
            </a>
            <a href="/analytics" className="block p-3 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors">
              <div className="text-sm font-medium">사용량 분석</div>
              <div className="text-xs text-gray-500">토큰 사용량 및 비용</div>
            </a>
          </div>
        </div>
      </div>

      {/* Active Sessions & Recent Activity */}
      <div className="grid grid-cols-2 gap-6">
        <div className="card">
          <h2 className="text-lg font-semibold mb-4">활성 세션 <span className="text-gray-400 text-sm font-normal">Active Sessions</span></h2>
          {data?.active_sessions?.length > 0 ? (
            <div className="space-y-2">
              {data.active_sessions.map((s: any) => (
                <div key={s.id} className="flex justify-between items-center py-2 border-b border-gray-100 last:border-0">
                  <div>
                    <div className="font-medium text-sm">{s.title || s.session_id?.slice(0, 30)}</div>
                    <div className="text-xs text-gray-500">{s.task_purpose}</div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-gray-400">{s.model_class}</span>
                    <span className="badge-green">{s.status}</span>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="text-center py-6">
              <p className="text-gray-400 text-sm">활성 세션이 없습니다</p>
              <a href="/sessions" className="text-patty-600 text-sm hover:underline mt-2 inline-block">새 세션 시작 →</a>
            </div>
          )}
        </div>

        <div className="card">
          <h2 className="text-lg font-semibold mb-4">최근 활동 <span className="text-gray-400 text-sm font-normal">Recent Activity</span></h2>
          {data?.recent_events?.length > 0 ? (
            <div className="space-y-2">
              {data.recent_events.slice(0, 10).map((e: any) => (
                <div key={e.id} className="flex justify-between items-center py-2 border-b border-gray-100 last:border-0">
                  <div>
                    <div className="font-medium text-sm">{e.action}</div>
                    <div className="text-xs text-gray-500">{e.resource_type} · {e.occurred_at?.slice(0, 19)}</div>
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

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
