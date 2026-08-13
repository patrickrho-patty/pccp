import { useState, useEffect, Fragment } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['name', 'slug', 'clone_url', 'scm_provider'],
  searchPlaceholder: '저장소명, URL로 검색...',
  dropdowns: [
    { key: 'scm_provider', label: 'SCM', options: [
      { value: 'github', label: 'GitHub' }, { value: 'gitlab', label: 'GitLab' },
      { value: 'bitbucket', label: 'Bitbucket' }, { value: 'gitea', label: 'Gitea' },
    ]},
  ],
}

export default function Repositories() {
  const [repos, setRepos] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [form, setForm] = useState({ name: '', slug: '', project_id: '', scm_provider: 'github', clone_url: '', default_branch: 'main' })

  const load = () => {
    api.listRepositories().then(data => setRepos(Array.isArray(data) ? data : []))
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : []))
  }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(repos, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createRepository(form)
      setForm({ name: '', slug: '', project_id: '', scm_provider: 'github', clone_url: '', default_branch: 'main' })
      setShowForm(false)
      load()
    } catch (err: any) { alert('생성 실패: ' + err.message) }
  }

  const handleEdit = (r: any) => {
    setEditingId(r.id)
    setForm({ name: r.name || '', slug: r.slug || '', project_id: r.project_id || '', scm_provider: r.scm_provider || 'github', clone_url: r.clone_url || '', default_branch: r.default_branch || 'main' })
    setShowForm(true)
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingId) return
    try {
      await api.updateRepository(editingId, form)
      setEditingId(null)
      setShowForm(false)
      load()
    } catch { alert('수정 실패') }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('이 저장소를 삭제하시겠습니까?')) return
    try { await api.deleteRepository(id); load() } catch {}
  }

  const handleBranchProtection = async (repo: any) => {
    const level = prompt('브랜치 보호 수준을 선택하세요:\n1. 표준 (standard)\n2. 보호됨 (protected)\n3. 릴리스 (release)\n4. 프로덕션 (production)\n5. 잠금 (locked)', 'standard')
    if (!level) return
    try {
      await fetch('/api/scm/branch-protection', {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ repository_id: repo.id, branch: repo.default_branch || 'main', level, requires_approval: level === 'release' || level === 'production' }),
      })
      alert(`브랜치 보호 설정: ${level}`)
      load()
    } catch {}
  }

  const getProject = (projId: string) => projects.find(p => p.id === projId)

  const scmIcon: Record<string, string> = { github: '🐙', gitlab: '🦊', bitbucket: '🪣', gitea: '🍵', git: '📦' }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-2xl font-bold">저장소 <span className="text-gray-400 text-lg font-normal">Repositories</span></h1>
          <p className="text-xs text-gray-400 mt-1">Git 저장소 관리 · 브랜치 보호 · 프로젝트 연결 · PRD §18</p>
        </div>
        <button onClick={() => { setEditingId(null); setForm({ name: '', slug: '', project_id: '', scm_provider: 'github', clone_url: '', default_branch: 'main' }); setShowForm(!showForm) }} className="btn-primary">
          {showForm ? '취소' : '+ 저장소 추가'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold">{editingId ? '저장소 수정' : '새 저장소'}</h2>
          <div className="grid grid-cols-3 gap-4">
            <div><label className="label">저장소명 · Name</label><input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="backend-api" required /></div>
            <div><label className="label">슬러그 · Slug</label><input className="input" value={form.slug} onChange={e => setForm({ ...form, slug: e.target.value })} placeholder="backend-api" disabled={!!editingId} /></div>
            <div><label className="label">프로젝트 · Project</label>
              <select className="input" value={form.project_id} onChange={e => setForm({ ...form, project_id: e.target.value })}>
                <option value="">선택 안함</option>
                {projects.map(p => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}
              </select>
            </div>
            <div><label className="label">SCM 제공자</label>
              <select className="input" value={form.scm_provider} onChange={e => setForm({ ...form, scm_provider: e.target.value })}>
                <option value="github">GitHub</option><option value="gitlab">GitLab</option>
                <option value="bitbucket">Bitbucket</option><option value="gitea">Gitea</option>
                <option value="git">Git (기본)</option>
              </select>
            </div>
            <div><label className="label">Clone URL</label><input className="input font-mono text-xs" value={form.clone_url} onChange={e => setForm({ ...form, clone_url: e.target.value })} placeholder="https://github.com/org/repo.git" /></div>
            <div><label className="label">기본 브랜치</label><input className="input" value={form.default_branch} onChange={e => setForm({ ...form, default_branch: e.target.value })} placeholder="main" /></div>
          </div>
          <button type="submit" className="btn-primary">{editingId ? '수정 저장' : '생성'}</button>
        </form>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        <table className="w-full">
          <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
            <th className="pb-3">저장소</th>
            <th className="pb-3">프로젝트</th>
            <th className="pb-3">SCM</th>
            <th className="pb-3">브랜치</th>
            <th className="pb-3">작업</th>
          </tr></thead>
          <tbody>
            {paged.map(r => {
              const project = getProject(r.project_id)
              return (
<Fragment key={r.id}>
                  <tr key={r.id} className={`border-b border-gray-100 last:border-0 cursor-pointer ${expandedId === r.id ? 'bg-blue-50' : 'hover:bg-gray-50'}`}
                    onClick={() => setExpandedId(expandedId === r.id ? null : r.id)}>
                    <td className="py-3">
                      <div className="flex items-center gap-2">
                        <span className="text-lg">{scmIcon[r.scm_provider] || '📦'}</span>
                        <div>
                          <div className="font-medium text-sm">{r.name}</div>
                          {r.clone_url && <div className="text-xs text-gray-400 font-mono truncate max-w-xs">{r.clone_url}</div>}
                        </div>
                      </div>
                    </td>
                    <td className="py-3">
                      {project ? (
                        <Link to="/projects" className="text-sm text-blue-600 hover:underline">{project.name_ko || project.name}</Link>
                      ) : <span className="text-xs text-gray-400">-</span>}
                    </td>
                    <td className="py-3"><span className="badge-gray">{r.scm_provider || 'git'}</span></td>
                    <td className="py-3 text-sm font-mono">{r.default_branch || 'main'}</td>
                    <td className="py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex gap-2">
                        <button onClick={() => handleEdit(r)} className="text-xs text-blue-600 hover:underline">편집</button>
                        <button onClick={() => handleBranchProtection(r)} className="text-xs text-yellow-600 hover:underline">브랜치 보호</button>
                        <button onClick={() => handleDelete(r.id)} className="text-xs text-red-600 hover:underline">삭제</button>
                      </div>
                    </td>
                  </tr>
                  {expandedId === r.id && (
                    <tr className="bg-gray-50"><td colSpan={5} className="p-4">
                      <div className="grid grid-cols-3 gap-6">
                        <div>
                          <div className="text-xs font-semibold text-gray-600 mb-2">저장소 정보</div>
                          <div className="space-y-1 text-xs text-gray-500">
                            <div>ID: <span className="font-mono">{r.id}</span></div>
                            <div>슬러그: {r.slug || '-'}</div>
                            <div>Clone URL: {r.clone_url || '-'}</div>
                            <div>기본 브랜치: <span className="font-mono">{r.default_branch || 'main'}</span></div>
                            <div>생성일: {r.created_at?.slice(0, 10)}</div>
                          </div>
                        </div>
                        <div>
                          <div className="text-xs font-semibold text-gray-600 mb-2">프로젝트</div>
                          {project ? (
                            <div className="space-y-1 text-xs">
                              <Link to="/projects" className="text-blue-600 hover:underline font-medium">{project.name_ko || project.name}</Link>
                              <div className="text-gray-400">{project.slug}</div>
                              {project.description && <div className="text-gray-500">{project.description}</div>}
                              <Link to="/projects" className="text-blue-600 hover:underline mt-1 block">프로젝트 보기 →</Link>
                            </div>
                          ) : (
                            <div className="text-xs text-gray-400">
                              <p>연결된 프로젝트 없음</p>
                              <Link to="/projects" className="text-blue-600 hover:underline mt-1 block">프로젝트 연결 →</Link>
                            </div>
                          )}
                        </div>
                        <div>
                          <div className="text-xs font-semibold text-gray-600 mb-2">보안 및 거버넌스</div>
                          <div className="space-y-2">
                            <button onClick={() => handleBranchProtection(r)} className="text-xs text-yellow-600 hover:underline block">🌿 브랜치 보호 설정</button>
                            <Link to="/explorer" className="text-xs text-blue-600 hover:underline block">🔬 프로바이던스 탐색 →</Link>
                            <Link to="/sessions" className="text-xs text-blue-600 hover:underline block">◐ 관련 세션 보기 →</Link>
                            <Link to="/security" className="text-xs text-blue-600 hover:underline block">🛡 보안 발견 보기 →</Link>
                          </div>
                        </div>
                      </div>
                    </td></tr>
                  )}
</Fragment>
              )
            })}
          </tbody>
        </table>
        {paged.length === 0 && <p className="text-gray-400 text-center py-8">저장소가 없습니다</p>}
        <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
      </div>
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }