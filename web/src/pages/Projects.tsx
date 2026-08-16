import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useServerTable } from '../hooks/useServerTable'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { StatCard } from '../components/StatCard'
import { EntitySelect } from '../components/EntitySelect'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { formatRelative } from '../utils/format'
import { exportCSV } from '../utils/csv'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

const PAGE_SIZE = 12
const ROLES = [
  { value: 'owner', label: '소유자 · Owner' },
  { value: 'admin', label: '관리자 · Admin' },
  { value: 'member', label: '멤버 · Member' },
  { value: 'viewer', label: '뷰어 · Viewer' },
]

export default function Projects() {
  const confirm = useConfirm()
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [archiveTarget, setArchiveTarget] = useState<any | null>(null)
  const [archiveImpact, setArchiveImpact] = useState<any>(null)
  const [memberTarget, setMemberTarget] = useState<any | null>(null)
  const [memberForm, setMemberForm] = useState({ user_id: '', role: 'member' })
  const [attachTarget, setAttachTarget] = useState<any | null>(null)
  const [attachRepo, setAttachRepo] = useState('')
  const [catalogModels, setCatalogModels] = useState<any[]>([])
  const [packTarget, setPackTarget] = useState<any | null>(null)
  const [packs, setPacks] = useState<any[]>([])
  const { favorites, sortPinnedFirst } = useFavorites('projects')
  const [tab, setTab] = useState('active')
  const [repos, setRepos] = useState<any[]>([])

  const [form, setForm] = useState({
    name: '', name_ko: '', slug: '', allowed_models: ['patty-code-standard'],
    description: '', project_code: '', group_affiliate: '', policy_pack_id: '',
  })

  const table = useServerTable<any>((q) =>
    api.listProjects({
      page: String(q.page), size: String(q.size), search: q.search,
      ...q.filters,
      ...(tab === 'active' ? { status: 'active' } : { status: 'archived' }),
    })
  , { size: PAGE_SIZE })

  const load = () => {
    api.catalogModels().then(data => setCatalogModels(Array.isArray(data) ? data : [])).catch(() => {})
    api.listPolicyPacks().then(data => setPacks(Array.isArray(data) ? data : [])).catch(() => {})
    api.listRepositories().then(data => setRepos(Array.isArray(data) ? data : [])).catch(() => {})
  }
  useEffect(() => { load() }, [])
  useEffect(() => { table.reload() }, [tab])

  const rows = useMemo(() => sortPinnedFirst(table.rows, p => p.id), [table.rows, favorites])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createProject({ ...form })
      setForm({ name: '', name_ko: '', slug: '', allowed_models: ['patty-code-standard'], description: '', project_code: '', group_affiliate: '', policy_pack_id: '' })
      setShowForm(false)
      showToast('프로젝트 생성됨', 'success')
      table.reload(); load()
    } catch (err: any) { showToast('생성 실패: ' + err.message, 'error') }
  }

  const handleEdit = (proj: any) => {
    setEditingId(proj.id)
    setForm({
      name: proj.name || '', name_ko: proj.name_ko || '', slug: proj.slug || '',
      allowed_models: Array.isArray(proj.allowed_model_classes) ? proj.allowed_model_classes : (proj.allowed_model_classes ? [proj.allowed_model_classes] : ['patty-code-standard']),
      description: proj.description || '', project_code: proj.project_code || '',
      group_affiliate: proj.group_affiliate || '', policy_pack_id: proj.policy_pack_id || '',
    })
    setShowForm(true)
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingId) return
    try {
      await api.updateProject(editingId, {
        name: form.name, name_ko: form.name_ko, description: form.description,
        allowed_models: form.allowed_models, project_code: form.project_code,
        group_affiliate: form.group_affiliate, policy_pack_id: form.policy_pack_id,
      })
      setEditingId(null); setShowForm(false)
      showToast('수정 완료', 'success')
      table.reload()
    } catch (err: any) { showToast('수정 실패: ' + err.message, 'error') }
  }

  const requestArchive = async (proj: any) => {
    setArchiveTarget(proj)
    try { setArchiveImpact(await api.projectArchiveImpact(proj.id)) } catch { setArchiveImpact(null) }
  }

  const handleArchive = async () => {
    if (!archiveTarget) return
    try { await api.deleteProject(archiveTarget.id); showToast('보관됨 · 새 세션이 차단됩니다', 'info'); setArchiveTarget(null); table.reload() } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const handleRestore = async (id: string) => {
    if (!await confirm({ title: '복원', message: '이 프로젝트를 복원하시겠습니까?', danger: false })) return
    try { await api.restoreProject(id); showToast('복원됨', 'success'); table.reload() } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const submitMember = async () => {
    if (!memberTarget || !memberForm.user_id) { showToast('사용자를 선택하세요', 'error'); return }
    try {
      await api.addProjectMember(memberTarget.id, memberForm)
      showToast('멤버 추가됨', 'success')
      setMemberTarget(null); setMemberForm({ user_id: '', role: 'member' })
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const submitAttach = async () => {
    if (!attachTarget || !attachRepo) return
    try {
      await api.updateRepository(attachRepo, { project_id: attachTarget.id })
      showToast('저장소 연결됨', 'success')
      setAttachTarget(null); setAttachRepo('')
      table.reload()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const unassignedRepos = repos.filter(r => !r.project_id || r.project_id === '')

  return (
    <div>
      <div className="flex justify-between items-center mb-6 flex-wrap gap-2">
        <div>
          <h1 className="text-2xl font-bold">프로젝트 <span className="text-gray-400 text-lg font-normal">Projects</span></h1>
          <p className="text-xs text-gray-400 mt-1">프로젝트별 저장소, 세션, 멤버, 정책 관리 · 카드 클릭 → 상세</p>
        </div>
        <div className="flex gap-2 shrink-0 flex-wrap">
          <button onClick={() => { if (editingId) { setEditingId(null); setForm({ name: '', name_ko: '', slug: '', allowed_models: ['patty-code-standard'], description: '', project_code: '', group_affiliate: '', policy_pack_id: '' }) } setShowForm(!showForm) }} className="btn-primary">
            {showForm ? '취소' : '+ 프로젝트 생성'}
          </button>
          <button onClick={() => exportCSV(`projects_${new Date().toISOString().slice(0,10)}.csv`, ['프로젝트명', '한글명', '슬러그', '코드', '그룹', '상태', '생성일'], table.rows.map(p => [p.name, p.name_ko, p.slug, p.project_code, p.group_affiliate, p.status, p.created_at?.slice(0,10)]))} className="btn-sm btn-secondary">📥 CSV</button>
        </div>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4 expand-enter">
          <h2 className="text-sm font-semibold">{editingId ? '프로젝트 수정' : '새 프로젝트'}</h2>
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
            <div><label className="label">프로젝트명 · Name</label><input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value, slug: !editingId ? e.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, '-') : form.slug })} placeholder="my-project" required /></div>
            <div><label className="label">한글명 · Korean Name</label><input className="input" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} placeholder="마이 프로젝트" /></div>
            <div><label className="label">슬러그 · Slug</label><input className="input" value={form.slug} onChange={e => setForm({ ...form, slug: e.target.value })} placeholder="my-project" disabled={!!editingId} /></div>
            <div><label className="label">프로젝트 코드 · Code</label><input className="input" value={form.project_code} onChange={e => setForm({ ...form, project_code: e.target.value })} placeholder="기업 전산 코드 (선택)" /></div>
            <div><label className="label">그룹/계열사 · Group Affiliate</label><input className="input" value={form.group_affiliate} onChange={e => setForm({ ...form, group_affiliate: e.target.value })} placeholder="계열사명 (선택)" /></div>
            <div><label className="label">정책 팩 · Policy Pack</label><EntitySelect entity="policy_pack" value={form.policy_pack_id} onChange={v => setForm({ ...form, policy_pack_id: v })} /></div>
            <div className="col-span-2 md:col-span-3">
              <label className="label">허용 모델 · Allowed Models (카탈로그)</label>
              {catalogModels.length > 0 ? (
                <div className="flex flex-wrap gap-2 border border-gray-200 rounded-md p-3">
                  {catalogModels.map(m => (
                    <label key={m.catalog_model_id} className="flex items-center gap-1 text-sm cursor-pointer">
                      <input type="checkbox" checked={form.allowed_models.includes(m.catalog_model_id)} onChange={e => {
                        const current = [...form.allowed_models].filter(Boolean)
                        if (e.target.checked) current.push(m.catalog_model_id)
                        else { const i = current.indexOf(m.catalog_model_id); if (i >= 0) current.splice(i, 1) }
                        setForm({ ...form, allowed_models: current })
                      }} />
                      <span>{m.display_name_ko || m.display_name || m.catalog_model_id}</span>
                    </label>
                  ))}
                </div>
              ) : (
                <input className="input" value={form.allowed_models.join(',')} onChange={e => setForm({ ...form, allowed_models: e.target.value.split(',') })} placeholder="patty-code-standard, patty-code-fast" />
              )}
            </div>
            <div className="col-span-2 md:col-span-3"><label className="label">설명 · Description</label><input className="input" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} placeholder="프로젝트 설명" /></div>
          </div>
          <button type="submit" className="btn-primary">{editingId ? '수정 저장' : '생성'}</button>
        </form>
      )}

      {/* Active / Archived sub-menu (UX12) */}
      <div className="flex gap-1 mb-4 border-b border-gray-200">
        {[
          { id: 'active', label: '활성', en: 'Active' },
          { id: 'archived', label: '보관됨', en: 'Archived' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} <span className="text-xs text-gray-400">{t.en}</span>
          </button>
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-2 mb-4">
        <input className="input flex-1 min-w-[200px]" placeholder="프로젝트명, 슬러그 검색..." value={table.search} onChange={e => table.setSearch(e.target.value)} />
        <select className="input max-w-[160px] text-xs" value={table.filters.group_affiliate || ''} onChange={e => table.setFilter('group_affiliate', e.target.value)}>
          <option value="">그룹: 전체</option>
          {[...new Set((table.rows as any[]).map(p => p.group_affiliate).filter(Boolean))].map(g => <option key={g} value={g}>{g}</option>)}
        </select>
      </div>

      {table.loading && table.rows.length === 0 ? (
        <div className="grid grid-cols-2 gap-4 animate-pulse">
          {[0,1,2,3].map(i => <div key={i} className="card h-40"><div className="h-4 bg-gray-100 rounded w-1/2 mb-3" /><div className="h-3 bg-gray-100 rounded w-3/4" /></div>)}
        </div>
      ) : table.rows.length === 0 ? (
        <div className="card text-center py-12">
          <EmptyState
            icon="📂"
            title={tab === 'archived' ? '보관된 프로젝트가 없습니다' : '프로젝트가 없습니다'}
            message={tab === 'active' ? '프로젝트 생성 버튼으로 첫 프로젝트를 만드세요' : '활성 탭에서 프로젝트를 보관하면 여기에 표시됩니다'}
            action={tab === 'active' ? { label: '+ 프로젝트 생성', onClick: () => setShowForm(true) } : undefined}
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {rows.map(p => {
            const projRepos = repos.filter(r => r.project_id === p.id)
            return (
              <div key={p.id} className="card hover:border-gray-300 transition-colors">
                <div className="flex items-start justify-between mb-3">
                  <div>
                    <h3 className="text-base font-semibold flex items-center gap-2">
                      <input type="checkbox" checked={selectedProjects.has(p.id)} onChange={() => { const n = new Set(selectedProjects); if (n.has(p.id)) n.delete(p.id); else n.add(p.id); setSelectedProjects(n) }} />
                      <FavoriteStar entity="projects" id={p.id} />
                      <Link to={`/projects/${p.id}`} className="text-blue-600 hover:underline">{p.name_ko || p.name}</Link>
                    </h3>
                    <p className="text-xs text-gray-400">{p.name} · {p.slug}{p.project_code && <span> · 코드 {p.project_code}</span>}</p>
                    {p.group_affiliate && <p className="text-xs text-gray-400">🏢 {p.group_affiliate}</p>}
                    {p.description && <p className="text-xs text-gray-500 mt-1">{p.description}</p>}
                  </div>
                  <div className="flex items-center gap-2">
                    {p.policy_pack_id && <span className="badge-blue" title="정책 팩 바인딩됨">팩</span>}
                    <span className={p.status === 'archived' ? 'badge-gray' : 'badge-green'}>{p.status || 'active'}</span>
                  </div>
                </div>

              {/* Stats — every count is a drill-down (00 A5, projects UX5) */}
              <div className="grid grid-cols-4 gap-2 mb-3 stat-grid">
                <StatCard label="저장소" value={projRepos.length} accent="blue" to="/repositories" query={`?project_id=${p.id}`} />
                <StatCard label="멤버" value={p.member_count ?? '-'} accent="green" to={`/projects/${p.id}`} />
                <StatCard label="활성 세션" value={p.active_session_count ?? '-'} accent="purple" to="/sessions" query={`?project_id=${p.id}`} />
                <StatCard label="전체 세션" value={p.session_count ?? '-'} accent="gray" to="/sessions" query={`?project_id=${p.id}`} />
              </div>

              <div className="mb-3">
                <div className="text-xs font-medium text-gray-500 mb-1">연결된 저장소 · Repositories ({projRepos.length})</div>
                {projRepos.length === 0 ? (
                  <button onClick={() => { setAttachTarget(p); setAttachRepo('') }} className="btn-link">+ 저장소 연결 (인라인)</button>
                ) : (
                  <div className="space-y-1">
                    {projRepos.slice(0, 4).map(r => (
                      <div key={r.id} className="flex items-center gap-2 text-xs p-1.5 bg-gray-50 rounded">
                        <span>📦</span>
                        <Link to={`/repositories/${r.id}`} className="text-blue-600 hover:underline font-medium">{r.name}</Link>
                        <span className="text-gray-400">{r.scm_provider || 'git'}</span>
                        {r.default_branch && <span className="text-gray-400 font-mono">{r.default_branch}</span>}
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {p.allowed_model_classes && (
                <div className="mb-3">
                  <div className="text-xs font-medium text-gray-500 mb-1">허용 모델</div>
                  <div className="flex flex-wrap gap-1">
                    {(Array.isArray(p.allowed_model_classes) ? p.allowed_model_classes : [p.allowed_model_classes]).map((m: string) => (
                      <Link key={m} to={`/models/${m}`} className="text-[10px] bg-blue-50 text-blue-600 px-1.5 py-0.5 rounded hover:bg-blue-100" title="모델 상세 →">{m}</Link>
                    ))}
                  </div>
                </div>
              )}

              <div className="flex gap-2 pt-2 border-t border-gray-50 flex-wrap">
                <button onClick={() => setExpandedId(expandedId === p.id ? null : p.id)} className="btn-link">{expandedId === p.id ? '접기' : '상세'}</button>
                <button onClick={() => handleEdit(p)} className="btn-link">편집</button>
                <button onClick={() => { setMemberTarget(p); setMemberForm({ user_id: '', role: 'member' }) }} className="btn-link">멤버</button>
                {p.status === 'active'
                  ? <button onClick={() => requestArchive(p)} className="btn-link-danger">보관</button>
                  : <button onClick={() => handleRestore(p.id)} className="text-xs text-green-600 hover:underline">복원</button>}
                <Link to={`/projects/${p.id}`} className="text-xs text-blue-600 hover:underline ml-auto">상세 →</Link>
              </div>

              {expandedId === p.id && (
                <div className="mt-3 pt-3 border-t border-gray-100 space-y-2 expand-enter">
                  <div className="text-xs text-gray-500"><span className="font-medium">프로젝트 ID:</span> <span className="font-mono">{p.id}</span></div>
                  <div className="text-xs text-gray-500"><span className="font-medium">생성일:</span> {formatRelative(p.created_at)}</div>
                  {p.allowed_model_classes && (
                    <div className="text-xs text-gray-500"><span className="font-medium">허용 모델:</span> {Array.isArray(p.allowed_model_classes) ? p.allowed_model_classes.join(', ') : p.allowed_model_classes}</div>
                  )}
                  <div className="text-xs text-gray-500"><span className="font-medium">정책 팩:</span> {packs.find(pk => pk.id === p.policy_pack_id)?.name || p.policy_pack_id || '-'}</div>
                  <Link to={`/projects/${p.id}`} className="text-xs text-blue-600 hover:underline block">멤버/사용량/변경승인 큐 → 상세 페이지</Link>
                </div>
              )}
            </div>
            )
          })}
        </div>
      )}

      <div className="flex items-center justify-between mt-4 text-xs text-gray-500">
        <span>{(table.page - 1) * PAGE_SIZE + 1}-{Math.min(table.page * PAGE_SIZE, table.total)} / {table.total}건</span>
        <div className="flex gap-1">
          <button onClick={() => table.setPage(table.page - 1)} disabled={table.page === 1} className="btn-sm btn-secondary">이전</button>
          <span className="px-2 py-1">{table.page} / {Math.max(Math.ceil(table.total / PAGE_SIZE), 1)}</span>
          <button onClick={() => table.setPage(table.page + 1)} disabled={table.page * PAGE_SIZE >= table.total} className="btn-sm btn-secondary">다음</button>
        </div>
      </div>

      {/* Archive confirm — impact preview (UX14) */}
      <Modal open={!!archiveTarget} title="프로젝트 보관 · Archive Project" subtitle={archiveTarget?.name_ko || archiveTarget?.name} onClose={() => setArchiveTarget(null)} size="sm"
        footer={<ModalFooter onCancel={() => setArchiveTarget(null)} onConfirm={handleArchive} confirmLabel="보관 실행" danger />}>
        <div className="space-y-3">
          <p className="text-sm text-gray-600">보관하면 새 세션이 차단됩니다. 복원 시 해제됩니다.</p>
          {archiveImpact && (
            <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-3 text-sm space-y-1">
              <div>⚠ 영향 미리보기 · Impact:</div>
              <div className="text-xs text-gray-600">· {archiveImpact.active_sessions}개 활성 세션이 동결됩니다</div>
              <div className="text-xs text-gray-600">· {archiveImpact.repositories}개 저장소가 그대로 유지됩니다</div>
              <div className="text-xs text-gray-600">· {archiveImpact.members}명 멤버 기록이 유지됩니다</div>
            </div>
          )}
        </div>
      </Modal>

      {/* Member assignment (B1) */}
      <Modal open={!!memberTarget} title="멤버 추가 · Add Member" subtitle={memberTarget?.name_ko || memberTarget?.name} onClose={() => setMemberTarget(null)} size="sm"
        footer={<ModalFooter onCancel={() => setMemberTarget(null)} onConfirm={submitMember} confirmLabel="추가" disabled={!memberForm.user_id} />}>
        <div className="space-y-3">
          <div>
            <label className="label">사용자 · User</label>
            <EntitySelect entity="user" value={memberForm.user_id} onChange={v => setMemberForm({ ...memberForm, user_id: v })} />
          </div>
          <div>
            <label className="label">역할 · Role</label>
            <select className="input" value={memberForm.role} onChange={e => setMemberForm({ ...memberForm, role: e.target.value })}>
              {ROLES.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}
            </select>
          </div>
        </div>
      </Modal>

      {/* Inline repo attach (UX9) */}
      <Modal open={!!attachTarget} title="저장소 연결 · Attach Repository" subtitle={attachTarget?.name_ko || attachTarget?.name} onClose={() => setAttachTarget(null)} size="sm"
        footer={<ModalFooter onCancel={() => setAttachTarget(null)} onConfirm={submitAttach} confirmLabel="연결" disabled={!attachRepo} />}>
        <div>
          <label className="label">저장소 · Repository</label>
          {unassignedRepos.length === 0 ? (
            <p className="text-xs text-gray-400">미연결 저장소가 없습니다. <Link to="/repositories" className="text-blue-600 hover:underline">저장소 페이지에서 추가 →</Link></p>
          ) : (
            <EntitySelect entity="repository" value={attachRepo} onChange={setAttachRepo} />
          )}
        </div>
      </Modal>
    </div>
  )
}
