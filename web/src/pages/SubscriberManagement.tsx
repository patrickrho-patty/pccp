import { useState, useEffect } from 'react'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['email', 'display_name', 'display_name_ko', 'oauth_provider'],
  searchPlaceholder: '이메일, 이름, OAuth 제공자로 검색...',
  dropdowns: [
    { key: 'subscription_status', label: '구독', options: [
      { value: 'active', label: '활성' }, { value: 'grace', label: '미납' },
      { value: 'past_due', label: '연체' }, { value: 'cancelled', label: '취소' },
      { value: 'expired', label: '만료' }, { value: 'none', label: '미가입' },
    ]},
    { key: 'subscription_plan', label: '플랜', options: [
      { value: 'free', label: 'Free' }, { value: 'developer', label: 'Developer' },
      { value: 'pro', label: 'Pro' }, { value: 'team', label: 'Team' },
      { value: 'enterprise', label: 'Enterprise' },
    ]},
    { key: 'account_integrity_state', label: '무결성', options: [
      { value: 'normal', label: '정상' }, { value: 'flagged', label: '주의' },
      { value: 'restricted', label: '제한' },
    ]},
    { key: 'oauth_provider', label: '로그인', options: [
      { value: 'google', label: 'Google' }, { value: 'apple', label: 'Apple' },
      { value: 'kakao', label: '카카오' }, { value: 'naver', label: '네이버' },
      { value: 'email', label: '이메일' },
    ]},
  ],
}

const subStatusLabel: Record<string, string> = {
  active: '활성', grace: '미납', past_due: '연체', cancelled: '취소', expired: '만료', none: '미가입', suspended: '정지',
}
const subStatusBadge: Record<string, string> = {
  active: 'badge-green', grace: 'badge-yellow', past_due: 'badge-orange', cancelled: 'badge-gray', expired: 'badge-red', none: 'badge-gray', suspended: 'badge-red',
}
const riskBadge = (s: string) => s === 'normal' ? 'badge-green' : s === 'flagged' || s === 'suspicious' ? 'badge-yellow' : 'badge-red'

export default function SubscriberManagement() {
  const [accounts, setAccounts] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [usage, setUsage] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const pageSize = 20

  useEffect(() => {
    Promise.all([
      fetch('/api/public/accounts', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/api/harnesses', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/api/analytics/usage', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
    ]).then(([accts, harns, usageData]) => {
      setAccounts(Array.isArray(accts) ? accts : [])
      setHarnesses(Array.isArray(harns) ? harns : [])
      setUsage(Array.isArray(usageData?.records || usageData) ? (usageData?.records || usageData) : [])
      setLoading(false)
    })
  }, [])

  const filtered = useFilteredData(accounts, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const getAccountHarnesses = (accountId: string) => harnesses.filter(h => h.account_id === accountId)
  const getAccountUsage = (accountId: string) => {
    // Sum usage records for sessions belonging to this account's harnesses
    const harnessIds = getAccountHarnesses(accountId).map(h => h.harness_id)
    const recs = usage.filter(u => harnessIds.includes(u.harness_id))
    const tokensIn = recs.filter(r => r.metric_type === 'tokens_in').reduce((a, r) => a + r.quantity, 0)
    const tokensOut = recs.filter(r => r.metric_type === 'tokens_out').reduce((a, r) => a + r.quantity, 0)
    return { tokensIn, tokensOut, total: tokensIn + tokensOut, records: recs.length }
  }

  // Aggregate stats
  const stats = {
    total: accounts.length,
    active: accounts.filter(a => a.subscription_status === 'active').length,
    grace: accounts.filter(a => a.subscription_status === 'grace').length,
    paying: accounts.filter(a => ['developer', 'pro', 'team', 'enterprise'].includes(a.subscription_plan)).length,
    flagged: accounts.filter(a => a.account_integrity_state !== 'normal').length,
    new30d: accounts.filter(a => {
      if (!a.created_at) return false
      const d = new Date(a.created_at)
      return Date.now() - d.getTime() < 30 * 24 * 60 * 60 * 1000
    }).length,
  }

  const selectedAccount = accounts.find(a => a.id === selectedId)

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">구독자 관리 <span className="text-gray-400 text-lg font-normal">Subscriber Management</span></h1>
      <p className="text-xs text-gray-400 mb-6">퍼블릭 클라우드 구독자 현황 · 계정, 결제, 사용량, 위험도 통합 관리</p>

      {/* Stats */}
      <div className="grid grid-cols-6 gap-3 mb-6">
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-blue-600">{stats.total}</div><div className="text-xs text-gray-500">총 가입자</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-green-600">{stats.active}</div><div className="text-xs text-gray-500">활성 구독</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-purple-600">{stats.paying}</div><div className="text-xs text-gray-500">유료 구독자</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-yellow-600">{stats.grace}</div><div className="text-xs text-gray-500">미납</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-red-600">{stats.flagged}</div><div className="text-xs text-gray-500">위험 플래그</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-indigo-600">{stats.new30d}</div><div className="text-xs text-gray-500">최근 30일 가입</div></div>
      </div>

      {/* Filter + Table */}
      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="grid grid-cols-12 gap-4">
        {/* Account List */}
        <div className={selectedId ? 'col-span-7' : 'col-span-12'}>
          <div className="card">
            <table className="w-full">
              <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3">구독자</th>
                <th className="pb-3">플랜</th>
                <th className="pb-3">상태</th>
                <th className="pb-3">토큰</th>
                <th className="pb-3">기기</th>
                <th className="pb-3">위험</th>
                <th className="pb-3">가입일</th>
              </tr></thead>
              <tbody>
                {paged.map(a => {
                  const u = getAccountUsage(a.id)
                  const hs = getAccountHarnesses(a.id)
                  return (
                    <tr key={a.id} className={`border-b border-gray-100 last:border-0 cursor-pointer ${selectedId === a.id ? 'bg-blue-50' : 'hover:bg-gray-50'}`}
                      onClick={() => setSelectedId(selectedId === a.id ? null : a.id)}>
                      <td className="py-3">
                        <div className="font-medium text-sm">{a.display_name_ko || a.display_name || a.email}</div>
                        <div className="text-xs text-gray-400">{a.email}</div>
                        {a.oauth_provider && <span className="text-[10px] text-gray-400">{a.oauth_provider}</span>}
                      </td>
                      <td className="py-3"><span className="badge-gray">{a.subscription_plan || 'free'}</span></td>
                      <td className="py-3"><span className={subStatusBadge[a.subscription_status] || 'badge-gray'}>{subStatusLabel[a.subscription_status] || a.subscription_status}</span></td>
                      <td className="py-3 text-xs text-gray-500">{(u.total / 1000).toFixed(1)}K</td>
                      <td className="py-3 text-xs">{hs.length}</td>
                      <td className="py-3"><span className={riskBadge(a.account_integrity_state)}>{a.account_integrity_state === 'normal' ? '정상' : '주의'}</span></td>
                      <td className="py-3 text-xs text-gray-400">{a.created_at?.slice(0, 10) || '-'}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
          </div>
        </div>

        {/* Detail Panel */}
        {selectedAccount && (
          <div className="col-span-5 space-y-4">
            <div className="card">
              <div className="flex items-center justify-between mb-3">
                <h3 className="text-sm font-semibold">구독자 상세</h3>
                <button onClick={() => setSelectedId(null)} className="text-gray-400 hover:text-gray-600">✕</button>
              </div>
              <div className="space-y-2 text-sm">
                <div className="flex justify-between"><span className="text-gray-500">이름</span><span className="font-medium">{selectedAccount.display_name_ko || selectedAccount.display_name || '-'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">이메일</span><span className="text-xs">{selectedAccount.email}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">플랜</span><span className="badge-gray">{selectedAccount.subscription_plan || 'free'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">구독 상태</span><span className={subStatusBadge[selectedAccount.subscription_status]}>{subStatusLabel[selectedAccount.subscription_status]}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">결제 만료</span><span className="text-xs">{selectedAccount.subscription_expiry?.slice(0, 10) || '-'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">OAuth</span><span className="text-xs">{selectedAccount.oauth_provider || 'email'} {selectedAccount.oauth_subject?.slice(0, 15)}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">로케일</span><span>{selectedAccount.locale || 'ko-KR'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">시간대</span><span>{selectedAccount.timezone || 'Asia/Seoul'}</span></div>
                <div className="flex justify-between"><span className="text-gray-500">가입일</span><span className="text-xs">{selectedAccount.created_at?.slice(0, 10)}</span></div>
              </div>
            </div>

            {/* Risk States */}
            <div className="card">
              <h3 className="text-sm font-semibold mb-3">리스크 상태 · Risk States (§10C)</h3>
              <div className="grid grid-cols-2 gap-2">
                <div className="text-center p-2 bg-gray-50 rounded">
                  <div className="text-xs text-gray-500">계정 무결성</div>
                  <span className={riskBadge(selectedAccount.account_integrity_state)}>{selectedAccount.account_integrity_state}</span>
                </div>
                <div className="text-center p-2 bg-gray-50 rounded">
                  <div className="text-xs text-gray-500">신뢰·안전</div>
                  <span className={riskBadge(selectedAccount.trust_safety_state)}>{selectedAccount.trust_safety_state}</span>
                </div>
                <div className="text-center p-2 bg-gray-50 rounded">
                  <div className="text-xs text-gray-500">플랫폼 보안</div>
                  <span className={riskBadge(selectedAccount.platform_security_state)}>{selectedAccount.platform_security_state}</span>
                </div>
                <div className="text-center p-2 bg-gray-50 rounded">
                  <div className="text-xs text-gray-500">용량 상태</div>
                  <span className={riskBadge(selectedAccount.capacity_state)}>{selectedAccount.capacity_state}</span>
                </div>
              </div>
            </div>

            {/* Usage */}
            <div className="card">
              <h3 className="text-sm font-semibold mb-3">사용량 · Usage</h3>
              {(() => {
                const u = getAccountUsage(selectedAccount.id)
                return (
                  <div className="grid grid-cols-3 gap-3">
                    <div className="text-center p-2 bg-blue-50 rounded"><div className="text-lg font-bold text-blue-600">{(u.tokensIn / 1000).toFixed(1)}K</div><div className="text-[10px] text-gray-500">입력 토큰</div></div>
                    <div className="text-center p-2 bg-green-50 rounded"><div className="text-lg font-bold text-green-600">{(u.tokensOut / 1000).toFixed(1)}K</div><div className="text-[10px] text-gray-500">출력 토큰</div></div>
                    <div className="text-center p-2 bg-purple-50 rounded"><div className="text-lg font-bold text-purple-600">{u.records}</div><div className="text-[10px] text-gray-500">추론 수</div></div>
                  </div>
                )
              })()}
            </div>

            {/* Devices */}
            <div className="card">
              <h3 className="text-sm font-semibold mb-3">등록 기기 · Registered Devices</h3>
              {getAccountHarnesses(selectedAccount.id).length === 0 ? (
                <p className="text-xs text-gray-400">등록된 기기 없음</p>
              ) : (
                <div className="space-y-1">
                  {getAccountHarnesses(selectedAccount.id).map(h => (
                    <div key={h.id} className="flex items-center justify-between text-xs p-2 bg-gray-50 rounded">
                      <div>
                        <div className="font-mono">{h.harness_id?.slice(0, 20)}</div>
                        <div className="text-gray-400">v{h.binary_version} · {h.status}</div>
                      </div>
                      <span className={h.status === 'active' ? 'badge-green' : 'badge-gray'}>{h.status}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Actions */}
            <div className="card">
              <h3 className="text-sm font-semibold mb-3">관리 작업 · Actions</h3>
              <div className="flex flex-wrap gap-2">
                <button className="btn-sm btn-secondary" onClick={() => alert('이메일 발송 기능은 마케팅 도구 연동 필요')}>📧 이메일</button>
                <button className="btn-sm btn-secondary" onClick={() => alert('플랜 변경')}>플랜 변경</button>
                <button className="btn-sm btn-secondary" onClick={() => { if (confirm('재인증 요청?')) {} }}>재인증 요청</button>
                <button className="btn-sm btn-danger" onClick={() => { if (confirm('계정 정지?')) {} }}>계정 정지</button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
