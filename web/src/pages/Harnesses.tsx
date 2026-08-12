import { useState, useEffect } from 'react'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['harness_id', 'binary_version', 'device_id'],
  searchPlaceholder: '하네스 ID, 버전으로 검색...',
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
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [form, setForm] = useState({ user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0' })

  const load = () => { api.listHarnesses().then(data => setHarnesses(Array.isArray(data) ? data : [])); api.listUsers().then(data => setUsers(Array.isArray(data) ? data : [])) }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(harnesses, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const handleEnroll = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = harnesses[0]?.organization_id || users[0]?.organization_id || ''
    try {
      await api.enrollHarness({ ...form, organization_id: orgId, enrollment_mode: 'sso', device_hostname: 'dev-machine', device_os: 'darwin', device_arch: 'arm64' })
      setShowForm(false); setForm({ user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0' }); load()
    } catch (err: any) { alert('등록 실패: ' + err.message) }
  }
  const handleRevoke = async (id: string) => { if (confirm('폐기하시겠습니까?')) { try { await api.revokeHarness(id, 'manual revoke'); load() } catch {} } }
  const handleQuarantine = async (id: string) => { if (confirm('격리하시겠습니까? 모든 활성 세션이 종료됩니다.')) { try { await api.quarantineHarness(id); load() } catch {} } }
  const handleReactivate = async (id: string) => { try { await api.reactivateHarness(id); load() } catch {} }

  const statusBadge = (s: string) => { const m: Record<string,string> = { enrolled:'badge-green', active:'badge-green', pending:'badge-yellow', quarantined:'badge-red', revoked:'badge-gray' }; return m[s] || 'badge-gray' }
  const statusLabel = (s: string) => { const m: Record<string,string> = { enrolled:'등록됨', active:'활성', pending:'대기', quarantined:'격리됨', revoked:'폐기됨' }; return m[s] || s }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">하네스 <span className="text-gray-400 text-lg font-normal">Harnesses</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">{showForm ? '취소' : '+ 하네스 등록'}</button>
      </div>

      {showForm && (
        <form onSubmit={handleEnroll} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold">하네스 등록 · Enroll Harness</h2>
          <div className="grid grid-cols-2 gap-4">
            <div><label className="label">사용자 · User</label><select className="input" value={form.user_id} onChange={e => setForm({ ...form, user_id: e.target.value })} required><option value="">선택...</option>{users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>)}</select></div>
            <div><label className="label">하네스 ID · Harness Peer ID</label><input className="input font-mono text-xs" value={form.harness_id} onChange={e => setForm({ ...form, harness_id: e.target.value })} placeholder="hrn_xxx" required /></div>
            <div><label className="label">공개키 · Ed25519 Hex</label><input className="input font-mono text-xs" value={form.public_key_hex} onChange={e => setForm({ ...form, public_key_hex: e.target.value })} placeholder="a1b2c3..." required /></div>
            <div><label className="label">버전 · Binary Version</label><input className="input" value={form.binary_version} onChange={e => setForm({ ...form, binary_version: e.target.value })} /></div>
          </div>
          <button type="submit" className="btn-primary">등록 · Enroll</button>
        </form>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        {paged.length === 0 ? (
          <p className="text-gray-400 text-center py-8">{filters.search ? '검색 결과가 없습니다' : '등록된 하네스가 없습니다'}</p>
        ) : (
          <table className="w-full">
            <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
              <th className="pb-3">하네스 ID</th><th className="pb-3">버전</th><th className="pb-3">등록 모드</th><th className="pb-3">위험도</th><th className="pb-3">상태</th><th className="pb-3 text-right">작업</th>
            </tr></thead>
            <tbody>
              {paged.map(h => (
                <>
                  <tr key={h.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer" onClick={() => setExpandedId(expandedId === h.id ? null : h.id)}>
                    <td className="py-3 font-mono text-xs">{h.harness_id}</td>
                    <td className="py-3 text-sm">{h.binary_version}</td>
                    <td className="py-3"><span className="badge-gray">{h.enrollment_mode}</span></td>
                    <td className="py-3"><span className={h.risk_state === 'normal' ? 'badge-green' : h.risk_state === 'high' ? 'badge-red' : 'badge-yellow'}>{h.risk_state}</span></td>
                    <td className="py-3"><span className={statusBadge(h.status)}>{statusLabel(h.status)}</span></td>
                    <td className="py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex gap-2 justify-end">
                        {(h.status === 'enrolled' || h.status === 'active') && (<><button onClick={() => handleQuarantine(h.id)} className="text-yellow-600 text-xs hover:underline">격리</button><button onClick={() => handleRevoke(h.id)} className="text-red-600 text-xs hover:underline">폐기</button></>)}
                        {h.status === 'quarantined' && <button onClick={() => handleReactivate(h.id)} className="text-green-600 text-xs hover:underline">재활성화</button>}
                      </div>
                    </td>
                  </tr>
                  {expandedId === h.id && (
                    <tr className="bg-gray-50"><td colSpan={6} className="p-4">
                      <div className="grid grid-cols-3 gap-4 text-sm">
                        <div><span className="text-gray-500">디바이스:</span> {h.device_id || '-'}</div>
                        <div><span className="text-gray-500">정책 프로필:</span> {h.policy_profile || '-'}</div>
                        <div><span className="text-gray-500">마지막 하트비트:</span> {h.last_heartbeat?.slice(0, 19) || '-'}</div>
                        <div><span className="text-gray-500">마지막 증명:</span> {h.last_attestation?.slice(0, 19) || '-'}</div>
                        <div><span className="text-gray-500">빌드 채널:</span> {h.build_channel || '-'}</div>
                        <div><span className="text-gray-500">CP 엔드포인트:</span> {h.cp_endpoint || '-'}</div>
                        <div className="col-span-3"><span className="text-gray-500">공개키:</span> <code className="text-xs bg-white px-1.5 py-0.5 rounded border border-gray-200">{h.public_key?.slice(0, 40)}...</code></div>
                        {h.revocation_reason && <div className="col-span-3 text-red-600">폐기 사유: {h.revocation_reason}</div>}
                      </div>
                    </td></tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
    </div>
  )
}
