import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Repositories() {
  const [repos, setRepos] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ project_id: '', name: '', full_name: '', default_branch: 'main', sensitivity: 'internal' })

  const load = () => {
    fetch('/api/repositories', { headers: authHeaders() })
      .then(r => r.json()).then(data => setRepos(Array.isArray(data) ? data : data || []))
      .catch(() => setRepos([]))
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : data || []))
  }

  useEffect(() => { load() }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = repos[0]?.organization_id || projects[0]?.organization_id || ''
    try {
      await api.registerRepository({ ...form, organization_id: orgId })
      setShowForm(false)
      setForm({ project_id: '', name: '', full_name: '', default_branch: 'main', sensitivity: 'internal' })
      load()
    } catch (err: any) {
      alert('저장소 등록 실패: ' + err.message)
    }
  }

  const sensBadge = (s: string) => {
    const map: Record<string, string> = { public: 'badge-green', internal: 'badge-blue', confidential: 'badge-yellow', restricted: 'badge-red' }
    return map[s] || 'badge-gray'
  }

  const sensLabel = (s: string) => {
    const map: Record<string, string> = { public: '공개', internal: '내부', confidential: '기밀', restricted: '제한' }
    return map[s] || s
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">저장소 <span className="text-gray-400 text-lg font-normal">Repositories</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          {showForm ? '취소' : '+ 저장소 등록'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={handleCreate} className="card mb-6">
          <h2 className="text-lg font-semibold mb-4">새 저장소 등록 <span className="text-gray-400 text-sm font-normal">Register Repository</span></h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">프로젝트 · Project</label>
              <select className="input" value={form.project_id} onChange={e => setForm({ ...form, project_id: e.target.value })} required>
                <option value="">선택하세요...</option>
                {projects.map(p => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}
              </select>
            </div>
            <div>
              <label className="label">저장소명 · Repository Name</label>
              <input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="payment-service" required />
            </div>
            <div>
              <label className="label">전체 경로 · Full Name</label>
              <input className="input" value={form.full_name} onChange={e => setForm({ ...form, full_name: e.target.value })} placeholder="org/payment-service" required />
            </div>
            <div>
              <label className="label">기본 브랜치 · Default Branch</label>
              <input className="input" value={form.default_branch} onChange={e => setForm({ ...form, default_branch: e.target.value })} />
            </div>
            <div>
              <label className="label">민감도 · Sensitivity</label>
              <select className="input" value={form.sensitivity} onChange={e => setForm({ ...form, sensitivity: e.target.value })}>
                <option value="public">공개 · Public</option>
                <option value="internal">내부 · Internal</option>
                <option value="confidential">기밀 · Confidential</option>
                <option value="restricted">제한 · Restricted</option>
              </select>
            </div>
          </div>
          <button type="submit" className="btn-primary mt-4">등록 · Register</button>
        </form>
      )}

      <div className="card">
        {repos.length === 0 ? (
          <div className="text-center py-8">
            <p className="text-gray-400">등록된 저장소가 없습니다.</p>
            <p className="text-sm text-gray-400 mt-1">Git/SCM 저장소를 등록하면 브랜치 보호, 베이스라인 관리, 프로바이던스 추적이 가능합니다.</p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">저장소 · Repository</th>
                <th className="pb-3">기본 브랜치 · Branch</th>
                <th className="pb-3">민감도 · Sensitivity</th>
                <th className="pb-3">상태 · Status</th>
              </tr>
            </thead>
            <tbody>
              {repos.map(r => (
                <tr key={r.id} className="border-b border-gray-100 last:border-0 hover:bg-gray-50">
                  <td className="py-3">
                    <div className="font-medium">{r.name}</div>
                    <div className="text-xs text-gray-400">{r.full_name}</div>
                  </td>
                  <td className="py-3 text-sm font-mono">{r.default_branch}</td>
                  <td className="py-3"><span className={sensBadge(r.sensitivity)}>{sensLabel(r.sensitivity)}</span></td>
                  <td className="py-3"><span className="badge-green">{r.status}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
