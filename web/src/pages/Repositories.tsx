import { useState, useEffect } from 'react'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['name', 'full_name', 'scm_type'],
  searchPlaceholder: '저장소명, 경로로 검색...',
  dropdowns: [
    { key: 'sensitivity', label: '민감도', options: [
      { value: 'public', label: '공개' }, { value: 'internal', label: '내부' },
      { value: 'confidential', label: '기밀' }, { value: 'restricted', label: '제한' },
    ]},
    { key: 'status', label: '상태', options: [
      { value: 'active', label: '활성' }, { value: 'unregistered', label: '해제됨' },
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
  const [form, setForm] = useState({ project_id: '', name: '', full_name: '', default_branch: 'main', sensitivity: 'internal' })

  const load = () => { fetch('/api/repositories', { headers: authHeaders() }).then(r => r.json()).then(data => setRepos(Array.isArray(data) ? data : [])).catch(() => setRepos([])); api.listProjects().then(data => setProjects(Array.isArray(data) ? data : [])) }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(repos, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = repos[0]?.organization_id || projects[0]?.organization_id || ''
    try { await api.registerRepository({ ...form, organization_id: orgId }); setForm({ project_id: '', name: '', full_name: '', default_branch: 'main', sensitivity: 'internal' }); setShowForm(false); load() } catch (err: any) { alert('등록 실패: ' + err.message) }
  }
  const handleEdit = (repo: any) => { setEditingId(repo.id); setForm({ project_id: repo.project_id || '', name: repo.name || '', full_name: repo.full_name || '', default_branch: repo.default_branch || 'main', sensitivity: repo.sensitivity || 'internal' }); setShowForm(true) }
  const handleUpdate = async (e: React.FormEvent) => { e.preventDefault(); if (!editingId) return; try { await api.updateRepository(editingId, { sensitivity: form.sensitivity }); setEditingId(null); setShowForm(false); load() } catch (err: any) { alert('수정 실패: ' + err.message) } }
  const handleUnregister = async (id: string) => { if (confirm('등록 해제하시겠습니까?')) { try { await fetch(`/api/repositories/${id}`, { method: 'DELETE', headers: authHeaders() }); load() } catch {} } }

  const sensBadge = (s: string) => { const m: Record<string,string> = { public:'badge-green', internal:'badge-blue', confidential:'badge-yellow', restricted:'badge-red' }; return m[s] || 'badge-gray' }
  const sensLabel = (s: string) => { const m: Record<string,string> = { public:'공개', internal:'내부', confidential:'기밀', restricted:'제한' }; return m[s] || s }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">저장소 <span className="text-gray-400 text-lg font-normal">Repositories</span></h1>
        <button onClick={() => { if (editingId) { setEditingId(null); setForm({ project_id: '', name: '', full_name: '', default_branch: 'main', sensitivity: 'internal' }) } setShowForm(!showForm) }} className="btn-primary">{showForm ? '취소' : '+ 저장소 등록'}</button>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold">{editingId ? '저장소 수정 · Edit Repository' : '새 저장소 등록 · Register Repository'}</h2>
          {!editingId && (<div className="grid grid-cols-2 gap-4">
            <div><label className="label">프로젝트 · Project</label><select className="input" value={form.project_id} onChange={e => setForm({ ...form, project_id: e.target.value })} required><option value="">선택...</option>{projects.map(p => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}</select></div>
            <div><label className="label">저장소명 · Name</label><input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="payment-service" required /></div>
            <div><label className="label">전체 경로 · Full Name</label><input className="input" value={form.full_name} onChange={e => setForm({ ...form, full_name: e.target.value })} placeholder="org/payment-service" required /></div>
            <div><label className="label">기본 브랜치 · Default Branch</label><input className="input" value={form.default_branch} onChange={e => setForm({ ...form, default_branch: e.target.value })} /></div>
          </div>)}
          <div><label className="label">민감도 · Sensitivity</label><select className="input" value={form.sensitivity} onChange={e => setForm({ ...form, sensitivity: e.target.value })}><option value="public">공개 · Public</option><option value="internal">내부 · Internal</option><option value="confidential">기밀 · Confidential</option><option value="restricted">제한 · Restricted</option></select></div>
          <button type="submit" className="btn-primary">{editingId ? '수정 · Save' : '등록 · Register'}</button>
        </form>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        {paged.length === 0 ? (
          <div className="text-center py-8"><p className="text-gray-400">{filters.search ? '검색 결과가 없습니다' : '등록된 저장소가 없습니다'}</p></div>
        ) : (
          <table className="w-full">
            <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
              <th className="pb-3">저장소 · Repository</th><th className="pb-3">SCM</th><th className="pb-3">기본 브랜치</th><th className="pb-3">민감도</th><th className="pb-3">상태</th><th className="pb-3 text-right">작업</th>
            </tr></thead>
            <tbody>
              {paged.map(r => (
                <>
                  <tr key={r.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer" onClick={() => setExpandedId(expandedId === r.id ? null : r.id)}>
                    <td className="py-3"><div className="font-medium text-sm">{r.name}</div><div className="text-xs text-gray-400">{r.full_name}</div></td>
                    <td className="py-3"><span className="badge-gray">{r.scm_type}</span></td>
                    <td className="py-3 text-sm font-mono">{r.default_branch}</td>
                    <td className="py-3"><span className={sensBadge(r.sensitivity)}>{sensLabel(r.sensitivity)}</span></td>
                    <td className="py-3"><span className="badge-green">{r.status}</span></td>
                    <td className="py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex gap-2 justify-end">
                        <button onClick={() => handleEdit(r)} className="text-blue-600 text-xs hover:underline">수정</button>
                        <button onClick={() => handleUnregister(r.id)} className="text-red-600 text-xs hover:underline">해제</button>
                      </div>
                    </td>
                  </tr>
                  {expandedId === r.id && (
                    <tr className="bg-gray-50"><td colSpan={6} className="p-4">
                      <div className="grid grid-cols-3 gap-4 text-sm">
                        <div><span className="text-gray-500">프로젝트:</span> {r.project_id?.slice(0, 12) || '-'}</div>
                        <div><span className="text-gray-500">SCM 제공자:</span> {r.scm_provider || '-'}</div>
                        <div><span className="text-gray-500">Clone URL:</span> {r.clone_url || '-'}</div>
                        <div><span className="text-gray-500">생성일:</span> {r.created_at?.slice(0, 10)}</div>
                      </div>
                      <div className="mt-3 pt-3 border-t border-gray-200">
                        <div className="flex items-center gap-3">
                          <span className="text-xs font-semibold text-gray-600">브랜치 보호 · Branch Protection</span>
                          <select className="input max-w-[160px] text-xs" defaultValue="standard" onChange={async (e) => {
                            try {
                              await fetch('/api/scm/branch-protection', {
                                method: 'POST',
                                headers: { ...authHeaders(), 'Content-Type': 'application/json' },
                                body: JSON.stringify({ repository_id: r.id, branch: r.default_branch || 'main', level: e.target.value, requires_approval: e.target.value === 'release' || e.target.value === 'production' })
                              })
                            } catch {}
                          }}>
                            <option value="standard">표준 · Standard</option>
                            <option value="protected">보호됨 · Protected</option>
                            <option value="release">릴리스 · Release</option>
                            <option value="production">프로덕션 · Production</option>
                            <option value="locked">잠금 · Locked</option>
                          </select>
                          <span className="text-xs text-gray-400">브랜치: {r.default_branch || 'main'}</span>
                        </div>
                      </div>
                    </td></tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }
