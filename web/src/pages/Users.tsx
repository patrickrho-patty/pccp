import { useState, useEffect } from 'react'
import { api } from '../api'

const AUTH_METHODS = [
  { value: 'local', label: 'Local (로컬)' },
  { value: 'oidc', label: 'OIDC' },
  { value: 'saml', label: 'SAML 2.0' },
  { value: 'ldap', label: 'LDAP / AD' },
  { value: 'scim', label: 'SCIM' },
]

export default function Users() {
  const [users, setUsers] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [form, setForm] = useState({
    email: '', name: '', name_ko: '', title: '', auth_method: 'local',
  })

  const load = () => api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
  useEffect(() => { load() }, [])

  const filtered = users.filter(u => {
    if (!searchQuery) return true
    const q = searchQuery.toLowerCase()
    return (u.name_ko || '').toLowerCase().includes(q) ||
      (u.name || '').toLowerCase().includes(q) ||
      (u.email || '').toLowerCase().includes(q) ||
      (u.auth_method || '').toLowerCase().includes(q)
  })

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createUser(form)
      setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local' })
      setShowForm(false)
      load()
    } catch (err: any) { alert('생성 실패: ' + err.message) }
  }

  const handleEdit = (user: any) => {
    setEditingId(user.id)
    setForm({
      email: user.email || '', name: user.name || '', name_ko: user.name_ko || '',
      title: user.title || '', auth_method: user.auth_method || 'local',
    })
    setShowForm(true)
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingId) return
    try {
      await api.updateUser(editingId, form)
      setEditingId(null)
      setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local' })
      setShowForm(false)
      load()
    } catch (err: any) { alert('수정 실패: ' + err.message) }
  }

  const handleStatusChange = async (user: any, newStatus: string) => {
    try {
      await api.updateUser(user.id, { status: newStatus })
      load()
    } catch (err: any) { alert('상태 변경 실패: ' + err.message) }
  }

  const handleDelete = async (user: any) => {
    if (!confirm(`${user.name_ko || user.name}을(를) 퇴사 처리하시겠습니까?`)) return
    try {
      await api.deleteUser(user.id)
      load()
    } catch (err: any) { alert('삭제 실패: ' + err.message) }
  }

  const statusBadge = (status: string) => {
    const map: Record<string, string> = {
      active: 'badge-green', suspended: 'badge-yellow', offboarded: 'badge-gray',
    }
    return map[status] || 'badge-gray'
  }

  const statusLabel = (status: string) => {
    const map: Record<string, string> = {
      active: '활성', suspended: '정지', offboarded: '퇴사',
    }
    return map[status] || status
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">사용자 <span className="text-gray-400 text-lg font-normal">Users</span></h1>
        <button onClick={() => {
          if (editingId) {
            setEditingId(null)
            setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local' })
          }
          setShowForm(!showForm)
        }} className="btn-primary">
          {showForm ? '취소' : '+ 사용자 추가'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold text-gray-700">
            {editingId ? '사용자 수정 · Edit User' : '새 사용자 · New User'}
          </h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">이메일 · Email</label>
              <input className="input" value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} required />
            </div>
            <div>
              <label className="label">이름 · Name</label>
              <input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} required />
            </div>
            <div>
              <label className="label">한글 이름 · Korean Name</label>
              <input className="input" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} placeholder="김개발" />
            </div>
            <div>
              <label className="label">직함 · Title</label>
              <input className="input" value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} placeholder="시니어 개발자" />
            </div>
            <div>
              <label className="label">인증 방식 · Auth Method</label>
              <select className="input" value={form.auth_method} onChange={e => setForm({ ...form, auth_method: e.target.value })}>
                {AUTH_METHODS.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
              </select>
            </div>
          </div>
          <button type="submit" className="btn-primary">
            {editingId ? '수정 · Save' : '생성 · Create'}
          </button>
        </form>
      )}

      <div className="flex gap-3 mb-4">
        <input
          className="input flex-1"
          placeholder="이름, 이메일, 인증 방식으로 검색 · Search by name, email, auth method..."
          value={searchQuery}
          onChange={e => setSearchQuery(e.target.value)}
        />
        <span className="text-sm text-gray-500 self-center">{filtered.length}명</span>
      </div>

      <div className="card">
        <table className="w-full">
          <thead>
            <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
              <th className="pb-3">이름 · Name</th>
              <th className="pb-3">이메일</th>
              <th className="pb-3">직함</th>
              <th className="pb-3">인증</th>
              <th className="pb-3">상태</th>
              <th className="pb-3 text-right">작업</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 ? (
              <tr><td colSpan={6} className="py-8 text-center text-gray-400">
                {searchQuery ? '검색 결과가 없습니다' : '등록된 사용자가 없습니다'}
              </td></tr>
            ) : filtered.map((u) => (
              <tr key={u.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30">
                <td className="py-3">
                  <div className="font-medium text-sm">{u.name_ko || u.name}</div>
                  <div className="text-xs text-gray-400">{u.name}</div>
                </td>
                <td className="py-3 text-sm">{u.email}</td>
                <td className="py-3 text-sm text-gray-600">{u.title || '-'}</td>
                <td className="py-3"><span className="badge-gray">{u.auth_method}</span></td>
                <td className="py-3"><span className={statusBadge(u.status)}>{statusLabel(u.status)}</span></td>
                <td className="py-3">
                  <div className="flex gap-2 justify-end">
                    <button onClick={() => handleEdit(u)} className="text-blue-600 text-xs hover:underline">수정</button>
                    {u.status === 'active' && (
                      <button onClick={() => handleStatusChange(u, 'suspended')} className="text-yellow-600 text-xs hover:underline">정지</button>
                    )}
                    {u.status === 'suspended' && (
                      <button onClick={() => handleStatusChange(u, 'active')} className="text-green-600 text-xs hover:underline">활성화</button>
                    )}
                    {u.status !== 'offboarded' && (
                      <button onClick={() => handleDelete(u)} className="text-red-600 text-xs hover:underline">퇴사</button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
