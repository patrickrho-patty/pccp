import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Projects() {
  const [projects, setProjects] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [form, setForm] = useState({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard' })

  const [sessions, setSessions] = useState<any[]>([])
  const load = () => {
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : []))
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : []))
  }
  useEffect(() => { load() }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createProject({
        ...form,
        allowed_models: form.allowed_models.split(',').map(s => s.trim()),
      })
      setForm({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard' })
      setShowForm(false)
      load()
    } catch (err: any) { alert('생성 실패: ' + err.message) }
  }

  const handleEdit = (proj: any) => {
    setEditingId(proj.id)
    setForm({
      name: proj.name || '', name_ko: proj.name_ko || '', slug: proj.slug || '',
      allowed_models: proj.allowed_model_classes || 'patty-code-standard',
    })
    setShowForm(true)
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingId) return
    try {
      await api.updateProject(editingId, { name: form.name, name_ko: form.name_ko })
      setEditingId(null)
      setForm({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard' })
      setShowForm(false)
      load()
    } catch (err: any) { alert('수정 실패: ' + err.message) }
  }

  const getSessionCount = (projId: string) => sessions.filter(s => s.project_id === projId && s.status === 'active').length

  const handleArchive = async (id: string) => {
    if (!confirm('이 프로젝트를 보관 처리하시겠습니까?')) return
    try { await api.deleteProject(id); load() } catch {}
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">프로젝트 <span className="text-gray-400 text-lg font-normal">Projects</span></h1>
        <button onClick={() => {
          if (editingId) { setEditingId(null); setForm({ name: '', name_ko: '', slug: '', allowed_models: 'patty-code-standard' }) }
          setShowForm(!showForm)
        }} className="btn-primary">
          {showForm ? '취소' : '+ 프로젝트 생성'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold">{editingId ? '프로젝트 수정 · Edit Project' : '새 프로젝트 · New Project'}</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">이름 · Name</label>
              <input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} required />
            </div>
            <div>
              <label className="label">한글 이름</label>
              <input className="input" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} placeholder="결제 서비스" />
            </div>
            {!editingId && (
              <>
                <div>
                  <label className="label">슬러그 · Slug</label>
                  <input className="input" value={form.slug} onChange={e => setForm({ ...form, slug: e.target.value })} placeholder="payment-service" />
                </div>
                <div>
                  <label className="label">허용 모델 (쉼표 구분)</label>
                  <input className="input" value={form.allowed_models} onChange={e => setForm({ ...form, allowed_models: e.target.value })} />
                </div>
              </>
            )}
          </div>
          <button type="submit" className="btn-primary">{editingId ? '수정 · Save' : '생성 · Create'}</button>
        </form>
      )}

      <div className="grid grid-cols-3 gap-4">
        {projects.length === 0 ? (
          <div className="col-span-3 card text-center py-8 text-gray-400">프로젝트가 없습니다</div>
        ) : projects.map(p => (
          <div key={p.id} className="card cursor-pointer hover:border-blue-300 transition-colors"
               onClick={() => setExpandedId(expandedId === p.id ? null : p.id)}>
            <div className="flex items-start justify-between mb-2">
              <div>
                <h3 className="font-semibold">{p.name_ko || p.name}</h3>
                <p className="text-sm text-gray-500">{p.name}</p>
              </div>
              <span className={p.status === 'active' ? 'badge-green' : 'badge-gray'}>{p.status}</span>
            </div>
            {p.slug && <p className="text-xs text-gray-400 font-mono mb-2">{p.slug}</p>}
            <div className="flex gap-3 text-xs text-gray-500">
              <span>활성 세션: <strong className="text-gray-700">{getSessionCount(p.id)}</strong></span>
            </div>

            {expandedId === p.id && (
              <div className="mt-3 pt-3 border-t border-gray-100 space-y-2">
                {p.allowed_model_classes && (
                  <div className="text-xs">
                    <span className="text-gray-500">허용 모델: </span>
                    {p.allowed_model_classes?.split(',').map((m: string) => (
                      <span key={m} className="badge-blue mr-1">{m.trim()}</span>
                    ))}
                  </div>
                )}
                {p.description && <p className="text-xs text-gray-500">{p.description}</p>}
                <div className="flex gap-4 text-xs text-gray-400 mt-2">
                  <span>생성: {p.created_at?.slice(0, 10)}</span>
                  <span>상태: {p.status}</span>
                </div>
                <div className="flex gap-2 mt-2">
                  <button onClick={(e) => { e.stopPropagation(); handleEdit(p) }} className="text-blue-600 text-xs hover:underline">수정</button>
                  {p.status === 'active' && (
                    <button onClick={(e) => { e.stopPropagation(); handleArchive(p.id) }} className="text-red-600 text-xs hover:underline">보관</button>
                  )}
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
