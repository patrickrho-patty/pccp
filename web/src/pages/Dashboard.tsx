import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Dashboard() {
  const [data, setData] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.dashboard().then(setData).finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="text-gray-500">로딩 중...</div>
  if (!data) return <div>데이터 없음</div>

  const stats = [
    { label: '사용자', labelEn: 'Users', value: data.users, color: 'bg-blue-500' },
    { label: '하네스', labelEn: 'Harnesses', value: data.harnesses, color: 'bg-green-500' },
    { label: '세션', labelEn: 'Sessions', value: data.sessions, color: 'bg-purple-500' },
    { label: '엔드포인트', labelEn: 'Endpoints', value: data.endpoints, color: 'bg-orange-500' },
  ]

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">대시보드 <span className="text-gray-400 text-lg font-normal">Dashboard</span></h1>

      <div className="grid grid-cols-4 gap-4 mb-8">
        {stats.map((stat) => (
          <div key={stat.label} className="card">
            <div className={`w-3 h-3 rounded-full ${stat.color} mb-3`} />
            <div className="text-3xl font-bold">{stat.value}</div>
            <div className="text-sm text-gray-500">{stat.label} · {stat.labelEn}</div>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-2 gap-6">
        <div className="card">
          <h2 className="text-lg font-semibold mb-4">활성 세션 <span className="text-gray-400 text-sm font-normal">Active Sessions</span></h2>
          {data.active_sessions?.length === 0 ? (
            <p className="text-gray-400 text-sm">활성 세션이 없습니다</p>
          ) : (
            <div className="space-y-2">
              {data.active_sessions?.map((s: any) => (
                <div key={s.id} className="flex justify-between items-center py-2 border-b border-gray-100 last:border-0">
                  <div>
                    <div className="font-medium text-sm">{s.title || s.session_id?.slice(0, 30)}</div>
                    <div className="text-xs text-gray-500">{s.task_purpose}</div>
                  </div>
                  <span className="badge-green">{s.status}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="card">
          <h2 className="text-lg font-semibold mb-4">최근 활동 <span className="text-gray-400 text-sm font-normal">Recent Activity</span></h2>
          {data.recent_events?.length === 0 ? (
            <p className="text-gray-400 text-sm">최근 활동이 없습니다</p>
          ) : (
            <div className="space-y-2">
              {data.recent_events?.map((e: any) => (
                <div key={e.id} className="flex justify-between items-center py-2 border-b border-gray-100 last:border-0">
                  <div>
                    <div className="font-medium text-sm">{e.action}</div>
                    <div className="text-xs text-gray-500">{e.resource_type} · {e.occurred_at}</div>
                  </div>
                  <span className="badge-green">{e.result}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
