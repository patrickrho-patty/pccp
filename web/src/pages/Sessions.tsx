import { useState, useEffect } from 'react'
import { api } from '../api'
import { Link } from 'react-router-dom'

export default function Sessions() {
  const [sessions, setSessions] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [repos, setRepos] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [provenance, setProvenance] = useState<any>(null)
  const [form, setForm] = useState({
    user_id: '', project_id: '', repository_id: '', branch: '',
    title: '', task_purpose: '', model_class: 'patty-code-standard',
  })

  const load = () => {
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : []))
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : []))
    api.listRepositories().then(data => setRepos(Array.isArray(data) ? data : []))
    api.listHarnesses().then(data => setHarnesses(Array.isArray(data) ? data : []))
  }

  useEffect(() => { load() }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = sessions[0]?.organization_id || users[0]?.organization_id || ''
    const harnessId = harnesses[0]?.harness_id || 'hrn_dev'
    try {
      await api.openSession({ ...form, organization_id: orgId, harness_id: harnessId })
      setShowForm(false)
      setForm({ user_id: '', project_id: '', repository_id: '', branch: '', title: '', task_purpose: '', model_class: 'patty-code-standard' })
      load()
    } catch (err: any) { alert('세션 생성 실패: ' + err.message) }
  }

  const handleClose = async (id: string) => {
    if (!confirm('이 세션을 종료하시겠습니까?')) return
    try { await api.closeSession(id); load() } catch {}
  }

  const handlePause = async (id: string) => {
    try { await api.pauseSession(id); load() } catch {}
  }

  const handleResume = async (id: string) => {
    try { await api.resumeSession(id); load() } catch {}
  }

  const toggleExpand = async (session: any) => {
    if (expandedId === session.id) {
      setExpandedId(null)
      return
    }
    setExpandedId(session.id)
    try {
      const chain = await api.getProvenance(session.id)
      setProvenance(chain)
    } catch { setProvenance(null) }
  }

  const getUserName = (userId: string) => {
    const u = users.find(u => u.id === userId)
    return u?.name_ko || u?.name || userId?.slice(0, 8)
  }

  const statusBadge = (s: string) => {
    const map: Record<string, string> = {
      active: 'badge-green', pending: 'badge-yellow', closed: 'badge-gray',
      terminated: 'badge-red', paused: 'badge-yellow',
    }
    return map[s] || 'badge-gray'
  }

  const statusLabel = (s: string) => {
    const map: Record<string, string> = {
      active: '활성', pending: '대기', closed: '종료', terminated: '강제종료', paused: '일시정지',
    }
    return map[s] || s
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
          <h2 className="text-sm font-semibold mb-4">새 AI 코딩 세션 · New Session</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">개발자 · Developer</label>
              <select className="input" value={form.user_id} onChange={e => setForm({ ...form, user_id: e.target.value })} required>
                <option value="">선택...</option>
                {users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>)}
              </select>
            </div>
            <div>
              <label className="label">프로젝트 · Project</label>
              <select className="input" value={form.project_id} onChange={e => setForm({ ...form, project_id: e.target.value })} required>
                <option value="">선택...</option>
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
              <input className="input" value={form.branch} onChange={e => setForm({ ...form, branch: e.target.value })} placeholder="feature/refund" />
            </div>
            <div>
              <label className="label">세션 제목 · Title</label>
              <input className="input" value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} placeholder="환불 로직 구현" required />
            </div>
            <div>
              <label className="label">모델 · Model</label>
              <select className="input" value={form.model_class} onChange={e => setForm({ ...form, model_class: e.target.value })}>
                <option value="patty-code-standard">Patty Code Standard (패티 코드 스탠다드)</option>
                <option value="patty-code-pro">Patty Code Pro (패티 코드 프로)</option>
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
            <p className="text-gray-400 mb-2">활성 세션이 없습니다</p>
            <p className="text-sm text-gray-400">상단의 "세션 시작" 버튼으로 새 AI 코딩 세션을 시작하세요.</p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3">제목 · Title</th>
                <th className="pb-3">개발자</th>
                <th className="pb-3">모델</th>
                <th className="pb-3">브랜치</th>
                <th className="pb-3">상태</th>
                <th className="pb-3 text-right">작업</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <>
                  <tr key={s.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer"
                      onClick={() => toggleExpand(s)}>
                    <td className="py-3">
                      <div className="font-medium text-sm">{s.title || '제목 없음'}</div>
                      <div className="text-xs text-gray-400">{s.task_purpose}</div>
                    </td>
                    <td className="py-3 text-sm">{getUserName(s.user_id)}</td>
                    <td className="py-3 text-sm">{s.model_class}</td>
                    <td className="py-3 text-sm font-mono">{s.branch || '-'}</td>
                    <td className="py-3"><span className={statusBadge(s.status)}>{statusLabel(s.status)}</span></td>
                    <td className="py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex gap-2 justify-end">
                        {s.status === 'active' && (
                          <>
                            <button onClick={() => handlePause(s.id)} className="text-yellow-600 text-xs hover:underline">일시정지</button>
                            <button onClick={() => handleClose(s.id)} className="text-red-600 text-xs hover:underline">종료</button>
                          </>
                        )}
                        {s.status === 'paused' && (
                          <button onClick={() => handleResume(s.id)} className="text-green-600 text-xs hover:underline">재개</button>
                        )}
                        <Link to={`/sessions/${s.id}/provenance`} className="text-blue-600 text-xs hover:underline">프로바이던스</Link>
                      </div>
                    </td>
                  </tr>
                  {expandedId === s.id && (
                    <tr className="bg-gray-50">
                      <td colSpan={6} className="p-4">
                        <div className="grid grid-cols-3 gap-4 text-sm">
                          <div>
                            <span className="text-gray-500">세션 ID:</span>{' '}
                            <span className="font-mono text-xs">{s.session_id?.slice(0, 30)}</span>
                          </div>
                          <div>
                            <span className="text-gray-500">하네스:</span>{' '}
                            <span className="font-mono text-xs">{s.harness_id}</span>
                          </div>
                          <div>
                            <span className="text-gray-500">시작:</span>{' '}
                            <span className="text-xs">{s.opened_at?.slice(0, 19)}</span>
                          </div>
                        </div>
                        {provenance && provenance.actions?.length > 0 && (
                          <div className="mt-3 pt-3 border-t border-gray-200">
                            <div className="text-xs font-semibold text-gray-600 mb-2">액션 이력 · Action Timeline ({provenance.actions.length})</div>
                            <div className="space-y-1">
                              {provenance.actions.slice(0, 5).map((a: any, i: number) => (
                                <div key={i} className="flex items-center gap-2 text-xs">
                                  <span className="text-blue-500">●</span>
                                  <span className="font-mono">{a.action_type}</span>
                                  <span className="text-gray-400">{a.occurred_at?.slice(0, 19)}</span>
                                  <span className="badge-green">{a.verdict_result}</span>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                        {provenance && provenance.change_sets?.length > 0 && (
                          <div className="mt-3 pt-3 border-t border-gray-200">
                            <div className="text-xs font-semibold text-gray-600 mb-2">변경 세트 · Change Sets ({provenance.change_sets.length})</div>
                            <div className="space-y-1">
                              {provenance.change_sets.map((cs: any, i: number) => (
                                <div key={i} className="text-xs">
                                  <span className="text-green-600">+{cs.lines_added || 0}</span>{' '}
                                  <span className="text-red-600">-{cs.lines_removed || 0}</span>{' '}
                                  <span className="text-gray-500">{cs.files_changed}</span>{' '}
                                  <span className="badge-blue">{cs.attribution_state}</span>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                      </td>
                    </tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
