import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'
import EmptyState from '../components/EmptyState'

// Account Portal (web/24): public self-service. Access is keyed by the
// portal access token (issued once at account creation; never stored
// in the console). The portal never exposes transferable API
// credentials (§6.6) and always offers a way back to the console (C).

const PLANS = [
  { value: 'free', ko: '무료', en: 'Free' },
  { value: 'developer', ko: '개발자', en: 'Developer' },
  { value: 'pro', ko: '프로', en: 'Pro' },
  { value: 'team', ko: '팀', en: 'Team' },
  { value: 'enterprise', ko: '엔터프라이즈', en: 'Enterprise' },
]

export default function AccountPortal() {
  const [token, setToken] = useState('')
  const [self, setSelf] = useState<any>(null)
  const [loading, setLoading] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [createForm, setCreateForm] = useState({ email: '', display_name: '', display_name_ko: '', plan: 'free' })
  const [newToken, setNewToken] = useState('')
  const [planSelect, setPlanSelect] = useState('')
  const [supportSubject, setSupportSubject] = useState('')

  const loadSelf = async (tk: string) => {
    if (!tk.trim()) return
    setLoading(true)
    try {
      const res = await fetch('/api/public/portal/self', { headers: { Authorization: `Bearer ${tk.trim()}` } })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'invalid token')
      setSelf(data)
      setPlanSelect(data.subscription?.plan || '')
    } catch (e: any) {
      showToast(e?.message || '포털 접근 실패', 'error')
      setSelf(null)
    } finally { setLoading(false) }
  }

  const createAccount = async () => {
    if (!createForm.email.includes('@')) {
      showToast('유효한 이메일이 필요합니다', 'error')
      return
    }
    try {
      const res = await api.publicCreateAccount(createForm.email, createForm.display_name, createForm.display_name_ko, createForm.plan)
      const created: any = res
      const acc = created.account || created
      const tok = created.portal_token
      if (!tok) {
        showToast('계정 생성 완료 — 포털 토큰은 생성 응답에서만 확인할 수 있습니다', 'info')
      } else {
        setNewToken(tok)
        setToken(tok)
        await loadSelf(tok)
      }
      void acc
      setShowCreate(false)
    } catch (e: any) { showToast(e?.message || '생성 실패', 'error') }
  }

  const changePlan = async () => {
    if (!token || !planSelect) return
    try {
      await fetch('/api/public/portal/plan', {
        method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ plan: planSelect }),
      })
      showToast('플랜 변경 완료', 'success')
      loadSelf(token)
    } catch { showToast('실패', 'error') }
  }

  const signOutAll = async () => {
    if (!token) return
    try {
      const res = await fetch('/api/public/portal/sign-out-all', { method: 'POST', headers: { Authorization: `Bearer ${token}` } })
      const out = await res.json()
      if (!res.ok) throw new Error(out.error || 'failed')
      showToast(`모든 세션 해지 완료 (리스 ${out.leases_revoked}개)`, 'success')
      loadSelf(token)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const fileSupport = async () => {
    if (!token || !supportSubject.trim()) {
      showToast('제목을 입력하세요', 'error')
      return
    }
    try {
      await fetch('/api/public/portal/support', {
        method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ subject: supportSubject.slice(0, 60), description: supportSubject }),
      })
      showToast('지원 요청 접수 완료', 'success')
      setSupportSubject('')
      loadSelf(token)
    } catch { showToast('실패', 'error') }
  }

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">계정 포털 · Account Portal</h2>
          <p className="text-[11px] text-gray-400">퍼블릭 구독자 셀프서비스 — 포털 액세스 토큰으로 접근합니다.</p>
        </div>
        {/* Console switcher (C): never trap the user. */}
        <Link to="/" className="btn-sm btn-secondary">콘솔로 돌아가기</Link>
      </div>

      {!self ? (
        <div className="card p-6 space-y-3 max-w-lg">
          <input className="input text-xs w-full font-mono" placeholder="포털 액세스 토큰"
            value={token} onChange={e => setToken(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') loadSelf(token) }} />
          <div className="flex gap-2">
            <button className="btn-sm btn-primary" onClick={() => loadSelf(token)} disabled={loading}>
              {loading ? '로딩...' : '포털 열기'}
            </button>
            <button className="btn-sm btn-secondary" onClick={() => setShowCreate(!showCreate)}>새 계정 만들기</button>
          </div>
          {showCreate && (
            <div className="space-y-2 border-t border-gray-100 pt-3">
              <input className="input text-xs w-full" placeholder="이메일" value={createForm.email} onChange={e => setCreateForm({ ...createForm, email: e.target.value })} />
              <input className="input text-xs w-full" placeholder="이름" value={createForm.display_name} onChange={e => setCreateForm({ ...createForm, display_name: e.target.value })} />
              <select className="input text-xs w-full" value={createForm.plan} onChange={e => setCreateForm({ ...createForm, plan: e.target.value })}>
                {PLANS.map(p => <option key={p.value} value={p.value}>{p.ko} {p.en}</option>)}
              </select>
              <button className="btn-sm btn-primary" onClick={createAccount}>계정 생성</button>
            </div>
          )}
          {newToken && (
            <div className="p-3 bg-amber-50 rounded text-[11px]">
              <div className="font-semibold text-amber-700">포털 토큰 (1회 표시 — 안전하게 보관하세요)</div>
              <div className="font-mono break-all">{newToken}</div>
            </div>
          )}
          {!newToken && <EmptyState icon="🔑" title="포털에 접속하세요" message="계정 생성 시 발급된 포털 액세스 토큰을 입력하세요." />}
        </div>
      ) : (
        <div className="space-y-3">
          <div className="card p-4">
            <h3 className="text-xs font-bold mb-2">{self.account?.display_name_ko || self.account?.display_name} ({self.account?.email})</h3>
            <div className="text-[11px] text-gray-500 space-y-1">
              <div className="flex justify-between"><span>플랜</span><span>{self.subscription?.plan || 'none'}</span></div>
              <div className="flex justify-between"><span>구독 상태</span><span>{self.account?.subscription_status}</span></div>
              <div className="flex justify-between"><span>만료</span><span>{(self.subscription?.expires_at || '').slice(0, 10)}</span></div>
              <div className="flex justify-between"><span>용량 리스</span><span>{self.leases?.length || 0}개</span></div>
              <div className="flex justify-between"><span>사용 기록</span><span>{self.usage_records || 0}건</span></div>
            </div>
          </div>

          <div className="card p-4 space-y-2">
            <h3 className="text-xs font-bold">플랜 변경</h3>
            <div className="flex gap-2">
              <select className="input text-xs" value={planSelect} onChange={e => setPlanSelect(e.target.value)}>
                {PLANS.map(p => <option key={p.value} value={p.value}>{p.ko} {p.en}</option>)}
              </select>
              <button className="btn-sm btn-primary" onClick={changePlan}>변경</button>
            </div>
          </div>

          <div className="card p-4 space-y-2">
            <h3 className="text-xs font-bold">보안</h3>
            <button className="btn-sm btn-danger" onClick={signOutAll}>모든 세션 해지 (Sign out all)</button>
            <p className="text-[10px] text-gray-400">모든 용량 리스를 해지합니다 — 하네스 연결이 즉시 끊깁니다.</p>
          </div>

          <div className="card p-4 space-y-2">
            <h3 className="text-xs font-bold">지원 요청</h3>
            <div className="flex gap-2">
              <input className="input text-xs flex-1" placeholder="요청 내용..." value={supportSubject} onChange={e => setSupportSubject(e.target.value)} />
              <button className="btn-sm btn-secondary" onClick={fileSupport}>접수</button>
            </div>
            {(self.support_cases || []).slice(0, 5).map((c: any) => (
              <div key={c.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
                <span className="text-gray-700 truncate">{c.subject}</span>
                <span className="text-gray-400">{c.status}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
