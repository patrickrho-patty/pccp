import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'

// 플랜 사양 — backend publiccloud.getPlanConfig과 동일 (internal/publiccloud/service.go)
const PLANS: { id: string; label: string; harnesses: number; active: number; normal: number; heavy: number }[] = [
  { id: 'free', label: 'Free (무료)', harnesses: 1, active: 1, normal: 1, heavy: 0 },
  { id: 'developer', label: 'Developer (개발자)', harnesses: 2, active: 2, normal: 5, heavy: 1 },
  { id: 'pro', label: 'Pro (프로)', harnesses: 3, active: 2, normal: 5, heavy: 2 },
  { id: 'team', label: 'Team (팀)', harnesses: 3, active: 3, normal: 8, heavy: 3 },
  { id: 'enterprise', label: 'Enterprise (기업)', harnesses: 10, active: 5, normal: 10, heavy: 5 },
]

// 아직 백엔드 라우트가 없는 셀프 서비스 기능 (spec 24 §A/B — honest placeholders)
const NOT_YET_AVAILABLE = [
  { ko: '결제 수단 · 인보이스', en: 'Invoices / Payment', ref: '§6.6' },
  { ko: '사용량 이력 · 페어유즈', en: 'Usage History / Fair Use', ref: '§10C.4' },
  { ko: '보안 이벤트', en: 'Security Events', ref: '§6.6' },
  { ko: '활성 세션 조회 · 원격 종료', en: 'Active Sessions / Remote Kill', ref: '§6.6' },
  { ko: '계정 복구', en: 'Account Recovery', ref: '§6.6' },
  { ko: '데이터 내보내기 · 삭제', en: 'Data Export / Delete', ref: '§6.6' },
  { ko: '지원 요청', en: 'Support Request', ref: '§6.6' },
]

export default function AccountPortal() {
  const [accounts, setAccounts] = useState<any[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState({ email: '', display_name: '', display_name_ko: '', plan: 'developer' })
  const [expanded, setExpanded] = useState<string | null>(null)
  const [leases, setLeases] = useState<Record<string, any>>({})
  const [planChange, setPlanChange] = useState<Record<string, string>>({})

  const load = () => {
    api.publicAccounts()
      .then(data => setAccounts(Array.isArray(data) ? data : []))
      .catch(() => setAccounts([]))
  }

  useEffect(() => { load() }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.publicCreateAccount(form)
      setShowCreate(false)
      setForm({ email: '', display_name: '', display_name_ko: '', plan: 'developer' })
      load()
    } catch (err: any) {
      showToast('계정 생성 실패: ' + err.message)
    }
  }

  const handleLease = async (id: string) => {
    try {
      const lease = await api.publicLease(id)
      setLeases(prev => ({ ...prev, [id]: lease }))
    } catch (err: any) {
      showToast('리스 발급 실패: ' + err.message)
    }
  }

  const handlePlanChange = async (id: string, plan: string) => {
    try {
      await api.publicCreateSub(id, plan)
      showToast(`플랜이 ${plan}(으)로 변경되었습니다`)
      setPlanChange(prev => ({ ...prev, [id]: '' }))
      load()
    } catch (err: any) {
      showToast('플랜 변경 실패: ' + err.message)
    }
  }

  const stateBadge = (s: string) => s === 'normal' || s === 'active' ? 'badge-green' : s === 'grace' || s === 'flagged' ? 'badge-yellow' : 'badge-red'

  const fmtTime = (t?: string) => t ? t.slice(0, 19).replace('T', ' ') : ''

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-2xl font-bold">계정 포털 <span className="text-gray-400 text-lg font-normal">Account Portal</span></h1>
          <p className="text-sm text-gray-500 mt-1">퍼블릭 클라우드 구독자 셀프 서비스 · Public Cloud Subscriber Self-Service (v2 §6.6)</p>
        </div>
        <button onClick={() => setShowCreate(!showCreate)} className="btn-primary">
          {showCreate ? '취소' : '+ 계정 생성'}
        </button>
      </div>

      {showCreate && (
        <form onSubmit={handleCreate} className="card mb-6">
          <h2 className="text-lg font-semibold mb-4">새 퍼블릭 계정 · New Public Account</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">이메일 · Email</label>
              <input className="input" type="email" required value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} />
            </div>
            <div>
              <label className="label">이름 · Name</label>
              <input className="input" value={form.display_name} onChange={e => setForm({ ...form, display_name: e.target.value })} />
            </div>
            <div>
              <label className="label">한글 이름 · Korean Name</label>
              <input className="input" value={form.display_name_ko} onChange={e => setForm({ ...form, display_name_ko: e.target.value })} placeholder="김개발" />
            </div>
            <div>
              <label className="label">플랜 · Plan</label>
              <select className="input" value={form.plan} onChange={e => setForm({ ...form, plan: e.target.value })}>
                {PLANS.map(p => (
                  <option key={p.id} value={p.id}>
                    {p.label} — 하네스 {p.harnesses}, 일반 슬롯 {p.normal}, 헤비 {p.heavy}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div className="mt-4 p-3 bg-blue-50 rounded-lg text-sm text-blue-700">
            ℹ️ 퍼블릭 계정은 API 키 없이 OAuth로 인증하고 DARI 프로토콜로 서비스를 이용합니다. (v2 §10C.1)
          </div>
          <button type="submit" className="btn-primary mt-4">생성 · Create Account</button>
        </form>
      )}

      {/* Account List */}
      {accounts.length === 0 && !showCreate ? (
        <div className="card text-center py-12">
          <p className="text-gray-400 mb-2">등록된 퍼블릭 계정이 없습니다.</p>
          <p className="text-sm text-gray-400">"+ 계정 생성" 버튼으로 첫 퍼블릭 구독자를 만드세요.</p>
        </div>
      ) : (
        <div className="space-y-4">
          {accounts.map(a => (
            <div key={a.id} className="card">
              {/* Account Header */}
              <div className="flex items-start justify-between mb-4">
                <div>
                  <h3 className="font-bold text-lg">{a.display_name_ko || a.display_name}</h3>
                  <p className="text-sm text-gray-500">{a.email}</p>
                  <p className="text-xs text-gray-400 font-mono mt-1">{a.id}</p>
                </div>
                <div className="flex items-center gap-2">
                  <span className={stateBadge(a.subscription_status)}>{a.subscription_status}</span>
                  <span className="badge-blue">{a.subscription_plan || 'none'}</span>
                </div>
              </div>

              {/* Subscription & Slots */}
              <div className="grid grid-cols-5 gap-3 mb-4">
                <div className="bg-gray-50 rounded p-2 text-center">
                  <div className="text-lg font-bold">{a.max_harnesses}</div>
                  <div className="text-xs text-gray-500">최대 하네스</div>
                </div>
                <div className="bg-gray-50 rounded p-2 text-center">
                  <div className="text-lg font-bold">{a.max_active_harnesses}</div>
                  <div className="text-xs text-gray-500">동시 하네스</div>
                </div>
                <div className="bg-gray-50 rounded p-2 text-center">
                  <div className="text-lg font-bold">{a.normal_work_slots}</div>
                  <div className="text-xs text-gray-500">일반 슬롯</div>
                </div>
                <div className="bg-gray-50 rounded p-2 text-center">
                  <div className="text-lg font-bold">{a.heavy_work_slots}</div>
                  <div className="text-xs text-gray-500">헤비 슬롯</div>
                </div>
                <div className="bg-gray-50 rounded p-2 text-center">
                  <div className="text-lg font-bold">{a.background_slots}</div>
                  <div className="text-xs text-gray-500">백그라운드</div>
                </div>
              </div>
              {a.subscription_expiry && (
                <p className="text-xs text-gray-400 mb-4">구독 만료 · Expiry: {fmtTime(a.subscription_expiry)}</p>
              )}

              {/* Risk States */}
              <div className="grid grid-cols-4 gap-2 mb-4">
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-gray-500">무결성</span>
                  <span className={stateBadge(a.account_integrity_state)}>{a.account_integrity_state}</span>
                </div>
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-gray-500">T&S</span>
                  <span className={stateBadge(a.trust_safety_state)}>{a.trust_safety_state}</span>
                </div>
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-gray-500">보안</span>
                  <span className={stateBadge(a.platform_security_state)}>{a.platform_security_state}</span>
                </div>
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-gray-500">용량</span>
                  <span className={stateBadge(a.capacity_state)}>{a.capacity_state}</span>
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 pt-3 border-t border-gray-100">
                <button onClick={() => setExpanded(expanded === a.id ? null : a.id)} className="btn-secondary text-xs">
                  {expanded === a.id ? '관리 닫기' : '계정 관리 · Manage'}
                </button>
                <button onClick={() => handleLease(a.id)} className="btn-secondary text-xs">
                  용량 리스 발급 · Issue Lease
                </button>
                {a.subscription_status !== 'active' && (
                  <button onClick={() => handlePlanChange(a.id, 'developer')} className="btn-primary text-xs">
                    구독 시작 · Subscribe
                  </button>
                )}
              </div>

              {/* Inline capacity lease result (replaces alert) */}
              {leases[a.id] && (
                <div className="mt-3 p-3 bg-green-50 border border-green-200 rounded-lg">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-semibold text-green-800">용량 리스 발급됨 · Capacity Lease Issued (§10C.5)</span>
                    <span className="text-[10px] text-green-700 font-mono">유효: {fmtTime(leases[a.id].valid_until)} (TTL 5분)</span>
                  </div>
                  <div className="grid grid-cols-4 gap-2 text-center">
                    <div><div className="text-sm font-bold text-green-800">{leases[a.id].active_agent_slots}</div><div className="text-[10px] text-green-700">에이전트 슬롯</div></div>
                    <div><div className="text-sm font-bold text-green-800">{leases[a.id].heavy_slots}</div><div className="text-[10px] text-green-700">헤비 슬롯</div></div>
                    <div><div className="text-sm font-bold text-green-800">{leases[a.id].background_slots}</div><div className="text-[10px] text-green-700">백그라운드</div></div>
                    <div><div className="text-sm font-bold text-green-800">{leases[a.id].priority_weight}</div><div className="text-[10px] text-green-700">우선순위</div></div>
                  </div>
                </div>
              )}

              {/* Manage panel */}
              {expanded === a.id && (
                <div className="mt-4 pt-4 border-t border-gray-100 space-y-4">
                  {/* Plan change — real: POST /api/public/accounts/{id}/subscription updates entitlements */}
                  <div>
                    <h4 className="text-sm font-semibold mb-2">플랜 변경 · Plan Change</h4>
                    <div className="flex gap-2">
                      <select
                        className="input flex-1"
                        value={planChange[a.id] ?? a.subscription_plan ?? 'developer'}
                        onChange={e => setPlanChange(prev => ({ ...prev, [a.id]: e.target.value }))}
                      >
                        {PLANS.map(p => (
                          <option key={p.id} value={p.id} disabled={p.id === a.subscription_plan}>
                            {p.label} — 하네스 {p.harnesses}, 일반 {p.normal}, 헤비 {p.heavy}{p.id === a.subscription_plan ? ' (현재)' : ''}
                          </option>
                        ))}
                      </select>
                      <button
                        onClick={() => handlePlanChange(a.id, planChange[a.id] ?? a.subscription_plan ?? 'developer')}
                        disabled={!planChange[a.id] || planChange[a.id] === a.subscription_plan}
                        className="btn-primary text-xs disabled:opacity-50"
                      >
                        적용
                      </button>
                    </div>
                    <p className="text-[10px] text-gray-400 mt-1">즉시 적용 · 결제/정산 미연동 — 인보이스는 아직 제공되지 않습니다.</p>
                  </div>

                  {/* Harness management — links to the existing My Devices surface */}
                  <div>
                    <h4 className="text-sm font-semibold mb-2">하네스 관리 · Harnesses</h4>
                    <p className="text-xs text-gray-500">
                      하네스 목록·해지는 <Link to="/harnesses" className="text-patty-600 hover:underline">내 기기 (My Devices)</Link> 메뉴에서 이용하세요. 계정 단위 일괄 로그아웃은 아직 제공되지 않습니다.
                    </p>
                  </div>

                  {/* Honest placeholders — no backend routes yet (spec 24 Phase 2/3) */}
                  <div>
                    <h4 className="text-sm font-semibold mb-2">아직 제공되지 않는 기능 · Not Yet Available</h4>
                    <div className="grid grid-cols-2 gap-2">
                      {NOT_YET_AVAILABLE.map(item => (
                        <div key={item.en} className="flex items-center justify-between p-2 rounded-lg border border-gray-100 bg-gray-50">
                          <div>
                            <div className="text-xs text-gray-600">{item.ko}</div>
                            <div className="text-[10px] text-gray-400">{item.en} · {item.ref}</div>
                          </div>
                          <span className="badge-gray text-[10px]">준비 중</span>
                        </div>
                      ))}
                    </div>
                    <p className="text-[10px] text-gray-400 mt-2">해당 기능의 백엔드 API가 아직 라우팅되지 않아 실제 동작 대신 상태를 정직하게 표시합니다.</p>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
