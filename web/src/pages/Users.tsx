import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Users() {
  const [users, setUsers] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ organization_id: '', email: '', name: '', name_ko: '', title: '' })

  const load = () => api.listUsers().then(data => setUsers(Array.isArray(data) ? data : data || []))
  useEffect(() => { load() }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    await api.createUser(form)
    setForm({ organization_id: '', email: '', name: '', name_ko: '', title: '' })
    setShowForm(false)
    load()
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">사용자 <span className="text-gray-400 text-lg font-normal">Users</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          {showForm ? '취소' : '+ 사용자 추가'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={handleCreate} className="card mb-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">이메일 (Email)</label>
              <input className="input" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} required />
            </div>
            <div>
              <label className="label">이름 (Name)</label>
              <input className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
            </div>
            <div>
              <label className="label">한글 이름 (Korean Name)</label>
              <input className="input" value={form.name_ko} onChange={(e) => setForm({ ...form, name_ko: e.target.value })} placeholder="김개발" />
            </div>
            <div>
              <label className="label">직함 (Title)</label>
              <input className="input" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="시니어 개발자" />
            </div>
          </div>
          <button type="submit" className="btn-primary">생성</button>
        </form>
      )}

      <div className="card">
        <table className="w-full">
          <thead>
            <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
              <th className="pb-3">이름 / Name</th>
              <th className="pb-3">이메일</th>
              <th className="pb-3">인증 방식</th>
              <th className="pb-3">상태</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id} className="border-b border-gray-100 last:border-0">
                <td className="py-3">
                  <div className="font-medium">{u.name_ko || u.name}</div>
                  <div className="text-xs text-gray-400">{u.name}</div>
                </td>
                <td className="py-3 text-sm">{u.email}</td>
                <td className="py-3"><span className="badge-gray">{u.auth_method}</span></td>
                <td className="py-3"><span className="badge-green">{u.status}</span></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
