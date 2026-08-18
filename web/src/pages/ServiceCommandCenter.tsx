import { useState, useEffect, useCallback } from 'react'
import EmptyState from '../components/EmptyState'
import { Link } from 'react-router-dom'
import { showToast } from '../components/Toast'

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
  const [selectedStatus, setSelectedStatus] = useState<string | null>(null)
  const [supportCases, setSupportCases] = useState<any[]>([])
  const [abuseCases, setAbuseCases] = useState<any[]>([])


  // refreshHealth: poll /health + /api/realtime/status; a failed fetch
  // renders 'unreachable' (red) — never silently keeps stale 'ok'.
  const refreshHealth = useCallback(() => {
    Promise.all([
      fetch('/health', { headers: authHeaders() }).then(r => r.json()).catch(() => null),
      fetch('/api/realtime/status', { headers: authHeaders() }).then(r => (r.ok ? r.json() : null)).catch(() => null),
    ]).then(([cp, rt]) => {
      setHealth((h: any) => ({ ...h, cp: cp ?? { status: 'unreachable' }, rt: rt ?? { relay_status: 'unreachable' } }))
    })
  }, [])

  useEffect(() => {
    Promise.all([
      fetch('/api/dashboard', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/public/accounts', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/health', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/realtime/status', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/telemetry/snapshot', { headers: authHeaders() }).then(r => r.json()).catch(() => ({})),
      fetch('/api/public/support-cases', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/api/public/abuse-cases', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
    ]).then(([d, accts, cp, rt, tel, sc, ab]) => {
      setDash(d); setAccounts(Array.isArray(accts) ? accts : []); setHealth({ cp, rt, tel })
      setSupportCases(Array.isArray(sc) ? sc : [])
      setAbuseCases(Array.isArray(ab) ? ab : [])
      setLoading(false)
    })
    // Health refreshes on an interval + on tab focus: operators must
    // see CURRENT component state without a manual reload (a stale
    // tab or cached bundle must never freeze the panel).
    const timer = window.setInterval(refreshHealth, 10000)
    const onVisible = () => { if (document.visibilityState === 'visible') refreshHealth() }
    document.addEventListener('visibilitychange', onVisible)
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', onVisible) }
  }, [refreshHealth])

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  // Compute metrics
  const totalAccounts = accounts.length
  const activeSubs = accounts.filter(a => a.subscription_status === 'active')
  const graceSubs = accounts.filter(a => a.subscription_status === 'grace')
  const pastDueSubs = accounts.filter(a => a.subscription_status === 'past_due')
  const cancelledSubs = accounts.filter(a => a.subscription_status === 'cancelled')
  const expiredSubs = accounts.filter(a => a.subscription_status === 'expired')
  const integrityFlags = accounts.filter(a => a.account_integrity_state !== 'normal').length
  const tsFlags = accounts.filter(a => a.trust_safety_state !== 'normal').length
  const capacityFlags = accounts.filter(a => a.capacity_state !== 'normal').length

  const statusLabels: Record<string, { ko: string; en: string; desc: string; color: string }> = {
    active: { ko: '활성', en: 'Active', desc: '결제 완료, 정상 이용 중', color: 'green' },
    grace: { ko: '미납', en: 'Unpaid', desc: '결제 실패, 일시적 이용 가능', color: 'yellow' },
    past_due: { ko: '연체', en: 'Past Due', desc: '미납 기간 종료, 접근 제한', color: 'orange' },
    cancelled: { ko: '취소', en: 'Cancelled', desc: '사용자 취소', color: 'gray' },
    expired: { ko: '만료', en: 'Expired', desc: '구독 완전 만료', color: 'red' },
  }

  const cardBg: Record<string, string> = {
    blue: 'bg-blue-50 hover:bg-blue-100',
    green: 'bg-green-50 hover:bg-green-100',
    yellow: 'bg-yellow-50 hover:bg-yellow-100',
    orange: 'bg-orange-50 hover:bg-orange-100',
    gray: 'bg-gray-100 hover:bg-gray-200',
    red: 'bg-red-50 hover:bg-red-100',
  }
  const cardText: Record<string, string> = {
    blue: 'text-blue-600', green: 'text-green-600', yellow: 'text-yellow-600',
    orange: 'text-orange-600', gray: 'text-gray-500', red: 'text-red-600',
  }

  const subCards = [
    { filter: '', count: totalAccounts, ko: '총 계정', en: 'Total', desc: '등록된 모든 사용자', color: 'blue' },
    { filter: 'active', items: activeSubs, ko: '활성', en: 'Active', desc: '결제 완료, 정상 이용 중', color: 'green' },
    { filter: 'grace', items: graceSubs, ko: '미납', en: 'Unpaid', desc: '결제 실패, 일시적 이용 가능', color: 'yellow' },
    { filter: 'past_due', items: pastDueSubs, ko: '연체', en: 'Past Due', desc: '미납 기간 종료, 접근 제한', color: 'orange' },
    { filter: 'cancelled', items: cancelledSubs, ko: '취소', en: 'Cancelled', desc: '사용자 취소', color: 'gray' },
    { filter: 'expired', items: expiredSubs, ko: '만료', en: 'Expired', desc: '구독 완전 만료', color: 'red' },
  ]

  const filteredAccounts = selectedStatus
    ? accounts.filter(a => selectedStatus === 'all' ? true : a.subscription_status === selectedStatus)
    : []

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">서비스 커맨드 센터 <span className="text-gray-400 text-lg font-normal">Service Command Center</span></h1>
      <p className="text-xs text-gray-400 mb-6">퍼블릭 클라우드 서비스 운영 현황 · Aggregate service health (PRD §7.1) — 개별 세션 콘텐츠 표시 안함</p>

      {/* Subscription Status Breakdown */}
      <div className="card mb-6">
        <h3 className="text-sm font-semibold mb-1">구독 상태 분포 · Subscription Status</h3>
        <p className="text-xs text-gray-400 mb-4">카드를 클릭하면 해당 상태의 계정 목록을 볼 수 있습니다 · Click a card to see accounts</p>
        <div className="grid grid-cols-6 gap-3">
          {subCards.map((c) => (
            <div
              key={c.filter || 'all'}
              onClick={() => setSelectedStatus(c.filter === '' ? 'all' : c.filter)}
              className={`text-center p-3 rounded-lg cursor-pointer transition-all ${cardBg[c.color]} ${selectedStatus === (c.filter === '' ? 'all' : c.filter) ? 'ring-2 ring-blue-400' : ''}`}
            >
              <div className={`text-2xl font-bold ${cardText[c.color]}`}>{c.filter === '' ? c.count : (c.items?.length ?? 0)}</div>
              <div className="text-xs font-medium text-gray-700 mt-1">{c.ko}</div>
              <div className="text-[10px] text-gray-400">{c.en}</div>
              <div className="text-[10px] text-gray-400 mt-1">{c.desc}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Account list when a status is selected */}
      {selectedStatus && (
        <div className="card mb-6">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold">
              {selectedStatus === 'all' ? '전체 계정' : statusLabels[selectedStatus]?.ko + ' 계정'}
              <span className="text-gray-400 font-normal ml-2">
                {selectedStatus === 'all' ? 'All Accounts' : statusLabels[selectedStatus]?.en + ' Accounts'}
              </span>
            </h3>
            <button onClick={() => setSelectedStatus(null)} className="text-gray-400 hover:text-gray-600 text-sm">✕ 닫기</button>
          </div>
          {filteredAccounts.length === 0 ? (
            <EmptyState icon="🔍" title="해당 상태의 계정이 없습니다" />
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead>
                <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                  <th className="pb-2">이메일</th>
                  <th className="pb-2">이름</th>
                  <th className="pb-2">플랜</th>
                  <th className="pb-2">구독 상태</th>
                  <th className="pb-2">무결성</th>
                  <th className="pb-2">신뢰·안전</th>
                  <th className="pb-2">용량</th>
                </tr>
              </thead>
              <tbody>
                {filteredAccounts.map((a: any, i: number) => (
                  <tr key={a.id || i} className="border-b border-gray-50 last:border-0 hover:bg-blue-50/30">
                    <td className="py-2 text-sm">{a.email || '-'}</td>
                    <td className="py-2 text-sm">{a.display_name_ko || a.display_name || '-'}</td>
                    <td className="py-2 text-xs"><span className="badge-gray">{a.plan || '-'}</span></td>
                    <td className="py-2 text-xs">
                      <span className={cardText[statusLabels[a.subscription_status]?.color || 'gray']}>
                        {statusLabels[a.subscription_status]?.ko || a.subscription_status}
                      </span>
                    </td>
                    <td className="py-2">
                      <span className={a.account_integrity_state !== 'normal' ? 'badge-yellow' : 'badge-green'}>
                        {a.account_integrity_state || 'normal'}
                      </span>
                    </td>
                    <td className="py-2">
                      <span className={a.trust_safety_state !== 'normal' ? 'badge-red' : 'badge-green'}>
                        {a.trust_safety_state || 'normal'}
                      </span>
                    </td>
                    <td className="py-2">
                      <span className={a.capacity_state !== 'normal' ? 'badge-yellow' : 'badge-green'}>
                        {a.capacity_state || 'normal'}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Infrastructure Overview */}
      <div className="grid grid-cols-3 gap-3 mb-6">
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

      {/* Live Traffic (partially instrumented — honest placeholder, §6.1) */}
      <div className="card mb-6">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold">실시간 트래픽 · Live Traffic</h3>
          <span className="badge-yellow">부분 계측 · Partially instrumented</span>
        </div>
        <p className="text-xs text-gray-400 mb-4">DARI 교환 스트림·큐 깊이·슬롯 대기열은 아직 연결되지 않았습니다. 아래는 실측 가능한 지표입니다.</p>
        <div className="grid grid-cols-4 gap-3">
          <div className="text-center p-3 bg-purple-50 rounded-lg">
            <div className="text-2xl font-bold text-purple-600">{dash?.active_sessions?.length || 0}</div>
            <div className="text-xs text-gray-500">활성 세션 · Active Sessions</div>
          </div>
          <div className="text-center p-3 bg-blue-50 rounded-lg">
            <div className="text-2xl font-bold text-blue-600">{health.rt?.connected_clients ?? 0}</div>
            <div className="text-xs text-gray-500">실시간 클라이언트 · Realtime Clients</div>
          </div>
          <div className="text-center p-3 bg-green-50 rounded-lg">
            <div className="text-2xl font-bold text-green-600">{dash?.harnesses || 0}</div>
            <div className="text-xs text-gray-500">하네스 · Harnesses</div>
          </div>
          <div className="text-center p-3 bg-gray-50 rounded-lg">
            <div className="text-2xl font-bold text-gray-400">–</div>
            <div className="text-xs text-gray-500">DARI 교환 · Exchanges <span className="text-[10px] text-gray-400 block">미계측</span></div>
          </div>
        </div>
      </div>

      {/* Ops queues: T&S / Refund / Support */}
      <div className="grid grid-cols-3 gap-4 mb-6">
        {/* Trust & Safety queue — derived from live account records */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-1">T&S 케이스 큐 · Trust & Safety Queue</h3>
          <p className="text-xs text-gray-400 mb-3">케이스 라이프사이클은 미구축 — 계정 레코드의 trust_safety_state 플래그에서 도출(실측)</p>
          {accounts.filter(a => a.trust_safety_state && a.trust_safety_state !== 'normal').length === 0 ? (
            <EmptyState icon="🛡" title="T&S 플래그 계정 없음" message="trust_safety_state가 'normal'이 아닌 계정이 여기 표시됩니다" />
          ) : (
            <div className="space-y-1">
              {accounts.filter(a => a.trust_safety_state && a.trust_safety_state !== 'normal').map((a: any, i: number) => (
                <div key={a.id || i} className="flex items-center gap-2 text-xs p-2 bg-red-50 rounded">
                  <span className="font-medium truncate flex-1">{a.display_name_ko || a.display_name || a.email}</span>
                  <span className="badge-red">{a.trust_safety_state}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Refund queue — honest placeholder (payments not configured) */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-1">환불 큐 · Refund Queue</h3>
          <p className="text-xs text-gray-400 mb-3">결제 제공자가 미설정 상태입니다(Payments not_configured)</p>
          <EmptyState icon="💳" title="환불 요청이 없습니다" message="결제 연동 후 환불/크레딧 요청이 여기 표시됩니다 (데모 데이터 생성 없음)" />
        </div>

        {/* Support timeline — honest placeholder */}
        <div className="card">
          <h3 className="text-sm font-semibold mb-1">지원 타임라인 · Support Timeline</h3>
          <p className="text-xs text-gray-400 mb-3">계정별 지원 케이스 타임라인(§6.1 Support)</p>
          <EmptyState icon="🎧" title="지원 케이스 파이프라인 미연결" message="지원 케이스 시스템 연동 후 계정별 타임라인이 표시됩니다" />
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
              { name: 'DARI Relay', status: health.rt?.relay_status || 'unknown' },
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
      {/* Live case queues (11 A5 / 12 A6) */}
      <div className="grid grid-cols-2 gap-4 mt-6">
        <div className="card p-4">
          <h3 className="text-sm font-semibold mb-2">지원 케이스 · Support ({supportCases.filter((c: any) => c.status === 'open').length} 열림)</h3>
          {supportCases.length === 0 && <p className="text-xs text-gray-400">케이스 없음</p>}
          {supportCases.slice(0, 8).map((c: any) => (
            <div key={c.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700 truncate">{c.subject}</span>
              <span className="text-gray-400">{c.status} · {c.priority}</span>
            </div>
          ))}
        </div>
        <div className="card p-4">
          <h3 className="text-sm font-semibold mb-2">어뷰즈 케이스 · Abuse ({abuseCases.filter((c: any) => c.status === 'open').length} 열림)</h3>
          {abuseCases.length === 0 && <p className="text-xs text-gray-400">케이스 없음</p>}
          {abuseCases.slice(0, 8).map((c: any) => (
            <div key={c.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-red-600 truncate">⚠ {c.category}</span>
              <span className="text-gray-400">{c.status}{c.decision ? ` · ${c.decision.slice(0, 24)}` : ''}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function authHeaders(): Record<string, string> {
  const token = sessionStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
