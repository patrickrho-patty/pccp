import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Repositories() {
  const [repos, setRepos] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ organization_id: '', project_id: '', name: '', full_name: '', default_branch: 'main', sensitivity: 'internal' })

  useEffect(() => { api.listRepositories().then(setRepos) }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    await api.registerRepository(form)
    setShowForm(false)
    api.listRepositories().then(setRepos)
  }

  const sensBadge = (s: string) => {
    const map: Record<string, string> = { public: 'badge-green', internal: 'badge-blue', confidential: 'badge-yellow', restricted: 'badge-red' }
    return map[s] || 'badge-gray'
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
        <form onSubmit={handleCreate} className="card mb-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">조직 ID</label>
              <input className="input" value={form.organization_id} onChange={(e) => setForm({ ...form, organization_id: e.target.value })} required />
            </div>
            <div>
              <label className="label">프로젝트 ID</label>
              <input className="input" value={form.project_id} onChange={(e) => setForm({ ...form, project_id: e.target.value })} required />
            </div>
            <div>
              <label className="label">저장소명 (Name)</label>
              <input className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="payment-service" required />
            </div>
            <div>
              <label className="label">전체명 (Full Name)</label>
              <input className="input" value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} placeholder="org/payment-service" required />
            </div>
            <div>
              <label className="label">기본 브랜치</label>
              <input className="input" value={form.default_branch} onChange={(e) => setForm({ ...form, default_branch: e.target.value })} />
            </div>
            <div>
              <label className="label">민감도 (Sensitivity)</label>
              <select className="input" value={form.sensitivity} onChange={(e) => setForm({ ...form, sensitivity: e.target.value })}>
                <option value="public">Public</option>
                <option value="internal">Internal</option>
                <option value="confidential">Confidential</option>
                <option value="restricted">Restricted</option>
              </select>
            </div>
          </div>
          <button type="submit" className="btn-primary">등록</button>
        </form>
      )}
      <div className="card">
        <table className="w-full">
          <thead>
            <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
              <th className="pb-3">저장소명</th>
              <th className="pb-3">기본 브랜치</th>
              <th className="pb-3">민감도</th>
              <th className="pb-3">상태</th>
            </tr>
          </thead>
          <tbody>
            {repos.map((r) => (
              <tr key={r.id} className="border-b border-gray-100 last:border-0">
                <td className="py-3"><div className="font-medium">{r.name}</div><div className="text-xs text-gray-400">{r.full_name}</div></td>
                <td className="py-3 text-sm font-mono">{r.default_branch}</td>
                <td className="py-3"><span className={sensBadge(r.sensitivity)}>{r.sensitivity}</span></td>
                <td className="py-3"><span className="badge-green">{r.status}</span></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
