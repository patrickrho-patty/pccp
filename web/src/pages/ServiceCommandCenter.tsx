import { useState, useEffect } from 'react'

const healthIcon = (status: string) => {
  if (status === 'ok' || status === 'healthy') return { icon: '✅', color: 'text-green-600' }
  if (status === 'degraded') return { icon: '⚠️', color: 'text-yellow-600' }
  return { icon: '🔴', color: 'text-red-600' }
}

export default function ServiceCommandCenter() {
  const [dash, setDash] = useState<any>(null)
  const [health, setHealth] = useState<any>({})
  const [accounts, setAccounts] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      fetch('/api/dashboard', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/public/accounts', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/health', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/realtime/status', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/telemetry/snapshot', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
    ]).then(([d, accts, cp, rt, tel]) => {
      setDash(d); setAccounts(Array.isArray(accts) ? accts : []); setHealth({ cp, rt, tel }); setLoading(false)
    })
  }, [])

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  // Compute metrics
  const totalAccounts = accounts.length
  const activeSubs = accounts.filter(a => a.subscription_status === 'active').length
  const graceSubs = accounts.filter(a => a.subscription_status === 'grace').length
  const integrityFlags = accounts.filter(a => a.account_integrity_state !== 'normal').length
  const tsFlags = accounts.filter(a => a.trust_safety_state !== 'normal').length
  const capacityFlags = accounts.filter(a => a.capacity_state !== 'normal').length

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">서비스 커맨드 센터 <span className="text-gray-400 text-lg font-normal">Service Command Center</span></h1>
      <p className="text-xs text-gray-400 mb-6">퍼블릭 클라우드 서비스 운영 현황 · Aggregate service health (PRD §7.1) — 개별 세션 콘텐츠 표시 안함</p>

      {/* Top-level stat cards */}
      <div className="grid grid-cols-6 gap-3 mb-6">
        <div className="card py-3 px-4">
          <div className="text-2xl font-bold text-blue-600">{totalAccounts}</div>
          <div className="text-xs text-gray-500">총 계정 · Accounts</div>
        </div>
        <div className="card py-3 px-4">
          <div className="text-2xl font-bold text-green-600">{activeSubs}</div>
          <div className="text-xs text-gray-500">활성 구독 · Active Subs</div>
        </div>
        <div className="card py-3 px-4">
          <div className="text-2xl font-bold text-yellow-600">{graceSubs}</div>
          <div className="text-xs text-gray-500">유예 · Grace</div>
        </div>
        <div className="card py-3 px-4">
          <div className="text-2xl font-bold text-purple-600">{dash?.harnesses || 0}</div>
          <div className="text-xs text-gray-500">하네스 · Harnesses</div>
        </div>
        <div className="card py-3 px-4">
          <div className="text-2xl font-bold text-indigo-600">{dash?.active_sessions?.length || 0}</div>
          <div className="text-xs text-gray-500">활성 세션 · Sessions</div>
        </div>
        <div className="card py-3 px-4">
          <div className="text-2xl font-bold text-orange-600">{dash?.endpoints || 0}</div>
          <div className="text-xs text-gray-500">엔드포인트 · Endpoints</div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        {/* Risk Overview */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">리스크 현황 · Risk Overview</h3>
          <div className="space-y-2">
            <div className="flex justify-between items-center py-2 border-b border-gray-50">
              <span className="text-sm">계정 무결성 플래그</span>
              <span className={integrityFlags > 0 ? 'badge-yellow' : 'badge-green'}>{integrityFlags}</span>
            </div>
            <div className="flex justify-between items-center py-2 border-b border-gray-50">
              <span className="text-sm">신뢰 및 안전 케이스</span>
              <span className={tsFlags > 0 ? 'badge-red' : 'badge-green'}>{tsFlags}</span>
            </div>
            <div className="flex justify-between items-center py-2 border-b border-gray-50">
              <span className="text-sm">용량 제한 계정</span>
              <span className={capacityFlags > 0 ? 'badge-yellow' : 'badge-green'}>{capacityFlags}</span>
            </div>
            <div className="flex justify-between items-center py-2">
              <span className="text-sm">미해결 보안 발견</span>
              <span className="badge-gray">{dash?.open_findings || 0}</span>
            </div>
          </div>
        </div>

        {/* System Health */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">시스템 헬스 · System Health</h3>
          <div className="space-y-2">
            {[
              { name: 'Control Plane', status: health.cp?.status || 'unknown' },
              { name: 'PAPER Relay', status: health.rt?.relay_status || 'unknown' },
              { name: 'Event Spine', status: health.rt?.event_spine || 'unknown' },
              { name: 'Metering', status: health.rt?.metering || 'unknown' },
              { name: 'Model Catalog', status: health.rt?.catalog || 'unknown' },
              { name: 'PIA / Model Plane', status: health.rt?.pia || 'unknown' },
            ].map(s => {
              const h = healthIcon(s.status)
              return (
                <div key={s.name} className="flex justify-between items-center py-1.5 border-b border-gray-50 last:border-0">
                  <span className="text-sm">{s.name}</span>
                  <span className={`text-xs ${h.color}`}>{h.icon} {s.status}</span>
                </div>
              )
            })}
          </div>
        </div>
      </div>

      {/* Recent Activity (aggregate only) */}
      <div className="card mt-4">
        <h3 className="text-sm font-semibold mb-3">최근 플랫폼 이벤트 · Recent Platform Events</h3>
        {dash?.recent_activity && dash.recent_activity.length > 0 ? (
          <div className="space-y-1">
            {dash.recent_activity.slice(0, 10).map((a: any, i: number) => (
              <div key={i} className="flex items-center gap-3 text-xs py-1.5 border-b border-gray-50 last:border-0">
                <span className="font-mono text-gray-400 w-20">{a.occurred_at?.slice(11, 19)}</span>
                <span className="font-medium w-40 truncate">{a.action || a.event_type || '-'}</span>
                <span className="text-gray-500 truncate flex-1">{a.resource_type || ''}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-gray-400 text-center py-4">이벤트 없음</p>
        )}
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
