import { useState, useEffect, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { EntitySelect } from '../components/EntitySelect'
import { FavoriteStar } from '../hooks/useFavorites'
import { showToast } from '../components/Toast'
import { formatUsageAmount, UsageReport } from '../components/UsageReport'
import { useAuth } from '../hooks/useAuth'
import { userActions, userActionSpec, applyUserLifecycle, canIssueEnrollment, lifecycleDenialLabel, STATUS_KO, STATUS_BADGE, UserLifecycleAction } from '../userLifecycle'
import { detailRoute } from '../relationLinks'
import { sessionLifecycleLabel } from '../glossary'
import { useTabParam } from '../hooks/useTabParam'

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

const emptyContractor = () => ({
  sponsor_user_id: '', company: '', contract_start: '', contract_end: '',
  allowed_repo_ids: [] as string[], allowed_model_classes: [] as string[], network_zone: '',
})

export default function UserDetail() {
  const { id } = useParams<{ id: string }>()
  const { email: operatorEmail } = useAuth()
  const [tab, setTab] = useTabParam('overview', ['overview', 'entitlements', 'sessions', 'harnesses', 'usage', 'audit', 'contractor']) as [string, (t: string) => void]

  const [user, setUser] = useState<any>(null)
  const [sessions, setSessions] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [allHarnesses, setAllHarnesses] = useState<any[]>([])
  const [auditEvents, setAuditEvents] = useState<any[]>([])
  const [usage, setUsage] = useState<any>(null)
  const [entitlements, setEntitlements] = useState<any>({ assignments: [], roles: [] })
  const [roles, setRoles] = useState<any[]>([])
  const [ssoStatus, setSsoStatus] = useState<any>(null)
  const [contractor, setContractor] = useState<any>(emptyContractor())
  const [allUsers, setAllUsers] = useState<any[]>([])
  const [contractorConfirmOpen, setContractorConfirmOpen] = useState(false)
  const [contractorReason, setContractorReason] = useState('')
  const [enrollmentCode, setEnrollmentCode] = useState<any>(null)
  const [usageLoading, setUsageLoading] = useState(true)
  const [usageError, setUsageError] = useState(false)
  const [reasonText, setReasonText] = useState('')
  const [pendingAction, setPendingAction] = useState<UserLifecycleAction | null>(null)
  const [lifecycleBusy, setLifecycleBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const detailedUsageID = useRef('')
  const summaryUsageID = useRef('')
	const loadGeneration = useRef(0)
	const routeID = useRef(id)
	routeID.current = id

	const load = async (requestedID = id) => {
		if (!requestedID || requestedID !== routeID.current) return
		const targetID = requestedID
		const generation = ++loadGeneration.current
    setLoading(true)
		const [loadedUser, loadedSessions, loadedHarnesses, loadedAllHarnesses, loadedAudit, loadedEntitlements, loadedRoles, loadedSSO, loadedAllUsers] = await Promise.all([
			api.getUser(targetID).catch(() => null),
			api.listSessions().then((d: any[]) => (Array.isArray(d) ? d : []).filter((s: any) => s.user_id === targetID)).catch(() => []),
			api.getUserHarnesses(targetID).then(d => Array.isArray(d) ? d : []).catch(() => []),
			api.listHarnesses().then(d => Array.isArray(d) ? d : []).catch(() => []),
			api.getUserAudit(targetID).then(d => Array.isArray(d) ? d : []).catch(() => []),
			api.getUserEntitlements(targetID).catch(() => ({ assignments: [], roles: [] })),
			api.listRoles().then(d => Array.isArray(d) ? d : []).catch(() => []),
			api.getUserSSOStatus(targetID).catch(() => null),
			api.listUsers().then(d => Array.isArray(d) ? d : []).catch(() => []),
		])
		if (generation !== loadGeneration.current || targetID !== routeID.current) return
		setUser(loadedUser)
		setSessions(loadedSessions)
		setHarnesses(loadedHarnesses)
		setAllHarnesses(loadedAllHarnesses)
		setAuditEvents(loadedAudit)
		setEntitlements(loadedEntitlements)
		setRoles(loadedRoles)
		setSsoStatus(loadedSSO)
		setAllUsers(Array.isArray(loadedAllUsers) ? loadedAllUsers : [])
    const nextContractor = emptyContractor()
    if (loadedUser?.contractor_info) {
      try { Object.assign(nextContractor, JSON.parse(loadedUser.contractor_info)) } catch { /* legacy blob */ }
    }
    setContractor(nextContractor)
    setLoading(false)
  }
  useEffect(() => {
		setUser(null)
		setPendingAction(null)
		setEnrollmentCode(null)
    load()
		return () => { loadGeneration.current++ }
  }, [id])

  useEffect(() => {
    if (!id) return
    detailedUsageID.current = ''
    summaryUsageID.current = ''
    setUsage(null)
    setUsageLoading(true)
    setUsageError(false)
  }, [id])

  useEffect(() => {
    if (!id) return
    const detailed = tab === 'usage'
    if (detailed ? detailedUsageID.current === id : summaryUsageID.current === id) return
    const controller = new AbortController()
    setUsageLoading(true)
    api.getUserUsage(id, '30d', '', controller.signal, !detailed).then(d => {
      setUsage(d)
      setUsageError(false)
      if (detailed) {
        detailedUsageID.current = id
        summaryUsageID.current = id
      } else {
        summaryUsageID.current = id
      }
    }).catch((error: any) => {
      if (error?.name !== 'AbortError') { setUsage(null); setUsageError(true) }
    }).finally(() => { if (!controller.signal.aborted) setUsageLoading(false) })
    return () => controller.abort()
  }, [id, tab])

  if (loading && !user) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!user) return <div className="text-gray-400 p-8 text-center">사용자를 찾을 수 없습니다</div>
  const mutable = user.can_manage === true && user.status !== 'offboarded'

  // Lifecycle moves run through the dedicated endpoints with a captured
  // reason (PAT-1489). A 409 means the page state is stale — reload so the
  // header re-derives valid actions from the persisted state. Suspend and
  // resume only invalidate user + audit (targeted reload); offboard changes
  // sessions and harnesses too (full reload).
	const refreshCore = async () => {
		const targetID = id!
		const generation = ++loadGeneration.current
    const [u, audit] = await Promise.all([
			api.getUser(targetID).catch(() => null),
			api.getUserAudit(targetID).catch(() => [] as any[]),
    ])
		if (generation !== loadGeneration.current || targetID !== routeID.current) return
    if (u) setUser(u)
    setAuditEvents(Array.isArray(audit) ? audit : [])
  }

  const runLifecycle = async () => {
    if (!id || !pendingAction || lifecycleBusy) return
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
        showToast(pendingAction === 'suspend'
          ? `정지 완료 — 세션 ${res.closed_sessions}개 종료, 권한 임대 ${res.revoked_leases}개 회수`
          : '재활성화 완료 — 상태가 반영되었습니다', 'success')
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
    if (!id || lifecycleBusy) return
		const targetID = id
		setLifecycleBusy(true)
    try {
      const current = entitlements.assignments || []
      const next = [
        ...current.filter((a: any) => !(a.role_id === roleId && a.scope === scope && a.scope_id === scopeId)),
        { role_id: roleId, scope, scope_id: scopeId },
      ].map(a => ({ role_id: a.role_id, scope: a.scope, scope_id: a.scope_id || '' }))
			await api.putUserEntitlements(targetID, next)
			const fresh = await api.getUserEntitlements(targetID)
			if (routeID.current === targetID) setEntitlements(fresh)
      showToast('권한 저장 완료', 'success')
		} catch (e: any) { showToast(e?.message || '실패', 'error') }
		finally { setLifecycleBusy(false) }
  }
  const revokeRole = async (roleId: string, scope: string, scopeId: string) => {
    if (!id || lifecycleBusy) return
		const targetID = id
		setLifecycleBusy(true)
    try {
      const next = (entitlements.assignments || [])
        .filter((a: any) => !(a.role_id === roleId && a.scope === scope && a.scope_id === scopeId))
        .map((a: any) => ({ role_id: a.role_id, scope: a.scope, scope_id: a.scope_id || '' }))
			await api.putUserEntitlements(targetID, next)
			const fresh = await api.getUserEntitlements(targetID)
			if (routeID.current === targetID) setEntitlements(fresh)
      showToast('권한 해제 완료', 'success')
		} catch (e: any) { showToast(e?.message || '실패', 'error') }
		finally { setLifecycleBusy(false) }
  }

  const saveContractor = async (reason?: string) => {
    if (!id || lifecycleBusy) return
		const targetID = id
		setLifecycleBusy(true)
    try {
			const payload: any = { ...contractor }
			if (reason) payload.transition_reason = reason
			const updated = await api.putContractor(targetID, payload)
			if (routeID.current === targetID) setUser(updated)
      showToast(reason ? '계약직 전환이 기록되었습니다' : '계약 정보 저장 완료', 'success')
		} catch (e: any) { showToast(e?.message || '실패', 'error') }
		finally { setLifecycleBusy(false) }
  }

  const issueEnrollment = async () => {
    if (!id || lifecycleBusy) return
		const targetID = id
		setLifecycleBusy(true)
    try {
			const res = await api.issueEnrollmentCode(targetID)
			if (routeID.current === targetID) setEnrollmentCode(typeof res === 'string' ? { code: res } : res)
		} catch (e: any) { showToast(e?.message || '발급 실패', 'error') }
		finally { setLifecycleBusy(false) }
  }

  const grantHarness = async (harnessId: string) => {
    if (!id || !harnessId || lifecycleBusy) return
		const targetID = id
		setLifecycleBusy(true)
    try {
			await api.grantUserHarness(targetID, harnessId)
			const d = await api.getUserHarnesses(targetID)
			if (routeID.current === targetID) setHarnesses(Array.isArray(d) ? d : [])
      showToast('하네스 바인딩 완료', 'success')
		} catch (e: any) { showToast(e?.message || '실패', 'error') }
		finally { setLifecycleBusy(false) }
  }
  const revokeHarness = async (harnessId: string) => {
    if (!id || lifecycleBusy) return
		const targetID = id
		setLifecycleBusy(true)
    try {
			await api.revokeUserHarness(targetID, harnessId)
			const d = await api.getUserHarnesses(targetID)
			if (routeID.current === targetID) setHarnesses(Array.isArray(d) ? d : [])
      showToast('하네스 바인딩 해제', 'success')
		} catch (e: any) { showToast(e?.message || '실패', 'error') }
		finally { setLifecycleBusy(false) }
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
            {canIssueEnrollment(user.status, user.can_manage) && (
              <button className="btn-sm btn-secondary" disabled={lifecycleBusy} onClick={issueEnrollment}>초대 코드 발급</button>
            )}
            {userActions(user.allowed_actions).map(a => (
              <button key={a.action} disabled={lifecycleBusy}
                className={a.action === 'offboard' ? 'btn-sm btn-danger' : 'btn-sm btn-secondary'}
                onClick={() => { setPendingAction(a.action); setReasonText('') }}>{a.label}</button>
            ))}
          </div>
        </div>
        {lifecycleDenialLabel(user.lifecycle_denial_reason) && (
          <p className="mt-2 text-[11px] text-amber-700">{lifecycleDenialLabel(user.lifecycle_denial_reason)}</p>
        )}
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
            <p className="text-[11px] text-gray-500">실행자: <span className="font-medium text-gray-700">{operatorEmail || '현재 콘솔 운영자'}</span></p>
            <textarea className="input text-xs w-full" rows={2} placeholder="사유 (감사 로그에 기록됩니다)"
              disabled={lifecycleBusy} value={reasonText} onChange={e => setReasonText(e.target.value)} />
            <div className="flex items-center gap-2">
              <button className={userActionSpec(pendingAction).danger ? 'btn-sm btn-danger' : 'btn-sm btn-primary'} disabled={lifecycleBusy} onClick={runLifecycle}>
                {lifecycleBusy ? '처리 중...' : `${userActionSpec(pendingAction).label} 확정`}
              </button>
              <button className="btn-sm btn-secondary" disabled={lifecycleBusy} onClick={() => setPendingAction(null)}>취소</button>
            </div>
          </div>
        )}
      </div>

      {user.status === 'offboarded' && (
        <div className="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-[11px] text-gray-600">
          퇴사 처리된 사용자의 기록입니다. 감사, 세션, 사용량 및 과거 바인딩은 조회할 수 있지만 권한·하네스·계약 정보는 변경할 수 없습니다.
        </div>
      )}

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
              {usage && <button type="button" onClick={() => setTab('usage')} className="flex w-full justify-between text-left hover:text-blue-600"><span className="text-gray-400">최근 30일 비용</span><span>{usage.display_total?.state === 'recorded' || usage.display_total?.state === 'zero' ? formatUsageAmount(usage.display_total?.amount_micros, usage.display_total?.currency) : usage.display_total?.state === 'error' ? '집계 오류' : '미수집'}</span></button>}
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
                        {mutable && <button disabled={lifecycleBusy} onClick={() => revokeRole(r.id, a.scope, a.scope_id || '')}>✕</button>}
                      </span>
                    ))}
                    {mutable && assigned.length === 0 && (
                      <button className="text-[10px] px-2 py-1 rounded bg-gray-100 hover:bg-blue-50 text-blue-600"
                        disabled={lifecycleBusy}
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
                <Link key={s.id} to={detailRoute('session', s.session_id || s.id)} state={{ from: `/users/${id}?tab=sessions` }} className="flex justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1">
                  <span className="text-gray-700">{s.title || s.session_id}</span>
                  <span className="text-gray-400">{sessionLifecycleLabel(s.status)} · {s.model_class || '—'}</span>
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
                {mutable && <button className="text-[10px] px-2 py-1 rounded text-red-600 hover:bg-red-50" disabled={lifecycleBusy} onClick={() => revokeHarness(h.id)}>해제</button>}
              </div>
            ))}
            {harnesses.length === 0 && <p className="text-[11px] text-gray-400">바인딩된 하네스 없음</p>}
          </div>
          {mutable && <div className="flex gap-2 items-center shrink-0">
            <select className="input text-xs" defaultValue="" disabled={lifecycleBusy}
              onChange={e => { if (e.target.value) grantHarness(e.target.value) }}>
              <option value="">하네스 바인딩 추가...</option>
              {allHarnesses.filter((h: any) => !harnesses.some((b: any) => b.id === h.id)).map((h: any) => (
                <option key={h.id} value={h.id}>{h.name} ({h.harness_id})</option>
              ))}
            </select>
          </div>}
        </div>
      )}

      {tab === 'usage' && (
        usageLoading ? <div className="card p-4 text-[11px] text-gray-400">사용량 원장을 불러오는 중입니다.</div> : usageError ? <div className="card p-4 text-[11px] text-red-600">사용량 원장을 조회할 권한이 없거나 조회 중 오류가 발생했습니다.</div> : <UsageReport report={usage} title={`${user.name_ko || user.name} 사용량 및 비용 원장`} loadMore={(cursor, signal) => api.getUserUsage(id!, '30d', cursor, signal)} />
      )}

      {tab === 'audit' && (
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">감사 이벤트 ({auditEvents.length})</h3>
          {auditEvents.length === 0 ? <p className="text-[11px] text-gray-400">감사 이벤트 없음</p> : (
            <div className="space-y-1">
              {auditEvents.map((e: any) => {
                let details: any = {}
                try { details = JSON.parse(e.details || '{}') } catch { details = {} }
                return (
                  <div key={e.id} className="border-b border-gray-50 py-2 text-[11px]">
                    <div className="flex justify-between gap-3">
                      <span className="font-medium text-gray-700">{e.action}</span>
                      <span className="text-gray-400">{(e.occurred_at || '').slice(0, 16)} · {e.result}</span>
                    </div>
                    <div className="mt-0.5 text-[10px] text-gray-500">
                      실행자 {e.actor_id || e.actor_type || '—'}
                      {details.from && details.to ? ` · ${STATUS_KO[details.from] || details.from} → ${STATUS_KO[details.to] || details.to}` : ''}
                      {details.reason ? ` · 사유: ${details.reason}` : ''}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {tab === 'contractor' && (() => {
        const isContractorUser = !!user.contractor_info
        const eligibleSponsors = allUsers.filter((u: any) => u.id !== id && u.status === 'active' && (!user.organization_id || u.organization_id === user.organization_id))
        const sponsorIsSelf = contractor.sponsor_user_id === id
        const sponsorExists = !contractor.sponsor_user_id || eligibleSponsors.some((u: any) => u.id === contractor.sponsor_user_id) || contractor.sponsor_user_id === id
        const today = new Date().toISOString().slice(0, 10)
        const isExpired = !!(contractor.contract_end && contractor.contract_end < today)
        const isExpiringSoon = !!(contractor.contract_end && !isExpired && contractor.contract_end <= new Date(Date.now() + 14 * 86400000).toISOString().slice(0, 10))
        const sponsorUser = allUsers.find((u: any) => u.id === contractor.sponsor_user_id)
        const sponsorInvalid = !!(contractor.sponsor_user_id && (!sponsorUser || sponsorUser.status !== 'active' || sponsorIsSelf))

        const handleSave = () => {
          if (sponsorIsSelf) { showToast('자기 자신을 스폰서로 지정할 수 없습니다', 'error'); return }
          if (sponsorInvalid) { showToast('유효하지 않은 스폰서입니다', 'error'); return }
          if (!isContractorUser) {
            setContractorConfirmOpen(true)
            return
          }
          saveContractor()
        }

        const confirmTransition = async () => {
          if (!contractorReason.trim()) { showToast('전환 사유를 입력해주세요', 'error'); return }
          const reason = contractorReason.trim()
          setContractorConfirmOpen(false)
          await saveContractor(reason)
          setContractorReason('')
        }

        return (
        <div className="card p-4 space-y-3">
          <h3 className="text-xs font-bold">계약직 프로필 (Contractor)</h3>
          {!isContractorUser ? (
            <div className="space-y-3">
              <div className="p-3 bg-gray-50 rounded-lg border border-gray-200">
                <p className="text-xs font-medium text-gray-700">계약직이 아닙니다</p>
                <p className="text-[11px] text-gray-500 mt-1">이 사용자는 현재 정규직으로 분류되어 계약직 전용 접근 제어가 비활성 상태입니다. 계약직으로 전환하면 스폰서, 허용 저장소/모델, 네트워크 존 등 계약직 전용 정책이 적용됩니다.</p>
              </div>
              {contractorConfirmOpen ? (
                <div className="p-3 bg-amber-50 rounded-lg border border-amber-200 space-y-2">
                  <p className="text-xs font-semibold text-amber-800">계약직으로 전환 — 영향도 미리보기</p>
                  <ul className="text-[11px] text-amber-700 list-disc ml-4 space-y-0.5">
                    <li>활성 세션 {sessions.length}개와 하네스 {harnesses.length}개의 접근 범위가 계약 종료일에 따라 재계산됩니다</li>
                    <li>스폰서 {contractor.sponsor_user_id ? (sponsorUser ? `${sponsorUser.name_ko || sponsorUser.name} (${sponsorUser.email})` : contractor.sponsor_user_id) : '미지정'}에게 알림이 기록됩니다</li>
                    <li>계약 종료일 {contractor.contract_end || '미지정'} 이후 자동 정지 정책이 적용됩니다</li>
                  </ul>
                  <div>
                    <label className="text-[10px] text-gray-500">전환 사유 (감사 로그에 기록)</label>
                    <textarea className="input text-xs w-full mt-1" rows={2} placeholder="사유를 입력해주세요" value={contractorReason} onChange={e => setContractorReason(e.target.value)} />
                  </div>
                  <div className="flex gap-2">
                    <button className="btn-sm btn-primary" disabled={lifecycleBusy} onClick={confirmTransition}>전환 확정</button>
                    <button className="btn-sm btn-secondary" disabled={lifecycleBusy} onClick={() => setContractorConfirmOpen(false)}>취소</button>
                  </div>
                </div>
              ) : (
                <div className="space-y-2">
                  <p className="text-[11px] text-gray-500">계약직 전용 필드는 전환 후에만 저장됩니다. 먼저 아래 계약 정보를 입력하고 전환을 진행하세요.</p>
                  <fieldset disabled={!mutable || lifecycleBusy} className="grid grid-cols-2 gap-2">
                    <div>
                      <label className="text-[10px] text-gray-500">스폰서 (사번/이메일) *</label>
                      <select className="input text-xs w-full" value={contractor.sponsor_user_id} onChange={e => setContractor({ ...contractor, sponsor_user_id: e.target.value })}>
                        <option value="">선택...</option>
                        {eligibleSponsors.map((u: any) => (
                          <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>
                        ))}
                      </select>
                      {sponsorIsSelf && <p className="text-[10px] text-red-600 mt-1">자기 자신을 스폰서로 지정할 수 없습니다</p>}
                      {!sponsorExists && contractor.sponsor_user_id && !sponsorIsSelf && <p className="text-[10px] text-red-600 mt-1">선택한 스폰서를 찾을 수 없거나 비활성 상태입니다</p>}
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
                      <input className="input text-xs w-full" value={contractor.allowed_repo_ids.join(',')} onChange={e => setContractor({ ...contractor, allowed_repo_ids: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} />
                    </div>
                    <div>
                      <label className="text-[10px] text-gray-500">허용 모델 클래스 (쉼표 구분)</label>
                      <input className="input text-xs w-full" value={contractor.allowed_model_classes.join(',')} onChange={e => setContractor({ ...contractor, allowed_model_classes: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} />
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
                  </fieldset>
                  {mutable && <button className="btn-sm btn-primary" disabled={lifecycleBusy || sponsorIsSelf} onClick={handleSave}>계약직으로 전환</button>}
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-3">
              {(isExpired || sponsorInvalid || isExpiringSoon) && (
                <div className={`p-2 rounded text-[11px] ${isExpired || sponsorInvalid ? 'bg-red-50 text-red-700 border border-red-200' : 'bg-amber-50 text-amber-700 border border-amber-200'}`}>
                  {isExpired && <div>⚠ 계약이 만료되었습니다 ({contractor.contract_end}) — 자동 정지 대상입니다</div>}
                  {sponsorInvalid && <div>⚠ 스폰서가 유효하지 않습니다 — {sponsorIsSelf ? '자기 자신을 스폰서로 지정할 수 없습니다' : sponsorUser ? `스폰서 ${sponsorUser.name_ko || sponsorUser.name} 비활성` : '스폰서를 찾을 수 없습니다'}</div>}
                  {isExpiringSoon && !isExpired && <div>계약 만료가 임박했습니다 ({contractor.contract_end}) — 갱신 또는 종료 조치가 필요합니다</div>}
                </div>
              )}
              <fieldset disabled={!mutable || lifecycleBusy} className="grid grid-cols-2 gap-2">
                <div>
                  <label className="text-[10px] text-gray-500">스폰서 (사번/이메일) *</label>
                  <select className="input text-xs w-full" value={contractor.sponsor_user_id} onChange={e => setContractor({ ...contractor, sponsor_user_id: e.target.value })}>
                    <option value="">선택...</option>
                    {eligibleSponsors.map((u: any) => (
                      <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>
                    ))}
                    {contractor.sponsor_user_id && !eligibleSponsors.some((u: any) => u.id === contractor.sponsor_user_id) && (
                      <option value={contractor.sponsor_user_id} disabled>현재 스폰서: {contractor.sponsor_user_id} (유효하지 않음)</option>
                    )}
                  </select>
                  {sponsorIsSelf && <p className="text-[10px] text-red-600 mt-1">자기 자신을 스폰서로 지정할 수 없습니다</p>}
                  {sponsorInvalid && !sponsorIsSelf && <p className="text-[10px] text-red-600 mt-1">유효하지 않은 스폰서입니다 — 활성 상태의 다른 사용자를 선택하세요</p>}
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
                  <input className="input text-xs w-full" value={contractor.allowed_repo_ids.join(',')} onChange={e => setContractor({ ...contractor, allowed_repo_ids: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} />
                </div>
                <div>
                  <label className="text-[10px] text-gray-500">허용 모델 클래스 (쉼표 구분)</label>
                  <input className="input text-xs w-full" value={contractor.allowed_model_classes.join(',')} onChange={e => setContractor({ ...contractor, allowed_model_classes: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} />
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
              </fieldset>
              <div className="flex gap-2">
                {mutable && <button className="btn-sm btn-primary" disabled={lifecycleBusy || sponsorIsSelf || sponsorInvalid} onClick={handleSave}>계약 정보 저장</button>}
                {mutable && <button className="btn-sm btn-secondary" disabled={lifecycleBusy} onClick={async () => {
                  if (!confirm('계약직 상태를 해제하면 스폰서 및 계약 전용 정책이 제거됩니다. 계속하시겠습니까?')) return
                  const reason = prompt('해제 사유를 입력하세요 (감사 로그에 기록됩니다)') || ''
                  if (!reason.trim()) { showToast('사유를 입력해주세요', 'error'); return }
                  try {
                    setLifecycleBusy(true)
                    await api.putContractor(id!, { sponsor_user_id: '', company: '', contract_start: '', contract_end: '', allowed_repo_ids: [], allowed_model_classes: [], network_zone: '', transition_reason: reason.trim() } as any)
                    // Reload to reflect non-contractor state
                    await load()
                    showToast('계약직 해제 완료', 'success')
                  } catch (e: any) { showToast(e?.message || '실패', 'error') } finally { setLifecycleBusy(false) }
                }}>계약직 해제</button>}
              </div>
            </div>
          )}
        </div>
        )
      })()}
    </div>
  )
}
