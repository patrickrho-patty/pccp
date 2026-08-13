import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'

export default function AccountPortal() {
  const [accounts, setAccounts] = useState<any[]>([])
  const [selected, setSelected] = useState<any>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState({ email: '', display_name: '', display_name_ko: '', plan: 'developer' })

  const load = () => {
    fetch('/api/public/accounts', { headers: authHeaders() })
      .then(r => r.json())
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

  const handleCreateSub = async (id: string, plan: string) => {
    try {
      await api.publicCreateSub(id, plan)
      load()
    } catch (err: any) {
      showToast('구독 생성 실패: ' + err.message)
    }
  }

  const handleLease = async (id: string) => {
    try {
      const lease = await api.publicLease(id)
      showToast(`용량 리스 발급됨\n슬롯: ${lease.active_agent_slots}개\n헤비: ${lease.heavy_slots}개\n유효: ${lease.valid_until}`)
    } catch (err: any) {
      showToast('리스 발급 실패: ' + err.message)
    }
  }

  const stateBadge = (s: string) => s === 'normal' || s === 'active' ? 'badge-green' : s === 'grace' || s === 'flagged' ? 'badge-yellow' : 'badge-red'

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
                <option value="free">Free (무료)</option>
                <option value="developer">Developer (개발자) — 2 하네스, 5 슬롯</option>
                <option value="pro">Pro (프로) — 3 하네스, 5 슬롯</option>
                <option value="team">Team (팀) — 3 하네스, 8 슬롯</option>
                <option value="enterprise">Enterprise (기업) — 10 하네스, 10 슬롯</option>
              </select>
            </div>
          </div>
          <div className="mt-4 p-3 bg-blue-50 rounded-lg text-sm text-blue-700">
            ℹ️ 퍼블릭 계정은 API 키 없이 OAuth로 인증하고 PAPER 프로토콜로 서비스를 이용합니다. (v2 §10C.1)
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
                <button onClick={() => handleLease(a.id)} className="btn-secondary text-xs">
                  용량 리스 발급 · Issue Lease
                </button>
                {a.subscription_status !== 'active' && (
                  <button onClick={() => handleCreateSub(a.id, 'developer')} className="btn-primary text-xs">
                    구독 시작 · Subscribe
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
