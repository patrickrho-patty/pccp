import { useState, useEffect } from 'react'
import { api } from '../api'
import { Link } from 'react-router-dom'

export default function Sessions() {
  const [sessions, setSessions] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [users, setUsers] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [repos, setRepos] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [form, setForm] = useState({ user_id: '', project_id: '', repository_id: '', branch: '', title: '', task_purpose: '', model_class: 'pmp_kocoder_v1' })

  const load = () => {
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : data || []))
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : data || []))
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : data || []))
    api.listRepositories().then(data => setRepos(Array.isArray(data) ? data : data || []))
    api.listHarnesses().then(data => setHarnesses(Array.isArray(data) ? data : data || []))
  }

  useEffect(() => { load() }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = sessions[0]?.organization_id || users[0]?.organization_id || ''
    try {
      await api.openSession({ ...form, organization_id: orgId, harness_id: harnesses[0]?.harness_id || 'hrn_demo' })
      setShowForm(false)
      load()
    } catch (err: any) {
      alert('세션 생성 실패: ' + err.message)
    }
  }

  const handleClose = async (id: string) => {
    if (!confirm('이 세션을 종료하시겠습니까?')) return
    try { await api.closeSession(id); load() } catch {}
  }

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { active: 'badge-green', pending: 'badge-yellow', closed: 'badge-gray', terminated: 'badge-red', paused: 'badge-yellow', contained: 'badge-red' }
    return map[s] || 'badge-gray'
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">AI 세션 <span className="text-gray-400 text-lg font-normal">AI Sessions</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          {showForm ? '취소' : '+ 세션 시작'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={handleCreate} className="card mb-6">
          <h2 className="text-lg font-semibold mb-4">새 AI 코딩 세션 <span className="text-gray-400 text-sm font-normal">New Session</span></h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">개발자 · Developer</label>
              <select className="input" value={form.user_id} onChange={e => setForm({ ...form, user_id: e.target.value })} required>
                <option value="">선택하세요...</option>
                {users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>)}
              </select>
            </div>
            <div>
              <label className="label">프로젝트 · Project</label>
              <select className="input" value={form.project_id} onChange={e => setForm({ ...form, project_id: e.target.value })} required>
                <option value="">선택하세요...</option>
                {projects.map(p => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}
              </select>
            </div>
            <div>
              <label className="label">저장소 · Repository</label>
              <select className="input" value={form.repository_id} onChange={e => setForm({ ...form, repository_id: e.target.value })}>
                <option value="">선택 안함</option>
                {repos.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
              </select>
            </div>
            <div>
              <label className="label">브랜치 · Branch</label>
              <input className="input" value={form.branch} onChange={e => setForm({ ...form, branch: e.target.value })} placeholder="feature/new-feature" />
            </div>
            <div>
              <label className="label">세션 제목 · Session Title</label>
              <input className="input" value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} placeholder="환불 로직 구현" required />
            </div>
            <div>
              <label className="label">모델 · Model</label>
              <select className="input" value={form.model_class} onChange={e => setForm({ ...form, model_class: e.target.value })}>
                <option value="pmp_kocoder_v1">Patty-KoCoder-v1 (패티 코더)</option>
              </select>
            </div>
            <div className="col-span-2">
              <label className="label">작업 목적 · Task Purpose</label>
              <input className="input" value={form.task_purpose} onChange={e => setForm({ ...form, task_purpose: e.target.value })} placeholder="payment refund processing" />
            </div>
          </div>
          <button type="submit" className="btn-primary mt-4">세션 시작 · Start Session</button>
        </form>
      )}

      <div className="card">
        {sessions.length === 0 ? (
          <div className="text-center py-12">
            <div className="text-4xl mb-3">∅</div>
            <p className="text-gray-400 mb-2">활성 세션이 없습니다</p>
            <p className="text-sm text-gray-400">상단의 "세션 시작" 버튼으로 새 AI 코딩 세션을 시작하세요.</p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">제목 · Title</th>
                <th className="pb-3">개발자 · Developer</th>
                <th className="pb-3">모델 · Model</th>
                <th className="pb-3">브랜치 · Branch</th>
                <th className="pb-3">상태 · Status</th>
                <th className="pb-3">작업</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => {
                const user = users.find(u => u.id === s.user_id)
                return (
                  <tr key={s.id} className="border-b border-gray-100 last:border-0 hover:bg-gray-50">
                    <td className="py-3">
                      <div className="font-medium">{s.title || '제목 없음'}</div>
                      <div className="text-xs text-gray-400">{s.task_purpose}</div>
                    </td>
                    <td className="py-3 text-sm">{user?.name_ko || user?.name || s.user_id?.slice(0, 8)}</td>
                    <td className="py-3 text-sm">{s.model_class}</td>
                    <td className="py-3 text-sm font-mono">{s.branch || '-'}</td>
                    <td className="py-3"><span className={statusBadge(s.status)}>{s.status}</span></td>
                    <td className="py-3">
                      <div className="flex gap-2">
                        <Link to={`/sessions/${s.id}/provenance`} className="text-patty-600 text-sm hover:underline">프로바이던스</Link>
                        {s.status === 'active' && (
                          <button onClick={() => handleClose(s.id)} className="text-red-600 text-sm hover:underline ml-2">종료</button>
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
