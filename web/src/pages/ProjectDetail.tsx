import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { EntitySelect } from '../components/EntitySelect'
import { Modal, ModalFooter } from '../components/Modal'
import { formatRelative } from '../utils/format'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'
import { formatUsageAmount, UsageReport } from '../components/UsageReport'
import { AllowedModelChips, resolveAllowedModels } from '../allowedModels'

const ROLES = [
  { value: 'owner', label: '소유자 · Owner' },
  { value: 'admin', label: '관리자 · Admin' },
  { value: 'member', label: '멤버 · Member' },
  { value: 'viewer', label: '뷰어 · Viewer' },
]

// ProjectDetail (projects B3) — repos, real membership roster,
// sessions, policy binding, usage/chargeback, AI change-control queue,
// and audit in one deep-linkable route.
export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>()
  const confirm = useConfirm()
  const [detail, setDetail] = useState<any>(null)
  const [usage, setUsage] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [tab, setTab] = useState<'overview' | 'members' | 'sessions' | 'governance' | 'audit'>('overview')
  const [addMember, setAddMember] = useState(false)
  const [memberForm, setMemberForm] = useState({ user_id: '', role: 'member' })
  const [packTarget, setPackTarget] = useState(false)
  const [packs, setPacks] = useState<any[]>([])
  const [modelPackages, setModelPackages] = useState<any[]>([])
  const [packPick, setPackPick] = useState('')

  const load = () => {
    if (!id) return
    api.getProjectDetail(id).then(setDetail).catch(() => setDetail(null)).finally(() => setLoading(false))
    api.projectUsage(id).then(setUsage).catch(() => setUsage(null))
    api.listPolicyPacks().then(d => setPacks(Array.isArray(d) ? d : [])).catch(() => {})
    api.listModels().then(d => setModelPackages(Array.isArray(d) ? d : [])).catch(() => setModelPackages([]))
  }
  useEffect(() => { load() }, [id])

  if (loading) return <div className="p-8 space-y-3 animate-pulse"><div className="h-4 bg-gray-100 rounded w-1/2" /><div className="h-4 bg-gray-100 rounded w-2/3" /></div>
  if (!detail?.project) return <div className="text-gray-400 p-8 text-center">프로젝트를 찾을 수 없습니다</div>

  const proj = detail.project
  const members = detail.members || []
  const repos = detail.repositories || []
  const sessions = detail.sessions || []
  const changeRequests = detail.change_requests || []
  const auditEvents = detail.audit_events || []
  const pendingChanges = changeRequests.filter((c: any) => c.status === 'pending')
  const activeSessions = sessions.filter((s: any) => s.status === 'active')
  const hasUsageLedger = Boolean(usage?.record_count)
  const delayedUsageMeters = (usage?.meters || []).filter((meter: any) => meter.state === 'delayed').length
  const displayCost = usage?.display_total?.state === 'unavailable' ? '미수집' : usage?.display_total ? formatUsageAmount(usage.display_total.amount_micros, usage.display_total.currency) : '—'

  const submitMember = async () => {
    if (!memberForm.user_id) { showToast('사용자를 선택하세요', 'error'); return }
    try {
      await api.addProjectMember(id!, memberForm)
      showToast('멤버 추가됨', 'success')
      setAddMember(false); setMemberForm({ user_id: '', role: 'member' })
      load()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const removeMember = async (userId: string, name: string) => {
    if (!await confirm({ title: '멤버 제거', message: `${name} 님을 프로젝트에서 제거하시겠습니까?`, danger: true })) return
    try { await api.removeProjectMember(id!, userId); showToast('제거됨', 'info'); load() } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const submitPack = async () => {
    try {
      await api.bindProjectPolicyPack(id!, packPick)
      showToast('정책 팩 바인딩됨', 'success')
      setPackTarget(false)
      load()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const decideChange = async (cr: any, approve: boolean) => {
    try {
      await api.decideChangeRequest(cr.id, approve, approve ? 'approved via console' : 'denied via console')
      showToast(approve ? '승인됨' : '거부됨', approve ? 'success' : 'info')
      load()
    } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  return (
    <div>
      <Link to="/projects" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 프로젝트 목록</Link>

      <div className="card mb-6 flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold">{proj.name_ko || proj.name}</h1>
          <p className="text-xs text-gray-400 mt-1">
            {proj.name} · {proj.slug}
            {proj.project_code && <span> · 코드 {proj.project_code}</span>}
            {proj.group_affiliate && <span> · 🏢 {proj.group_affiliate}</span>}
          </p>
          {proj.description && <p className="text-sm text-gray-500 mt-2">{proj.description}</p>}
        </div>
        <div className="flex gap-2 items-center shrink-0">
          {proj.status === 'archived'
            ? <button onClick={async () => { if (await confirm({ title: '복원', message: '이 프로젝트를 복원하시겠습니까?', danger: false })) { await api.restoreProject(proj.id); showToast('복원됨', 'success'); load() } }} className="btn-sm btn-primary">복원 · Restore</button>
            : <span className="badge-green">active</span>}
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-5 gap-3 stat-grid mb-6">
        <StatCard label="저장소" value={repos.length} accent="blue" />
        <StatCard label="멤버" value={members.length} accent="green" />
        <StatCard label="활성 세션" value={activeSessions.length} accent="purple" to="/sessions" query={`?project_id=${proj.id}`} />
        <StatCard label="최근 30일 토큰" value={hasUsageLedger ? (usage.total_tokens || 0).toLocaleString() : '미수집'} accent="orange" to={`/projects/${proj.id}`} query="#project-usage-ledger" sub={`원장 ${usage?.record_count ?? '—'}건`} />
        <StatCard label={`최근 30일 비용 (${usage?.display_currency || '통화 미확인'})`} value={displayCost} accent="red" to={`/projects/${proj.id}`} query="#project-usage-ledger" sub={!hasUsageLedger ? '원장 기록 없음' : `${usage?.reconciled ? '원장 대사 완료' : '대사 확인 필요'}${delayedUsageMeters ? ` · 지연 ${delayedUsageMeters}` : ''}`} />
      </div>

      {pendingChanges.length > 0 && (
        <div className="card mb-4 border-l-4 border-l-yellow-400">
          <h3 className="text-sm font-semibold mb-1">⚠ 승인 대기 중인 고위험 변경 · Pending AI Change-Control ({pendingChanges.length})</h3>
          <p className="text-xs text-gray-400">§33.4 — 고위험 AI 변경은 거버넌스 탭에서 승인해야 반영됩니다</p>
        </div>
      )}

      <div className="flex gap-1 mb-6 border-b border-gray-200 flex-wrap">
        {[
          { id: 'overview', label: '개요', en: 'Overview' },
          { id: 'members', label: '멤버', en: 'Members', count: members.length },
          { id: 'sessions', label: '세션', en: 'Sessions', count: sessions.length },
          { id: 'governance', label: '거버넌스', en: 'Governance', count: pendingChanges.length },
          { id: 'audit', label: '감사', en: 'Audit', count: auditEvents.length },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && t.count > 0 && `(${t.count})`}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <>
          <div className="card mb-4">
            <h3 className="text-sm font-semibold mb-3">연결 저장소 · Repositories ({repos.length})</h3>
            {repos.length === 0 ? <p className="text-xs text-gray-400">연결된 저장소 없음</p> : (
              <div className="space-y-2">
                {repos.map((r: any) => (
                  <div key={r.id} className="flex items-center gap-3 text-sm p-2 bg-gray-50 rounded flex-wrap">
                    <span>📦</span>
                    <Link to={`/repositories/${r.id}`} className="text-blue-600 hover:underline font-medium">{r.name}</Link>
                    <span className="text-xs text-gray-400">{r.scm_provider || 'git'} · {r.default_branch}</span>
                    <span className={`badge-gray text-[10px] ml-auto`}>{r.sensitivity}</span>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3">허용 모델 · Allowed Models</h3>
            <div className="flex flex-wrap gap-1">
              <AllowedModelChips items={resolveAllowedModels(proj.allowed_model_classes, modelPackages)} />
            </div>
          </div>

          <UsageReport report={usage} id="project-usage-ledger" title="프로젝트 사용량 및 비용 원장" />
        </>
      )}

      {tab === 'members' && (
        <div className="card">
          <div className="flex justify-between items-center mb-3">
            <h3 className="text-sm font-semibold">멤버 · Roster ({members.length})</h3>
            <button onClick={() => setAddMember(true)} className="btn-sm btn-primary">+ 멤버 추가</button>
          </div>
          {members.length === 0 ? <p className="text-xs text-gray-400">멤버 없음 — 세션 기반 추정이 아닌 실제 명단입니다</p> : (
            <div className="space-y-2">
              {members.map((m: any) => (
                <div key={m.id} className="flex items-center gap-3 text-sm p-2 bg-gray-50 rounded">
                  {m.user ? (
                    <Link to={`/users/${m.user.id}`} className="text-blue-600 hover:underline font-medium">{m.user.name_ko || m.user.name}</Link>
                  ) : <span className="font-mono text-xs">{m.user_id}</span>}
                  {m.user?.email && <span className="text-xs text-gray-400">{m.user.email}</span>}
                  <span className={`badge-gray text-[10px]`}>{m.role}</span>
                  <button onClick={() => removeMember(m.user_id, m.user?.name_ko || m.user_id)} className="text-xs text-red-600 hover:underline ml-auto">제거</button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'sessions' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">세션 · Sessions ({sessions.length})</h3>
          {sessions.length === 0 ? <p className="text-xs text-gray-400">세션 없음</p> : (
            <div className="space-y-2">
              {sessions.map((s: any) => (
                <div key={s.id} className="flex items-center gap-3 text-sm p-2 bg-gray-50 rounded flex-wrap">
                  <Link to={`/sessions/${s.session_id || s.id}`} className="text-blue-600 hover:underline font-medium">{s.title || '제목 없음'}</Link>
                  <span className={`text-xs ${s.status === 'active' ? 'text-green-600' : s.status === 'terminated' ? 'text-red-600' : 'text-gray-400'}`}>{s.status}</span>
                  {s.model_class && <span className="text-xs text-gray-400">· {s.model_class}</span>}
                  <span className="text-xs text-gray-400 ml-auto">{formatRelative(s.opened_at)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'governance' && (
        <div className="space-y-4">
          <div className="card">
            <div className="flex justify-between items-center mb-3">
              <h3 className="text-sm font-semibold">정책 팩 · Policy Pack</h3>
              <button onClick={() => { setPackPick(proj.policy_pack_id || ''); setPackTarget(true) }} className="btn-sm btn-secondary">변경</button>
            </div>
            {detail.policy_pack ? (
              <div className="space-y-1 text-sm">
                <div><span className="text-gray-500">이름:</span> {detail.policy_pack.name} <span className="badge-blue">{detail.policy_pack.version}</span></div>
                <div><span className="text-gray-500">상태:</span> {detail.policy_pack.status}</div>
                <div><span className="text-gray-500">다이제스트:</span> <span className="font-mono text-xs">{(detail.policy_pack.digest || '-').slice(0, 24)}</span></div>
              </div>
            ) : <p className="text-xs text-gray-400">바인딩된 팩 없음 — 모델 허용 목록만 적용됩니다</p>}
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3">AI 변경 통제 큐 · Change-Control ({changeRequests.length})</h3>
            {changeRequests.length === 0 ? <p className="text-xs text-gray-400">대기 중인 변경 없음 — 고위험 변경셋이 도착하면 여기에 쌓입니다</p> : (
              <div className="space-y-2">
                {changeRequests.map((c: any) => (
                  <div key={c.id} className="flex items-center gap-3 text-sm p-3 bg-gray-50 rounded flex-wrap">
                    <div className="flex-1 min-w-[200px]">
                      <div className="font-medium">{c.title}</div>
                      <div className="text-xs text-gray-400">
                        {c.kind} · 위험 {c.risk_level} ({c.risk_score}/100) · <span className="font-mono">{c.change_set_id?.slice(0, 12)}</span>
                      </div>
                      {c.decision_reason && <div className="text-xs text-gray-500 mt-0.5">사유: {c.decision_reason}</div>}
                    </div>
                    {c.status === 'pending' ? (
                      <div className="flex gap-2 shrink-0 flex-wrap">
                        <button onClick={() => decideChange(c, true)} className="btn-sm btn-primary">승인</button>
                        <button onClick={() => decideChange(c, false)} className="btn-sm btn-danger">거부</button>
                      </div>
                    ) : (
                      <span className={c.status === 'approved' ? 'badge-green' : 'badge-red'}>{c.status}</span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {tab === 'audit' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">감사 이벤트 · Audit ({auditEvents.length})</h3>
          {auditEvents.length === 0 ? <p className="text-xs text-gray-400">감사 이벤트 없음</p> : (
            <div className="space-y-1">
              {auditEvents.map((a: any) => (
                <div key={a.id} className="text-xs flex justify-between gap-3 py-1 border-b border-gray-50">
                  <div><span className="font-medium text-gray-700">{a.action}</span><span className="text-gray-400 ml-2">{a.details?.slice(0, 100)}</span></div>
                  <span className="text-gray-400 flex-shrink-0">{formatRelative(a.occurred_at)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      <Modal open={addMember} title="멤버 추가 · Add Member" subtitle={proj.name_ko || proj.name} onClose={() => setAddMember(false)} size="sm"
        footer={<ModalFooter onCancel={() => setAddMember(false)} onConfirm={submitMember} confirmLabel="추가" disabled={!memberForm.user_id} />}>
        <div className="space-y-3">
          <div><label className="label">사용자 · User</label><EntitySelect entity="user" value={memberForm.user_id} onChange={v => setMemberForm({ ...memberForm, user_id: v })} /></div>
          <div><label className="label">역할 · Role</label><select className="input" value={memberForm.role} onChange={e => setMemberForm({ ...memberForm, role: e.target.value })}>{ROLES.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}</select></div>
        </div>
      </Modal>

      <Modal open={packTarget} title="정책 팩 바인딩 · Bind Policy Pack" subtitle={proj.name_ko || proj.name} onClose={() => setPackTarget(false)} size="sm"
        footer={<ModalFooter onCancel={() => setPackTarget(false)} onConfirm={submitPack} confirmLabel="바인딩" />}>
        <div className="space-y-3">
          <EntitySelect entity="policy_pack" value={packPick} onChange={setPackPick} noneLabel="해제 · None" />
          {packs.length === 0 && <p className="text-xs text-gray-400">정책 페이지에서 팩을 먼저 만드세요</p>}
        </div>
      </Modal>
    </div>
  )
}
