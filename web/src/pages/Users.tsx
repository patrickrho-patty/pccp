import { useState, useEffect, Fragment } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'
import ConfirmDialog from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import { exportCSV } from '../utils/csv'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'
import { useRowNav } from '../hooks/useRowNav'

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
  const confirm = useConfirm()
  const [users, setUsers] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [filters, setFilters] = useState({
    search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string>,
  })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [sessions, setSessions] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [businessUnits, setBusinessUnits] = useState<any[]>([])
  const [org, setOrg] = useState<any>(null)
  const [expandedUserId, setExpandedUserId] = useState<string | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [offboardTarget, setOffboardTarget] = useState<any>(null)
  const [form, setForm] = useState({
    email: '', name: '', name_ko: '', title: '', auth_method: 'local', business_unit_id: '',
  })

  const load = () => {
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : []))
    api.listHarnesses().then(data => setHarnesses(Array.isArray(data) ? data : []))
    api.listBusinessUnits().then(data => setBusinessUnits(Array.isArray(data) ? data : []))
    api.listOrganizations().then(data => setOrg(Array.isArray(data) && data[0] ? data[0] : null))
  }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(users, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)
  const { selectedIndex } = useRowNav(paged.length, (i) => setExpandedUserId(expandedUserId === paged[i].id ? null : paged[i].id))

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createUser({ ...form, business_unit_id: form.business_unit_id })
      setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local', business_unit_id: '' })
      setShowForm(false)
      showToast('사용자 생성됨', 'success')
      load()
    } catch (err: any) { showToast('생성 실패: ' + err.message) }
  }

  const handleEdit = (user: any) => {
    setEditingId(user.id)
    setForm({
      email: user.email || '', name: user.name || '', name_ko: user.name_ko || '',
      title: user.title || '', auth_method: user.auth_method || 'local',
      business_unit_id: user.business_unit_id || '',
    })
    setShowForm(true)
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingId) return
    try {
      await api.updateUser(editingId, { ...form, business_unit_id: form.business_unit_id })
      setEditingId(null)
      setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local', business_unit_id: '' })
      setShowForm(false)
      showToast('수정 완료', 'success')
      load()
    } catch (err: any) { showToast('수정 실패: ' + err.message) }
  }

  const handleStatusChange = async (user: any, newStatus: string) => {
    try { await api.updateUser(user.id, { status: newStatus }); load() }
    catch (err: any) { showToast('상태 변경 실패: ' + err.message) }
  }

  const toggleSelect = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handleBulkSuspend = async () => {
    if (!await confirm({ title: '확인', message: selectedIds.size + '명을 정지하시겠습니까?', danger: true })) return
    for (const id of selectedIds) {
      try { await api.updateUser(id, { status: 'suspended' }) } catch {}
    }
    setSelectedIds(new Set())
    load()
  }

  const handleBulkOffboard = async () => {
    if (!await confirm({ title: '확인', message: selectedIds.size + '명을 퇴사 처리하시겠습니까?', danger: true })) return
    for (const id of selectedIds) {
      try { await api.deleteUser(id) } catch {}
    }
    setSelectedIds(new Set())
    load()
  }

  const handleDelete = async (user: any) => {
    if (!await confirm({ title: '퇴사 처리', message: `${user.name_ko || user.name}을(를) 퇴사 처리하시겠습니까?`, danger: true })) return
    try { await api.deleteUser(user.id); load() }
    catch (err: any) { showToast('삭제 실패: ' + err.message) }
  }

  const getUserSessions = (userId: string) => sessions.filter(s => s.user_id === userId)
  const getUserHarnesses = (userId: string) => {
    // Harnesses don't have user_id directly, but we can match via sessions
    const userSessionHarnessIds = sessions.filter(s => s.user_id === userId).map(s => s.harness_id)
    return harnesses.filter(h => userSessionHarnessIds.includes(h.harness_id))
  }
  const formatRelative = (ts: string) => {
    if (!ts) return '-'
    const d = new Date(ts)
    const diff = Date.now() - d.getTime()
    const mins = Math.floor(diff / 60000)
    if (mins < 1) return '방금 전'
    if (mins < 60) return mins + '분 전'
    const hours = Math.floor(mins / 60)
    if (hours < 24) return hours + '시간 전'
    return d.toLocaleDateString('ko-KR')
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
          {org && <span className="text-xs text-gray-400 ml-4">{users.filter(u => u.status !== 'offboarded').length}/{org.max_user_seats || '∞'} 좌석</span>}
        <button onClick={() => {
          if (editingId) { setEditingId(null); setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local', business_unit_id: '' }) }
          setShowForm(!showForm)
        }} className="btn-primary">
          {showForm ? '취소' : '+ 사용자 추가'}
        </button>
        <button onClick={() => exportCSV(`users_${new Date().toISOString().slice(0,10)}.csv`, ['이메일', '이름', '한글명', '직함', '인증방식', '부서', '상태', '등록일'], users.map(u => [u.email, u.name, u.name_ko, u.title, u.auth_method, u.business_unit_id, u.status, u.created_at?.slice(0,10)]))} className="btn-sm btn-secondary ml-2">📥 CSV 내보내기</button>
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
            <div>
              <label className="label">부서 · Department</label>
              <select className="input" value={form.business_unit_id} onChange={e => setForm({ ...form, business_unit_id: e.target.value })}>
                <option value="">선택 안함</option>
                {businessUnits.length > 0
                  ? businessUnits.map(bu => <option key={bu.id} value={bu.id}>{bu.name_ko || bu.name}</option>)
                  : <>
                    <option value="dev">개발팀 · Development</option>
                    <option value="qa">QA팀 · Quality Assurance</option>
                    <option value="devops">데브옵스팀 · DevOps</option>
                    <option value="security">보안팀 · Security</option>
                    <option value="data">데이터팀 · Data</option>
                    <option value="infra">인프라팀 · Infrastructure</option>
                    <option value="product">프로덕트팀 · Product</option>
                    <option value="exec">경영진 · Executive</option>
                  </>}
              </select>
            </div>
          </div>
          <button type="submit" className="btn-primary">{editingId ? '수정 · Save' : '생성 · Create'}</button>
        </form>
      )}

      {selectedIds.size > 0 && (
        <div className="flex items-center gap-3 mb-4 p-3 bg-blue-50 rounded-lg">
          <span className="text-sm font-medium text-blue-700">{selectedIds.size}명 선택됨</span>
          <button onClick={handleBulkSuspend} className="btn-sm btn-secondary">일괄 정지</button>
          <button onClick={handleBulkOffboard} className="btn-sm btn-danger">일괄 퇴사</button>
          <button onClick={() => setSelectedIds(new Set())} className="btn-sm btn-secondary">취소</button>
        </div>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        <table className="w-full overflow-x-auto block">
          <thead>
            <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
              <th className="pb-3 w-8"><input type="checkbox" onChange={(e) => { if (e.target.checked) setSelectedIds(new Set(paged.map(u => u.id))); else setSelectedIds(new Set()) }} /></th><th className="pb-3">이름 · Name</th>
              <th className="pb-3">이메일</th>
              <th className="pb-3">직함</th>
              <th className="pb-3">인증</th>
              <th className="pb-3">상태</th>
              <th className="pb-3 text-right">작업</th>
            </tr>
          </thead>
          <tbody>
            {paged.length === 0 ? (
              <tr><td colSpan={9} className="py-8">
                <EmptyState
                  icon={filters.search ? '🔍' : '👥'}
                  title={filters.search ? '검색 결과가 없습니다' : '등록된 사용자가 없습니다'}
                  message={filters.search ? '다른 검색어로 시도해보세요' : '+ 사용자 추가 버튼으로 첫 사용자를 등록하세요'}
                />
              </td></tr>
            ) : paged.map(u => (
              <Fragment key={u.id}>
                <tr key={u.id} className={`border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer ${selectedIndex === paged.indexOf(u) ? 'bg-blue-50 ring-1 ring-blue-300' : ''}`} onClick={() => setExpandedUserId(expandedUserId === u.id ? null : u.id)}>
                <td className="py-3" onClick={e => e.stopPropagation()}>
                  <Link to={`/users/${u.id}`} className="font-medium text-sm text-blue-600 hover:underline">{u.name_ko || u.name}</Link>
                  <div className="text-xs text-gray-400">{u.name}</div>
                </td>
                <td className="py-3 text-sm">{u.email}</td>
                <td className="py-3 text-sm text-gray-600">{u.title || '-'}</td>
                <td className="py-3" onClick={e => e.stopPropagation()}><span className="badge-gray">{u.auth_method}</span></td>
                <td className="py-3" onClick={e => e.stopPropagation()}><span className={statusBadge(u.status)}>{statusLabel(u.status)}</span></td>
                <td className="py-3" onClick={e => e.stopPropagation()}>
                  <div className="flex gap-2 justify-end">
                    <button onClick={() => handleEdit(u)} className="text-blue-600 text-xs hover:underline">수정</button>
                    {u.status === 'active' && <button onClick={() => handleStatusChange(u, 'suspended')} className="text-yellow-600 text-xs hover:underline">정지</button>}
                    {u.status === 'suspended' && <button onClick={() => handleStatusChange(u, 'active')} className="text-green-600 text-xs hover:underline">활성화</button>}
                    {u.status !== 'offboarded' && <button onClick={() => setOffboardTarget(u)} className="text-red-600 text-xs hover:underline">퇴사</button>}
                  </div>
                </td>
                </tr>
                {expandedUserId === u.id && (
                    <tr className="bg-gray-50">
                      <td colSpan={9} className="p-4">
                        <div className="grid grid-cols-3 gap-6">
                          {/* Sessions */}
                          <div>
                            <div className="text-xs font-semibold text-gray-600 mb-2">세션 이력 ({getUserSessions(u.id).length})</div>
                            {getUserSessions(u.id).length === 0 ? (
                              <p className="text-xs text-gray-400">세션 없음</p>
                            ) : (
                              <div className="space-y-1">
                                {getUserSessions(u.id).slice(0, 5).map(s => (
                                  <div key={s.id} className="text-xs">
                                    <span className="font-medium">{s.title || '제목 없음'}</span>
                                    <span className="text-gray-400 ml-2">{s.model_class}</span>
                                    <span className={`ml-2 ${s.status === 'active' ? 'text-green-600' : 'text-gray-400'}`}>{s.status}</span>
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                          {/* Harnesses */}
                          <div>
                            <div className="text-xs font-semibold text-gray-600 mb-2">하네스 ({getUserHarnesses(u.id).length})</div>
                            {getUserHarnesses(u.id).length === 0 ? (
                              <p className="text-xs text-gray-400">하네스 없음</p>
                            ) : (
                              <div className="space-y-1">
                                {getUserHarnesses(u.id).map(h => (
                                  <div key={h.id} className="text-xs">
                                    <span className="font-mono">{h.harness_id}</span>
                                    <span className={`ml-2 ${h.status === 'active' || h.status === 'enrolled' ? 'text-green-600' : 'text-gray-400'}`}>{h.status}</span>
                                    <span className="text-gray-400 ml-1">v{h.binary_version}</span>
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                          {/* Quick Info */}
                          <div>
                            <div className="text-xs font-semibold text-gray-600 mb-2">사용자 정보</div>
                            <div className="space-y-1 text-xs text-gray-500">
                              <div>인증: <span className="font-medium text-gray-700">{u.auth_method}</span></div>
                              <div>로케일: {u.locale || 'ko-KR'}</div>
                              <div>타임존: {u.timezone || 'Asia/Seoul'}</div>
                              <div>등록: {u.created_at?.slice(0, 10)}</div>
                              {u.last_login_at && <div>마지막 로그인: {formatRelative(u.last_login_at)}</div>}
                            </div>
                          </div>
                        </div>
                      </td>
                    </tr>
                  )}
              </Fragment>
                ))}
          </tbody>
        </table>
      </div>

      <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />

      <ConfirmDialog
        open={!!offboardTarget}
        title="퇴사 처리 · Offboard User"
        message={offboardTarget ? `${offboardTarget.name_ko || offboardTarget.name}을(를) 퇴사 처리하시겠습니까? 활성 세션이 종료되고 하네스가 폐기됩니다.` : ''}
        confirmLabel="퇴사 실행"
        danger
        onConfirm={async () => { if (offboardTarget) { try { await api.deleteUser(offboardTarget.id); load() } catch {} } setOffboardTarget(null) }}
        onCancel={() => setOffboardTarget(null)}
      />
    </div>
  )
}