import { useState, useEffect } from 'react'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['name', 'name_ko', 'email', 'auth_method', 'title'],
  searchPlaceholder: '이름, 이메일, 인증 방식, 직함으로 검색...',
  dropdowns: [
    {
      key: 'auth_method',
      label: '인증',
      options: [
        { value: 'local', label: 'Local' },
        { value: 'oidc', label: 'OIDC' },
        { value: 'saml', label: 'SAML' },
        { value: 'ldap', label: 'LDAP' },
        { value: 'scim', label: 'SCIM' },
      ],
    },
    {
      key: 'status',
      label: '상태',
      options: [
        { value: 'active', label: '활성' },
        { value: 'suspended', label: '정지' },
        { value: 'offboarded', label: '퇴사' },
      ],
    },
  ],
}

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
  const [filters, setFilters] = useState({
    search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string>,
  })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [form, setForm] = useState({
    email: '', name: '', name_ko: '', title: '', auth_method: 'local',
  })

  const load = () => api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(users, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

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
    try { await api.updateUser(user.id, { status: newStatus }); load() }
    catch (err: any) { alert('상태 변경 실패: ' + err.message) }
  }

  const handleDelete = async (user: any) => {
    if (!confirm(`${user.name_ko || user.name}을(를) 퇴사 처리하시겠습니까?`)) return
    try { await api.deleteUser(user.id); load() }
    catch (err: any) { alert('삭제 실패: ' + err.message) }
  }

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { active: 'badge-green', suspended: 'badge-yellow', offboarded: 'badge-gray' }
    return map[s] || 'badge-gray'
  }
  const statusLabel = (s: string) => {
    const map: Record<string, string> = { active: '활성', suspended: '정지', offboarded: '퇴사' }
    return map[s] || s
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">사용자 <span className="text-gray-400 text-lg font-normal">Users</span></h1>
        <button onClick={() => {
          if (editingId) { setEditingId(null); setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local' }) }
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
          <button type="submit" className="btn-primary">{editingId ? '수정 · Save' : '생성 · Create'}</button>
        </form>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

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
            {paged.length === 0 ? (
              <tr><td colSpan={7} className="py-8 text-center text-gray-400">
                {filters.search ? '검색 결과가 없습니다' : '등록된 사용자가 없습니다'}
              </td></tr>
            ) : paged.map(u => (
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
                    {u.status === 'active' && <button onClick={() => handleStatusChange(u, 'suspended')} className="text-yellow-600 text-xs hover:underline">정지</button>}
                    {u.status === 'suspended' && <button onClick={() => handleStatusChange(u, 'active')} className="text-green-600 text-xs hover:underline">활성화</button>}
                    {u.status !== 'offboarded' && <button onClick={() => handleDelete(u)} className="text-red-600 text-xs hover:underline">퇴사</button>}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
    </div>
  )
}
