import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Projects() {
  const [projects, setProjects] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ name: '', name_ko: '', slug: '', allowed_models: 'pmp_kocoder_v1' })

  useEffect(() => { api.listProjects().then(setProjects) }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    await api.createProject({ ...form, allowed_models: form.allowed_models.split(',').map(s => s.trim()) })
    setShowForm(false)
    api.listProjects().then(setProjects)
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">프로젝트 <span className="text-gray-400 text-lg font-normal">Projects</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          {showForm ? '취소' : '+ 프로젝트 생성'}
        </button>
      </div>
      {showForm && (
        <form onSubmit={handleCreate} className="card mb-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">이름 (Name)</label>
              <input className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
            </div>
            <div>
              <label className="label">한글 이름</label>
              <input className="input" value={form.name_ko} onChange={(e) => setForm({ ...form, name_ko: e.target.value })} placeholder="결제 서비스" />
            </div>
            <div>
              <label className="label">슬러그 (Slug)</label>
              <input className="input" value={form.slug} onChange={(e) => setForm({ ...form, slug: e.target.value })} placeholder="payment-service" />
            </div>
            <div>
              <label className="label">허용 모델 (쉼표 구분)</label>
              <input className="input" value={form.allowed_models} onChange={(e) => setForm({ ...form, allowed_models: e.target.value })} />
            </div>
          </div>
          <button type="submit" className="btn-primary">생성</button>
        </form>
      )}
      <div className="grid grid-cols-3 gap-4">
        {projects.map((p) => (
          <div key={p.id} className="card">
            <h3 className="font-semibold">{p.name_ko || p.name}</h3>
            <p className="text-sm text-gray-500 mt-1">{p.name}</p>
            <div className="mt-3 flex items-center gap-2">
              <span className="badge-green">{p.status}</span>
              {p.slug && <span className="badge-gray">{p.slug}</span>}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
