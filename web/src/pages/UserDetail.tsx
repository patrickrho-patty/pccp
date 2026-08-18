import { useState, useEffect } from 'react'
import { useParams, Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { EntitySelect } from '../components/EntitySelect'
import { FavoriteStar } from '../hooks/useFavorites'
import { showToast } from '../components/Toast'
import { formatUsageAmount, UsageReport } from '../components/UsageReport'
import { userActions, userActionSpec, applyUserLifecycle, canIssueEnrollment, STATUS_KO, STATUS_BADGE, UserLifecycleAction } from '../userLifecycle'

// UserDetail (web/01 B4): /users/:id with tabs — Overview /
// Entitlement / Sessions / Harnesses / Usage / Audit / Contractor.
const TABS = [
  { id: 'overview', label: '개요', en: 'Overview' },
  { id: 'entitlements', label: '권한', en: 'Entitlement' },
  { id: 'sessions', label: '세션', en: 'Sessions' },
  { id: 'harnesses', label: '하네스', en: 'Harnesses' },
  { id: 'usage', label: '사용량', en: 'Usage' },
  { id: 'audit', label: '감사', en: 'Audit' },
  { id: 'contractor', label: '계약', en: 'Contractor' },
]

export default function UserDetail() {
  const { id } = useParams<{ id: string }>()
  const [params, setParams] = useSearchParams()
  const tab = params.get('tab') || 'overview'
  const setTab = (t: string) => setParams(t === 'overview' ? {} : { tab: t })

  const [user, setUser] = useState<any>(null)
  const [sessions, setSessions] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [allHarnesses, setAllHarnesses] = useState<any[]>([])
  const [auditEvents, setAuditEvents] = useState<any[]>([])
  const [usage, setUsage] = useState<any>(null)
  const [entitlements, setEntitlements] = useState<any>({ assignments: [], roles: [] })
  const [roles, setRoles] = useState<any[]>([])
  const [ssoStatus, setSsoStatus] = useState<any>(null)
  const [contractor, setContractor] = useState<any>({
    sponsor_user_id: '', company: '', contract_start: '', contract_end: '',
    allowed_repo_ids: [] as string[], allowed_model_classes: [] as string[], network_zone: '',
  })
  const [enrollmentCode, setEnrollmentCode] = useState<any>(null)
  const [reasonText, setReasonText] = useState('')
  const [pendingAction, setPendingAction] = useState<UserLifecycleAction | null>(null)
  const [lifecycleBusy, setLifecycleBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  const load = async () => {
    if (!id) return
    setLoading(true)
    await Promise.all([
      api.getUser(id).then(setUser).catch(() => setUser(null)),
      api.listSessions().then((d: any[]) => setSessions((Array.isArray(d) ? d : []).filter((s: any) => s.user_id === id))).catch(() => {}),
      api.getUserHarnesses(id).then(d => setHarnesses(Array.isArray(d) ? d : [])).catch(() => {}),
      api.listHarnesses().then(d => setAllHarnesses(Array.isArray(d) ? d : [])).catch(() => {}),
      api.getUserAudit(id).then(d => setAuditEvents(Array.isArray(d) ? d : [])).catch(() => {}),
      api.getUserUsage(id).then(setUsage).catch(() => setUsage(null)),
      api.getUserEntitlements(id).then(setEntitlements).catch(() => {}),
      api.listRoles().then(d => setRoles(Array.isArray(d) ? d : [])).catch(() => {}),
      api.getUserSSOStatus(id).then(setSsoStatus).catch(() => setSsoStatus(null)),
    ])
    if (user?.contractor_info) {
      try { setContractor({ ...contractor, ...JSON.parse(user.contractor_info) }) } catch { /* legacy blob */ }
    }
    setLoading(false)
  }
  useEffect(() => { load() }, [id])

  if (loading && !user) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!user) return <div className="text-gray-400 p-8 text-center">사용자를 찾을 수 없습니다</div>

  // Lifecycle moves run through the dedicated endpoints with a captured
  // reason (PAT-1489). A 409 means the page state is stale — reload so the
  // header re-derives valid actions from the persisted state. Suspend and
  // resume only invalidate user + audit (targeted reload); offboard changes
  // sessions and harnesses too (full reload).
  const refreshCore = async () => {
    const [u, audit] = await Promise.all([
      api.getUser(id!).catch(() => null),
      api.getUserAudit(id!).catch(() => [] as any[]),
    ])
    if (u) setUser(u)
    setAuditEvents(Array.isArray(audit) ? audit : [])
  }

  const runLifecycle = async () => {
    if (!id || !pendingAction) return
    if (!reasonText.trim()) {
      showToast('사유를 입력해주세요 (감사 로그에 기록됩니다)', 'error')
      return
    }
    setLifecycleBusy(true)
    try {
      const res = await applyUserLifecycle(pendingAction, id, reasonText)
      if (pendingAction === 'offboard') {
        showToast(`퇴사 완료 — 세션 ${res.closed_sessions} 종료, 하네스 ${res.revoked_harnesses} 해제`, 'success')
      } else {
        showToast(pendingAction === 'suspend' ? '정지 완료 — 상태가 반영되었습니다' : '재활성화 완료 — 상태가 반영되었습니다', 'success')
      }
      setPendingAction(null)
      setReasonText('')
      if (pendingAction === 'offboard') {
        await load()
      } else {
        await refreshCore()
      }
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
      await refreshCore() // a 409 means stale page state — re-derive from persisted state
    } finally {
      setLifecycleBusy(false)
    }
  }

  const assignRole = async (roleId: string, scope: string, scopeId: string) => {
    if (!id) return
    try {
      const current = entitlements.assignments || []
      const next = [
        ...current.filter((a: any) => !(a.role_id === roleId && a.scope === scope && a.scope_id === scopeId)),
        { role_id: roleId, scope, scope_id: scopeId },
      ].map(a => ({ role_id: a.role_id, scope: a.scope, scope_id: a.scope_id || '' }))
      await api.putUserEntitlements(id, next)
      const fresh = await api.getUserEntitlements(id)
      setEntitlements(fresh)
      showToast('권한 저장 완료', 'success')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }
  const revokeRole = async (roleId: string, scope: string, scopeId: string) => {
    if (!id) return
    try {
      const next = (entitlements.assignments || [])
        .filter((a: any) => !(a.role_id === roleId && a.scope === scope && a.scope_id === scopeId))
        .map((a: any) => ({ role_id: a.role_id, scope: a.scope, scope_id: a.scope_id || '' }))
      await api.putUserEntitlements(id, next)
      const fresh = await api.getUserEntitlements(id)
      setEntitlements(fresh)
      showToast('권한 해제 완료', 'success')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const saveContractor = async () => {
    if (!id) return
    try {
      const updated = await api.putContractor(id, contractor)
      setUser(updated)
      showToast('계약 정보 저장 완료', 'success')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const issueEnrollment = async () => {
    if (!id) return
    try {
      const res = await api.issueEnrollmentCode(id)
      setEnrollmentCode(typeof res === 'string' ? { code: res } : res)
    } catch (e: any) { showToast(e?.message || '발급 실패', 'error') }
  }

  const grantHarness = async (harnessId: string) => {
    if (!id || !harnessId) return
    try {
      await api.grantUserHarness(id, harnessId)
      const d = await api.getUserHarnesses(id)
      setHarnesses(Array.isArray(d) ? d : [])
      showToast('하네스 바인딩 완료', 'success')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }
  const revokeHarness = async (harnessId: string) => {
    if (!id) return
    try {
      await api.revokeUserHarness(id, harnessId)
      const d = await api.getUserHarnesses(id)
      setHarnesses(Array.isArray(d) ? d : [])
      showToast('하네스 바인딩 해제', 'success')
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  return (
    <div className="p-6 space-y-4 page-enter">
      <Link to="/users" className="btn-link">← 사용자 목록</Link>

      <div className="card p-5">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="w-12 h-12 rounded-full bg-blue-100 text-blue-700 flex items-center justify-center font-bold">
              {(user.name_ko || user.name || '?').slice(0, 2).toUpperCase()}
            </div>
            <div>
              <h1 className="text-lg font-bold flex items-center gap-2">
                {user.name_ko || user.name} <span className="text-sm font-normal text-gray-400">({user.name})</span>
                <FavoriteStar entity="users" id={user.id} />
              </h1>
              <p className="text-xs text-gray-400">{user.email} · {user.title || user.title_ko || '직함 없음'} · 사번 {user.employee_id || '—'}</p>
              <div className="flex gap-2 mt-1 items-center">
                <span className={`text-[10px] px-2 py-0.5 rounded-full border ${STATUS_BADGE[user.status] || STATUS_BADGE.offboarded}`}>{STATUS_KO[user.status] || user.status}</span>
                {ssoStatus && (
                  <span className="text-[10px] text-gray-500">
                    {ssoStatus.connected ? 'SSO 연결됨' : 'SSO 미연결'} · {ssoStatus.last_login_at ? `최근 로그인 ${ssoStatus.last_login_at.slice(0, 10)}` : '로그인 기록 없음'}
                  </span>
                )}
              </div>
            </div>
          </div>
          <div className="flex gap-2 shrink-0 flex-wrap">
            {canIssueEnrollment(user.status) && (
              <button className="btn-sm btn-secondary" onClick={issueEnrollment}>초대 코드 발급</button>
            )}
            {userActions(user.status).map(a => (
              <button key={a.action} disabled={lifecycleBusy}
                className={a.action === 'offboard' ? 'btn-sm btn-danger' : 'btn-sm btn-secondary'}
                onClick={() => { setPendingAction(a.action); setReasonText('') }}>{a.label}</button>
            ))}
          </div>
        </div>
        {enrollmentCode && (
          <div className="mt-3 p-3 bg-gray-50 rounded-lg">
            <div className="text-[10px] text-gray-500">1회용 등록 코드</div>
            <div className="font-mono text-base tracking-widest">{enrollmentCode.code}</div>
            {enrollmentCode.expires_at && <div className="text-[10px] text-gray-400">만료: {enrollmentCode.expires_at}</div>}
          </div>
        )}
        {pendingAction && (
          <div className={`mt-3 p-3 rounded-lg space-y-2 ${userActionSpec(pendingAction).danger ? 'bg-red-50' : 'bg-green-50'}`}>
            <p className={`text-[11px] ${userActionSpec(pendingAction).danger ? 'text-red-600' : 'text-green-700'}`}>
              {userActionSpec(pendingAction).effect} 대상: {user.name_ko || user.name} ({user.email}). 사유를 남겨주세요.
            </p>
            <textarea className="input text-xs w-full" rows={2} placeholder="사유 (감사 로그에 기록됩니다)"
              value={reasonText} onChange={e => setReasonText(e.target.value)} />
            <div className="flex items-center gap-2">
              <button className={userActionSpec(pendingAction).danger ? 'btn-sm btn-danger' : 'btn-sm btn-primary'} disabled={lifecycleBusy} onClick={runLifecycle}>
                {lifecycleBusy ? '처리 중...' : `${userActionSpec(pendingAction).label} 확정`}
              </button>
              <button className="btn-sm btn-secondary" onClick={() => setPendingAction(null)}>취소</button>
            </div>
          </div>
        )}
      </div>

      <div className="flex gap-1 border-b border-gray-200 overflow-x-auto">
        {TABS.map(t => (
          <button key={t.id} onClick={() => setTab(t.id)}
            className={`px-3 py-2 text-xs whitespace-nowrap ${tab === t.id ? 'border-b-2 border-blue-600 text-blue-600 font-semibold' : 'text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.en}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <div className="card p-4">
            <h3 className="text-xs font-bold mb-2">기본 정보</h3>
            <dl className="text-[11px] space-y-1">
              <div className="flex justify-between"><dt className="text-gray-400">인증 방식</dt><dd>{user.auth_method}</dd></div>
              <div className="flex justify-between"><dt className="text-gray-400">외부 ID</dt><dd>{user.external_id || '—'}</dd></div>
              <div className="flex justify-between"><dt className="text-gray-400">MFA</dt><dd>{user.mfa_enrolled ? '등록됨' : '미등록'}</dd></div>
              <div className="flex justify-between"><dt className="text-gray-400">로케일</dt><dd>{user.locale}</dd></div>
              <div className="flex justify-between"><dt className="text-gray-400">시간대</dt><dd>{user.timezone}</dd></div>
              <div className="flex justify-between"><dt className="text-gray-400">생성일</dt><dd>{(user.created_at || '').slice(0, 10)}</dd></div>
              <div className="flex justify-between"><dt className="text-gray-400">계약직</dt><dd>{user.contractor_info ? '예' : '아니오'}</dd></div>
            </dl>
          </div>
          <div className="card p-4">
            <h3 className="text-xs font-bold mb-2">요약</h3>
            <div className="text-[11px] space-y-1">
              <div className="flex justify-between"><span className="text-gray-400">세션</span><span>{sessions.length}</span></div>
              <div className="flex justify-between"><span className="text-gray-400">하네스</span><span>{harnesses.length}</span></div>
              <div className="flex justify-between"><span className="text-gray-400">감사 이벤트</span><span>{auditEvents.length}</span></div>
              {usage && <button type="button" onClick={() => setTab('usage')} className="flex w-full justify-between text-left hover:text-blue-600"><span className="text-gray-400">최근 30일 비용</span><span>{usage.display_total?.state === 'unavailable' ? '미수집' : formatUsageAmount(usage.display_total?.amount_micros, usage.display_total?.currency)}</span></button>}
            </div>
          </div>
        </div>
      )}

      {tab === 'entitlements' && (
        <div className="card p-4 space-y-3">
          <h3 className="text-xs font-bold">사용자 권한 (Entitlement)</h3>
          <p className="text-[10px] text-gray-400">하네스를 통한 사용자 권한 범위입니다 (콘솔 운영자 권한과 별개).</p>
          <div className="space-y-2">
            {roles.map(r => {
              const assigned = (entitlements.assignments || []).filter((a: any) => a.role_id === r.id)
              return (
                <div key={r.id} className="flex items-center justify-between border rounded-lg p-2">
                  <div>
                    <div className="text-xs font-semibold">{r.name_ko || r.name}</div>
                    <div className="text-[10px] text-gray-400">{(() => { try { return JSON.parse(r.permissions).join(', ') } catch { return '' } })()}</div>
                  </div>
                  <div className="flex gap-1">
                    {assigned.map((a: any) => (
                      <span key={a.id} className="text-[10px] px-2 py-0.5 rounded bg-green-50 text-green-700 border border-green-200 flex items-center gap-1">
                        {a.scope || 'org'}
                        <button onClick={() => revokeRole(r.id, a.scope, a.scope_id || '')}>✕</button>
                      </span>
                    ))}
                    {assigned.length === 0 && (
                      <button className="text-[10px] px-2 py-1 rounded bg-gray-100 hover:bg-blue-50 text-blue-600"
                        onClick={() => assignRole(r.id, 'org', '')}>부여</button>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {tab === 'sessions' && (
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">세션 ({sessions.length})</h3>
          {sessions.length === 0 ? <p className="text-[11px] text-gray-400">세션 없음</p> : (
            <div className="space-y-1">
              {sessions.map((s: any) => (
                <Link key={s.id} to={`/sessions`} className="flex justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1">
                  <span className="text-gray-700">{s.title || s.session_id}</span>
                  <span className="text-gray-400">{s.status} · {s.model_class || '—'}</span>
                </Link>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'harnesses' && (
        <div className="card p-4 space-y-3">
          <h3 className="text-xs font-bold">하네스 바인딩 ({harnesses.length})</h3>
          <div className="space-y-1">
            {harnesses.map((h: any) => (
              <div key={h.id} className="flex justify-between items-center text-[11px] border-b border-gray-50 py-1">
                <span className="text-gray-700">{h.name} <span className="text-gray-400">({h.harness_id})</span></span>
                <button className="text-[10px] px-2 py-1 rounded text-red-600 hover:bg-red-50" onClick={() => revokeHarness(h.id)}>해제</button>
              </div>
            ))}
            {harnesses.length === 0 && <p className="text-[11px] text-gray-400">바인딩된 하네스 없음</p>}
          </div>
          <div className="flex gap-2 items-center shrink-0">
            <select className="input text-xs" defaultValue=""
              onChange={e => { if (e.target.value) grantHarness(e.target.value) }}>
              <option value="">하네스 바인딩 추가...</option>
              {allHarnesses.filter((h: any) => !harnesses.some((b: any) => b.id === h.id)).map((h: any) => (
                <option key={h.id} value={h.id}>{h.name} ({h.harness_id})</option>
              ))}
            </select>
          </div>
        </div>
      )}

      {tab === 'usage' && (
        <UsageReport report={usage} title={`${user.name_ko || user.name} 사용량 및 비용 원장`} />
      )}

      {tab === 'audit' && (
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">감사 이벤트 ({auditEvents.length})</h3>
          {auditEvents.length === 0 ? <p className="text-[11px] text-gray-400">감사 이벤트 없음</p> : (
            <div className="space-y-1">
              {auditEvents.map((e: any) => (
                <div key={e.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
                  <span className="text-gray-700">{e.action}</span>
                  <span className="text-gray-400">{(e.occurred_at || '').slice(0, 16)} · {e.result}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'contractor' && (
        <div className="card p-4 space-y-3">
          <h3 className="text-xs font-bold">계약직 프로필 (Contractor)</h3>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">스폰서 (사번/이메일)</label>
              <EntitySelect entity="user" value={contractor.sponsor_user_id} onChange={v => setContractor({ ...contractor, sponsor_user_id: v })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">회사</label>
              <input className="input text-xs w-full" value={contractor.company} onChange={e => setContractor({ ...contractor, company: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">계약 시작</label>
              <input className="input text-xs w-full" type="date" value={contractor.contract_start} onChange={e => setContractor({ ...contractor, contract_start: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">계약 종료 (만료 시 자동 정지)</label>
              <input className="input text-xs w-full" type="date" value={contractor.contract_end} onChange={e => setContractor({ ...contractor, contract_end: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">허용 저장소 (쉼표 구분)</label>
              <input className="input text-xs w-full" value={contractor.allowed_repo_ids.join(',')}
                onChange={e => setContractor({ ...contractor, allowed_repo_ids: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">허용 모델 클래스 (쉼표 구분)</label>
              <input className="input text-xs w-full" value={contractor.allowed_model_classes.join(',')}
                onChange={e => setContractor({ ...contractor, allowed_model_classes: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">네트워크 존</label>
              <select className="input text-xs w-full" value={contractor.network_zone} onChange={e => setContractor({ ...contractor, network_zone: e.target.value })}>
                <option value="">—</option>
                <option value="internal">내부망</option>
                <option value="dmz">DMZ</option>
                <option value="external">외부망</option>
              </select>
            </div>
          </div>
          <button className="btn-sm btn-primary" onClick={saveContractor}>계약 정보 저장</button>
        </div>
      )}
    </div>
  )
}
