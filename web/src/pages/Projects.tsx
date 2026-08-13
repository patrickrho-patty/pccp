import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'
import ConfirmDialog from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import { formatRelative } from '../utils/format'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['name', 'name_ko', 'slug'],
  searchPlaceholder: '프로젝트명으로 검색...',
  dropdowns: [
    { key: 'status', label: '상태', options: [
      { value: 'active', label: '활성' }, { value: 'archived', label: '보관' },
    ]},
  ],
}

export default function Projects() {
  const [projects, setProjects] = useState<any[]>([])
  const [repos, setRepos] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [archiveTarget, setArchiveTarget] = useState<string | null>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [form, setForm] = useState({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard', description: '' })

  const load = () => {
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : []))
    api.listRepositories().then(data => setRepos(Array.isArray(data) ? data : []))
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : []))
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
  }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(projects, filters, FILTER_CONFIG)

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createProject({
        ...form,
        allowed_models: form.allowed_models.split(',').map(s => s.trim()),
      })
      setForm({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard', description: '' })
      setShowForm(false)
      load()
    } catch (err: any) { alert('생성 실패: ' + err.message) }
  }

  const handleEdit = (proj: any) => {
    setEditingId(proj.id)
    setForm({
      name: proj.name || '', name_ko: proj.name_ko || '', slug: proj.slug || '',
      allowed_models: Array.isArray(proj.allowed_model_classes) ? proj.allowed_model_classes.join(',') : (proj.allowed_model_classes || 'patty-code-standard'),
      description: proj.description || '',
    })
    setShowForm(true)
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingId) return
    try {
      await api.updateProject(editingId, { name: form.name, name_ko: form.name_ko, description: form.description })
      setEditingId(null)
      setForm({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard', description: '' })
      setShowForm(false)
      load()
    } catch (err: any) { alert('수정 실패: ' + err.message) }
  }

  const handleArchive = async (id: string) => {
    if (!confirm('이 프로젝트를 보관 처리하시겠습니까?')) return
    try { await api.deleteProject(id); load() } catch {}
  }

  const getProjectRepos = (projId: string) => repos.filter(r => r.project_id === projId)
  const getProjectSessions = (projId: string) => sessions.filter(s => s.project_id === projId)
  const getActiveSessions = (projId: string) => getProjectSessions(projId).filter(s => s.status === 'active')
  const getProjectMembers = (projId: string) => users.filter(u => u.id && sessions.some(s => s.project_id === projId && s.user_id === u.id)).length

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-2xl font-bold">프로젝트 <span className="text-gray-400 text-lg font-normal">Projects</span></h1>
          <p className="text-xs text-gray-400 mt-1">프로젝트별 저장소, 세션, 멤버 관리 · 프로젝트를 클릭하여 상세 보기</p>
        </div>
        <button onClick={() => { if (editingId) { setEditingId(null); setForm({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard', description: '' }) } setShowForm(!showForm) }} className="btn-primary">
          {showForm ? '취소' : '+ 프로젝트 생성'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold">{editingId ? '프로젝트 수정' : '새 프로젝트'}</h2>
          <div className="grid grid-cols-3 gap-4">
            <div><label className="label">프로젝트명 · Name</label><input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="my-project" required /></div>
            <div><label className="label">한글명 · Korean Name</label><input className="input" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} placeholder="마이 프로젝트" /></div>
            <div><label className="label">슬러그 · Slug</label><input className="input" value={form.slug} onChange={e => setForm({ ...form, slug: e.target.value })} placeholder="my-project" disabled={!!editingId} /></div>
            <div><label className="label">허용 모델 · Allowed Models</label><input className="input" value={form.allowed_models} onChange={e => setForm({ ...form, allowed_models: e.target.value })} placeholder="patty-code-standard, patty-code-fast" /></div>
            <div className="col-span-2"><label className="label">설명 · Description</label><input className="input" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} placeholder="프로젝트 설명" /></div>
          </div>
          <button type="submit" className="btn-primary">{editingId ? '수정 저장' : '생성'}</button>
        </form>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      {/* Project cards grid */}
      <div className="grid grid-cols-2 gap-4">
        {filtered.map(p => {
          const projRepos = getProjectRepos(p.id)
          const activeSessions = getActiveSessions(p.id)
          const allSessions = getProjectSessions(p.id)
          return (
            <div key={p.id} className="card">
              {/* Header */}
              <div className="flex items-start justify-between mb-3">
                <div>
                  <h3 className="text-base font-semibold"><Link to={`/projects/${p.id}`} className="text-blue-600 hover:underline">{p.name_ko || p.name}</Link></h3>
                  <p className="text-xs text-gray-400">{p.name} · {p.slug}</p>
                  {p.description && <p className="text-xs text-gray-500 mt-1">{p.description}</p>}
                </div>
                <div className="flex items-center gap-2">
                  <span className={p.status === 'archived' ? 'badge-gray' : 'badge-green'}>{p.status || 'active'}</span>
                </div>
              </div>

              {/* Stats row */}
              <div className="grid grid-cols-4 gap-2 mb-3">
                <div className="text-center p-2 bg-gray-50 rounded">
                  <div className="text-lg font-bold text-blue-600">{projRepos.length}</div>
                  <div className="text-[10px] text-gray-500">저장소</div>
                </div>
                <div className="text-center p-2 bg-gray-50 rounded">
                  <Link to="/sessions" className="text-lg font-bold text-purple-600 hover:underline">{activeSessions.length}</Link>
                  <div className="text-[10px] text-gray-500">활성 세션</div>
                </div>
                <div className="text-center p-2 bg-gray-50 rounded">
                  <Link to="/sessions" className="text-lg font-bold text-gray-600 hover:underline">{allSessions.length}</Link>
                  <div className="text-[10px] text-gray-500">전체 세션</div>
                </div>
                <div className="text-center p-2 bg-gray-50 rounded">
                  <Link to="/users" className="text-lg font-bold text-green-600 hover:underline">{getProjectMembers(p.id)}</Link>
                  <div className="text-[10px] text-gray-500">멤버</div>
                </div>
              </div>

              {/* Repositories */}
              <div className="mb-3">
                <div className="text-xs font-medium text-gray-500 mb-1">연결된 저장소 · Repositories</div>
                {projRepos.length === 0 ? (
                  <p className="text-xs text-gray-400">
                    <Link to="/repositories" className="text-blue-600 hover:underline">+ 저장소 연결</Link>
                  </p>
                ) : (
                  <div className="space-y-1">
                    {projRepos.map(r => (
                      <div key={r.id} className="flex items-center gap-2 text-xs p-1.5 bg-gray-50 rounded">
                        <span>📦</span>
                        <Link to="/repositories" className="text-blue-600 hover:underline font-medium">{r.name}</Link>
                        <span className="text-gray-400">{r.scm_provider || 'git'}</span>
                        {r.default_branch && <span className="text-gray-400 font-mono">{r.default_branch}</span>}
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* Allowed models */}
              {p.allowed_model_classes && (
                <div className="mb-3">
                  <div className="text-xs font-medium text-gray-500 mb-1">허용 모델</div>
                  <div className="flex flex-wrap gap-1">
                    {(Array.isArray(p.allowed_model_classes) ? p.allowed_model_classes : [p.allowed_model_classes]).map((m: string) => (
                      <span key={m} className="text-[10px] bg-blue-50 text-blue-600 px-1.5 py-0.5 rounded">{m}</span>
                    ))}
                  </div>
                </div>
              )}

              {/* Actions */}
              <div className="flex gap-2 pt-2 border-t border-gray-50">
                <button onClick={() => setExpandedId(expandedId === p.id ? null : p.id)} className="text-xs text-blue-600 hover:underline">
                  {expandedId === p.id ? '접기' : '상세'}
                </button>
                <button onClick={() => handleEdit(p)} className="text-xs text-blue-600 hover:underline">편집</button>
                <button onClick={() => setArchiveTarget(p.id)} className="text-xs text-red-600 hover:underline">보관</button>
                <Link to="/sessions" className="text-xs text-blue-600 hover:underline ml-auto">세션 보기 →</Link>
              </div>

              {/* Expanded detail */}
              {expandedId === p.id && (
                <div className="mt-3 pt-3 border-t border-gray-100 space-y-2">
                  <div className="text-xs text-gray-500">
                    <span className="font-medium">프로젝트 ID:</span> <span className="font-mono">{p.id}</span>
                  </div>
                  <div className="text-xs text-gray-500">
                    <span className="font-medium">생성일:</span> {formatRelative(p.created_at)}
                  </div>
                  {allSessions.length > 0 && (
                    <div>
                      <div className="text-xs font-medium text-gray-500 mb-1">최근 세션</div>
                      <div className="space-y-1">
                        {allSessions.slice(0, 5).map(s => (
                          <div key={s.id} className="flex items-center gap-2 text-xs">
                            <Link to="/sessions" className="text-blue-600 hover:underline">{s.title || '제목 없음'}</Link>
                            <span className={`px-1 py-0.5 rounded text-[10px] ${s.status === 'active' ? 'bg-green-50 text-green-600' : 'bg-gray-50 text-gray-400'}`}>{s.status}</span>
                            <span className="text-gray-400">{s.opened_at?.slice(0, 10)}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )
        })}
        {filtered.length === 0 && (
          <div className="col-span-2 card text-center py-12">
            <EmptyState icon="📂" title="프로젝트가 없습니다" message="프로젝트 생성 버튼으로 시작하세요" />
          </div>
        )}
      </div>

      <ConfirmDialog
        open={!!archiveTarget}
        title="프로젝트 보관 · Archive Project"
        message="이 프로젝트를 보관 처리하시겠습니까?"
        confirmLabel="보관 실행"
        danger
        onConfirm={async () => { if (archiveTarget) { try { await api.deleteProject(archiveTarget); load() } catch {} } setArchiveTarget(null) }}
        onCancel={() => setArchiveTarget(null)}
      />
    </div>
  )
}
