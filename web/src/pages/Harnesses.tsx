import { useState, useEffect, Fragment } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'
import ConfirmDialog from '../components/ConfirmDialog'
import { formatRelative } from '../utils/format'
import { exportCSV } from '../utils/csv'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['harness_id', 'binary_version', 'device_id'],
  searchPlaceholder: '하네스 ID, 버전, 사용자로 검색...',
  dropdowns: [
    { key: 'status', label: '상태', options: [
      { value: 'enrolled', label: '등록됨' }, { value: 'active', label: '활성' },
      { value: 'quarantined', label: '격리됨' }, { value: 'revoked', label: '폐기됨' },
    ]},
    { key: 'risk_state', label: '위험도', options: [
      { value: 'normal', label: '정상' }, { value: 'elevated', label: '주의' }, { value: 'high', label: '높음' },
    ]},
  ],
}

export default function Harnesses() {
  const confirm = useConfirm()
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [org, setOrg] = useState<any>(null)
  const [revokeTarget, setRevokeTarget] = useState<string | null>(null)
  const [selectedHarnesses, setSelectedHarnesses] = useState<Set<string>>(new Set())
  const [showForm, setShowForm] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [form, setForm] = useState({ user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0' })

  const load = () => {
    api.listHarnesses().then(data => setHarnesses(Array.isArray(data) ? data : []))
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : []))
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : []))
    api.listOrganizations().then(data => setOrg(Array.isArray(data) && data[0] ? data[0] : null))
  }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(harnesses, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const getUser = (userId: string) => users.find(u => u.id === userId)
  const getHarnessUser = (h: any) => {
    try {
      const ids: string[] = JSON.parse(h.allowed_users || '[]')
      return users.find(u => ids.includes(u.id))
    } catch { return undefined }
  }
  const getProject = (projId: string) => projects.find(p => p.id === projId)
  const getHarnessSessions = (hrnId: string) => sessions.filter(s => s.harness_id === hrnId)
  const getActiveSessions = (hrnId: string) => getHarnessSessions(hrnId).filter(s => s.status === 'active')

  const handleEnroll = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = harnesses[0]?.organization_id || users[0]?.organization_id || ''
    try {
      await api.enrollHarness({ ...form, organization_id: orgId, enrollment_mode: 'sso', device_hostname: 'dev-machine', device_os: 'darwin', device_arch: 'arm64' })
      setShowForm(false); setForm({ user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0' }); load()
    } catch (err: any) { showToast('등록 실패: ' + err.message) }
  }
  const handleRevoke = async (id: string) => { if (await confirm({ title: '확인', message: '폐기하시겠습니까?', danger: true })) { try { await api.revokeHarness(id, 'manual revoke'); load() } catch {} } }
  const handleQuarantine = async (id: string) => { if (await confirm({ title: '확인', message: '격리하시겠습니까? 모든 활성 세션이 종료됩니다.', danger: true })) { try { await api.quarantineHarness(id); load() } catch {} } }
  const handleReactivate = async (id: string) => { try { await api.reactivateHarness(id); load() } catch {} }

  const statusBadge = (s: string) => { const m: Record<string,string> = { enrolled:'badge-green', active:'badge-green', pending:'badge-yellow', quarantined:'badge-red', revoked:'badge-gray' }; return m[s] || 'badge-gray' }
  const statusLabel = (s: string) => { const m: Record<string,string> = { enrolled:'등록됨', active:'활성', pending:'대기', quarantined:'격리됨', revoked:'폐기됨' }; return m[s] || s }
  const riskBadge = (s: string) => s === 'normal' ? 'badge-green' : s === 'elevated' ? 'badge-yellow' : 'badge-red'
  const formatTime = (ts: string) => { if (!ts || ts.startsWith('0001-01-01')) return '-'; return ts.slice(0, 16).replace('T', ' ') }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">하네스 <span className="text-gray-400 text-lg font-normal">Harnesses</span></h1>
          {org && <span className="text-xs text-gray-400 ml-4">{harnesses.filter(h => h.status !== 'revoked').length}/{org.max_harness_seats || '∞'} 좌석</span>}
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">{showForm ? '취소' : '+ 하네스 등록'}</button>
        <button onClick={() => exportCSV(`harnesses_${new Date().toISOString().slice(0,10)}.csv`, ['하네스 ID', '버전', '상태', '위험도', '사용자', '등록일'], harnesses.map(h => [h.harness_id, h.binary_version, h.status, h.risk_state, (users.find(u => { try { return JSON.parse(h.allowed_users || '[]').includes(u.id) } catch { return false } })?.name_ko) || '', h.enrolled_at?.slice(0,10)]))} className="btn-sm btn-secondary ml-2">📥 CSV</button>
      </div>

      {showForm && (
        <form onSubmit={handleEnroll} className="card mb-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div><label className="label">사용자 · User</label><select className="input" value={form.user_id} onChange={e => setForm({ ...form, user_id: e.target.value })} required><option value="">선택...</option>{users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>)}</select></div>
            <div><label className="label">하네스 ID</label><input className="input font-mono" value={form.harness_id} onChange={e => setForm({ ...form, harness_id: e.target.value })} placeholder="자동 생성 시 비워두기" /></div>
            <div><label className="label">공개키</label><input className="input font-mono" value={form.public_key_hex} onChange={e => setForm({ ...form, public_key_hex: e.target.value })} placeholder="ed25519 public key hex" /></div>
            <div><label className="label">바이너리 버전</label><input className="input" value={form.binary_version} onChange={e => setForm({ ...form, binary_version: e.target.value })} /></div>
          </div>
          <button type="submit" className="btn-primary">등록</button>
        </form>
      )}

      {selectedHarnesses.size > 0 && (
        <div className="flex items-center gap-3 mb-4 p-3 bg-blue-50 rounded-lg">
          <span className="text-sm font-medium text-blue-700">{selectedHarnesses.size}개 하네스 선택됨</span>
          <button onClick={async () => { for (const id of selectedHarnesses) { try { await api.quarantineHarness(id) } catch {} } setSelectedHarnesses(new Set()); load() }} className="btn-sm btn-secondary">일괄 격리</button>
          <button onClick={async () => { if (await confirm({ title: '확인', message: `${selectedHarnesses.size}개 하네스를 폐기하시겠습니까?`, danger: true })) { for (const id of selectedHarnesses) { api.revokeHarness(id, 'bulk revoke') } setSelectedHarnesses(new Set()); load() } }} className="btn-sm btn-danger">일괄 폐기</button>
          <button onClick={() => setSelectedHarnesses(new Set())} className="btn-sm btn-secondary">취소</button>
        </div>
      )}
      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        <table className="w-full overflow-x-auto block">
          <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
            <th className="pb-3 w-8"><input type="checkbox" onChange={(e) => { if (e.target.checked) setSelectedHarnesses(new Set(paged.map(h => h.id))); else setSelectedHarnesses(new Set()) }} /></th>
            <th className="pb-3">하네스 ID</th>
            <th className="pb-3">사용자 · User</th>
            <th className="pb-3">상태</th>
            <th className="pb-3">버전</th>
            <th className="pb-3">위험</th>
            <th className="pb-3">세션</th>
            <th className="pb-3">등록일</th>
            <th className="pb-3">작업</th>
          </tr></thead>
          <tbody>
            {paged.map(h => {
              const user = getHarnessUser(h)
              const activeSessions = getActiveSessions(h.harness_id)
              const allSessions = getHarnessSessions(h.harness_id)
              return (
                <Fragment key={h.id || h.key || i}>
                  <tr key={h.id} className={`border-b border-gray-100 last:border-0 cursor-pointer ${expandedId === h.id ? 'bg-blue-50' : 'hover:bg-gray-50'}`}
                    onClick={() => setExpandedId(expandedId === h.id ? null : h.id)}>
                    <td className="py-3" onClick={e => e.stopPropagation()}><input type="checkbox" checked={selectedHarnesses.has(h.id)} onChange={() => { const next = new Set(selectedHarnesses); if (next.has(h.id)) next.delete(h.id); else next.add(h.id); setSelectedHarnesses(next) }} /></td>
                    <td className="py-3 font-mono text-xs"><Link to={`/harnesses/${h.id}`} className="text-blue-600 hover:underline">{h.harness_id?.slice(0, 20)}</Link></td>
                    {/* USER COLUMN — clickable link to user detail */}
                    <td className="py-3">
                      {user ? (
                        <Link to={`/users/${user.id}`} className="text-sm font-medium text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>
                          {user.name_ko || user.name}
                        </Link>
                      ) : <span className="text-xs text-gray-400">-</span>}
                      {user?.email && <div className="text-xs text-gray-400">{user.email}</div>}
                    </td>
                    <td className="py-3"><span className={statusBadge(h.status)}>{statusLabel(h.status)}</span></td>
                    <td className="py-3 text-xs text-gray-500">v{h.binary_version}</td>
                    <td className="py-3"><span className={riskBadge(h.risk_state)}>{h.risk_state}</span></td>
                    {/* SESSIONS — clickable link to sessions page */}
                    <td className="py-3">
                      <Link to="/sessions" className="text-sm text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>
                        {activeSessions.length} 활성
                      </Link>
                    </td>
                    <td className="py-3 text-xs text-gray-400">{formatRelative(h.enrolled_at)}</td>
                    <td className="py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex gap-1 flex-wrap">
                        {(h.status === 'active' || h.status === 'enrolled') && <button onClick={() => handleQuarantine(h.id)} className="text-xs text-yellow-600 hover:underline">격리</button>}
                        {(h.status === 'active' || h.status === 'enrolled') && <button onClick={() => setRevokeTarget(h.id)} className="text-xs text-red-600 hover:underline">폐기</button>}
                        {h.status === 'quarantined' && <button onClick={() => handleReactivate(h.id)} className="text-xs text-green-600 hover:underline">재활성화</button>}
                        {(h.status === 'revoked' || h.status === 'quarantined') && <Link to="/fleet" className="text-xs text-blue-600 hover:underline">플릿 관리 →</Link>}
                      </div>
                    </td>
                  </tr>
                  {/* EXPANDED DETAIL */}
                  {expandedId === h.id && (
                    <tr className="bg-gray-50"><td colSpan={9} className="p-4">
                      <div className="grid grid-cols-4 gap-6">
                        {/* Harness info */}
                        <div>
                          <div className="text-xs font-semibold text-gray-600 mb-2">하네스 정보</div>
                          <div className="space-y-1 text-xs text-gray-500">
                            <div>하네스 ID: <span className="font-mono">{h.harness_id?.slice(0, 30)}</span></div>
                            <div>빌드 해시: <span className="font-mono">{h.build_hash?.slice(0, 16) || '-'}</span></div>
                            <div>릴리스 채널: {h.release_channel || 'stable'}</div>
                            <div>등록일: {formatRelative(h.enrolled_at)}</div>
                            <div>등록 모드: {h.enrollment_mode || 'sso'}</div>
                          </div>
                        </div>
                        {/* User info */}
                        <div>
                          <div className="text-xs font-semibold text-gray-600 mb-2">사용자</div>
                          {user ? (
                            <div className="space-y-1 text-xs">
                              <div><Link to={`/users/${user.id}`} className="text-blue-600 hover:underline font-medium">{user.name_ko || user.name}</Link></div>
                              <div className="text-gray-400">{user.email}</div>
                              {user.employee_id && <div className="text-gray-400">사번: {user.employee_id}</div>}
                              {user.business_unit_id && <div className="text-gray-400">부서: {user.business_unit_id}</div>}
                              {user.title_ko && <div className="text-gray-400">직책: {user.title_ko}</div>}
                            </div>
                          ) : <span className="text-xs text-gray-400">사용자 정보 없음</span>}
                        </div>
                        {/* Sessions */}
                        <div>
                          <div className="text-xs font-semibold text-gray-600 mb-2">세션 ({allSessions.length})</div>
                          {allSessions.length === 0 ? <span className="text-xs text-gray-400">세션 없음</span> : (
                            <div className="space-y-1">
                              {allSessions.slice(0, 5).map(s => (
                                <div key={s.id} className="text-xs">
                                  <Link to="/sessions" className="text-blue-600 hover:underline">{s.title || '제목 없음'}</Link>
                                  <span className={`ml-1 ${s.status === 'active' ? 'text-green-600' : 'text-gray-400'}`}>{s.status}</span>
                                  {s.model_class && <span className="text-gray-400 ml-1">· {s.model_class}</span>}
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                        {/* Risk + actions */}
                        <div>
                          <div className="text-xs font-semibold text-gray-600 mb-2">보안</div>
                          <div className="space-y-1 text-xs text-gray-500">
                            <div>위험 상태: <span className={riskBadge(h.risk_state)}>{h.risk_state}</span></div>
                            <div>인증 방식: {h.enrollment_mode || 'sso'}</div>
                          </div>
                          <div className="mt-3">
                            <Link to="/fleet" className="text-xs text-blue-600 hover:underline">플릿에서 관리 →</Link><br />
                            <Link to="/audit" className="text-xs text-blue-600 hover:underline">감사 로그 보기 →</Link>
                          </div>
                        </div>
                      </div>
                    </td></tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </table>
        <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
      </div>

      <ConfirmDialog
        open={!!revokeTarget}
        title="하네스 폐기 · Revoke Harness"
        message="이 하네스를 폐기하시겠습니까? 모든 활성 세션이 종료됩니다."
        confirmLabel="폐기 실행"
        danger
        onConfirm={async () => { if (revokeTarget) { try { await api.revokeHarness(revokeTarget, 'manual revoke'); load() } catch {} } setRevokeTarget(null) }}
        onCancel={() => setRevokeTarget(null)}
      />
    </div>
  )
}