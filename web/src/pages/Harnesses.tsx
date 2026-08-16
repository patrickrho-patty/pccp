import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useServerTable, buildQuery } from '../hooks/useServerTable'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { useRowNav } from '../hooks/useRowNav'
import { ResponsiveTable, Column } from '../components/ResponsiveTable'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { formatRelative } from '../utils/format'
import { exportCSV } from '../utils/csv'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

const PAGE_SIZE = 25

export default function Harnesses() {
  const confirm = useConfirm()
  const [users, setUsers] = useState<any[]>([])
  const [org, setOrg] = useState<any>(null)
  const [selectedHarnesses, setSelectedHarnesses] = useState<Set<string>>(new Set())
  const [showForm, setShowForm] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [codeTarget, setCodeTarget] = useState<any | null>(null)
  const [issuedCode, setIssuedCode] = useState<{ code: string; expires_at: string } | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<any | null>(null)
  const [revokeReason, setRevokeReason] = useState('')
  const { favorites, toggle, isFavorite, sortPinnedFirst } = useFavorites('harnesses')
  const [form, setForm] = useState({
    user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0',
    enrollment_code: '', device_hostname: '', device_os: '', device_arch: '',
  })

  // Server-side list (C4): filters/sort/pagination run on the API.
  const table = useServerTable<any>((q) =>
    api.listHarnesses({
      page: String(q.page), size: String(q.size), search: q.search,
      sort: q.sort,
      ...q.filters,
    })
  , { size: PAGE_SIZE, initialFilters: {} })

  const loadUsers = () => {
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
    api.listOrganizations().then(data => setOrg(Array.isArray(data) && data[0] ? data[0] : null))
  }
  useEffect(() => { loadUsers() }, [])

  const getUser = (h: any) => {
    try {
      const ids: string[] = JSON.parse(h.allowed_users || '[]')
      return users.find(u => ids.includes(u.id))
    } catch { return undefined }
  }

  const rows = useMemo(() => sortPinnedFirst(table.rows, h => h.id), [table.rows, favorites])

  const openDetail = (h: any) => window.location.assign(`/harnesses/${h.id}`)
  const { selectedIndex } = useRowNav(rows.length, (i) => openDetail(rows[i]), true)

  const handleEnroll = async (e: React.FormEvent) => {
    e.preventDefault()
    // Organization comes from the operator's JWT on the backend — never
    // guess from other rows (UX bug #4).
    try {
      const res: any = await api.enrollHarness({
        ...form,
        enrollment_mode: form.enrollment_code ? 'code' : 'sso',
      })
      showToast('하네스 등록됨 · PPC 발급 완료', 'success')
      setShowForm(false)
      setForm({ user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0', enrollment_code: '', device_hostname: '', device_os: '', device_arch: '' })
      table.reload()
      if (res?.credential) {
        showToast(`서명된 PPC가 발급되었습니다 (직렬: ${res.credential?.serial || '…'})`, 'info')
      }
    } catch (err: any) { showToast('등록 실패: ' + err.message, 'error') }
  }

  const issueCode = async () => {
    if (!codeTarget?.id) return
    try {
      const res: any = await api.issueEnrollmentCode(codeTarget.id)
      setIssuedCode({ code: res.enrollment_code || res.code, expires_at: res.expires_at || '' })
      showToast('1회용 등록 코드 발급됨', 'success')
    } catch (err: any) { showToast('코드 발급 실패: ' + err.message, 'error') }
  }

  const copyCode = () => {
    if (issuedCode) {
      navigator.clipboard?.writeText(issuedCode.code)
      showToast('코드가 클립보드에 복사됨', 'info')
    }
  }

  const handleRevoke = async () => {
    if (!revokeTarget) return
    if (!revokeReason.trim()) { showToast('폐기 사유를 입력하세요', 'error'); return }
    try {
      const res: any = await api.revokeHarness(revokeTarget.id, revokeReason)
      showToast(res?.relay_propagated === false ? '폐기됨 (릴레이 채널 미연결 — 다음 접속부터 차단)' : '폐기됨 · PPC가 CA 폐기 목록에 등록됨', 'info')
      setRevokeTarget(null); setRevokeReason('')
      table.reload()
    } catch (err: any) { showToast('폐기 실패: ' + err.message, 'error') }
  }

  const handleQuarantine = async (h: any) => {
    if (!await confirm({ title: '격리 확인', message: `${h.harness_id} 를 격리하시겠습니까? 모든 활성 세션이 종료됩니다.`, danger: true })) return
    try {
      const res: any = await api.quarantineHarness(h.id)
      showToast(res?.relay_propagated === false ? '격리됨 (릴레이 채널 미연결 — 다음 요청부터 차단)' : '격리됨 · 활성 세션 종료', 'info')
      table.reload()
    } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const handleReactivate = async (h: any) => { try { await api.reactivateHarness(h.id); showToast('재활성화됨', 'success'); table.reload() } catch { showToast('실패했습니다 · action failed', 'error') } }

  const bulk = async (action: 'quarantine' | 'revoke') => {
    const ids = [...selectedHarnesses]
    if (action === 'revoke' && !await confirm({ title: '일괄 폐기', message: `${ids.length}개 하네스를 폐기하시겠습니까?`, danger: true })) return
    let ok = 0
    for (const id of ids) {
      try {
        if (action === 'quarantine') await api.quarantineHarness(id)
        else await api.revokeHarness(id, 'bulk action')
        ok++
      } catch { showToast('실패했습니다 · action failed', 'error') }
    }
    showToast(`${ok}/${ids.length} 완료`, ok === ids.length ? 'success' : 'error')
    setSelectedHarnesses(new Set())
    table.reload()
  }

  const statusBadge = (s: string) => { const m: Record<string,string> = { enrolled:'badge-green', active:'badge-green', pending:'badge-yellow', quarantined:'badge-red', revoked:'badge-gray' }; return m[s] || 'badge-gray' }
  const statusLabel = (s: string) => { const m: Record<string,string> = { enrolled:'등록됨', active:'활성', pending:'대기', quarantined:'격리됨', revoked:'폐기됨' }; return m[s] || s }
  const riskBadge = (s: string) => s === 'normal' ? 'badge-green' : s === 'elevated' ? 'badge-yellow' : 'badge-red'

  const columns: Column<any>[] = [
    {
      key: 'pin', header: '★', cardLabel: '고정',
      render: (h) => <FavoriteStar entity="harnesses" id={h.id} onToggle={() => toggle(h.id)} />,
    },
    {
      key: 'id', header: '하네스 ID', cardLabel: '하네스 ID',
      render: (h) => (
        <Link to={`/harnesses/${h.id}`} className="font-mono text-xs text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>
          {h.harness_id?.slice(0, 20)}{h.stale && <span className="ml-1 text-[10px] text-yellow-600" title="하트비트 만료 · Stale">⚠</span>}
        </Link>
      ),
      onClick: (h) => setExpandedId(expandedId === h.id ? null : h.id),
    },
    {
      key: 'user', header: '사용자 · User', cardLabel: '사용자',
      render: (h) => {
        const user = getUser(h)
        return user ? (
          <Link to={`/users/${user.id}`} className="text-sm font-medium text-blue-600 hover:underline">{user.name_ko || user.name}</Link>
        ) : <span className="text-xs text-gray-400">-</span>
      },
    },
    {
      key: 'status', header: '상태', cardLabel: '상태',
      render: (h) => <span className={statusBadge(h.status)}>{statusLabel(h.status)}</span>,
    },
    {
      key: 'version', header: '버전', cardLabel: '버전',
      render: (h) => <span className="text-xs text-gray-500">v{h.binary_version} <span className="text-gray-400">({h.build_channel || 'stable'})</span></span>,
    },
    {
      key: 'risk', header: '위험', cardLabel: '위험도',
      render: (h) => <span className={riskBadge(h.risk_state)}>{h.risk_state}</span>,
    },
    {
      key: 'sessions', header: '세션', cardLabel: '세션',
      render: (h) => (
        <Link to={`/sessions?harness_id=${encodeURIComponent(h.harness_id)}`} className="text-sm text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>
          세션 보기 →
        </Link>
      ),
    },
    {
      key: 'enrolled', header: '등록일', cardLabel: '등록일',
      render: (h) => <span className="text-xs text-gray-400">{formatRelative(h.enrolled_at)}</span>,
    },
    {
      key: 'actions', header: '작업', cardLabel: '작업',
      render: (h) => (
        <div className="flex gap-1 flex-wrap" onClick={e => e.stopPropagation()}>
          {(h.status === 'active' || h.status === 'enrolled') && <button onClick={() => handleQuarantine(h)} className="text-xs text-yellow-600 hover:underline">격리</button>}
          {(h.status === 'active' || h.status === 'enrolled') && <button onClick={() => { setRevokeTarget(h); setRevokeReason('') }} className="btn-link-danger">폐기</button>}
          {h.status === 'quarantined' && <button onClick={() => handleReactivate(h)} className="text-xs text-green-600 hover:underline">재활성화</button>}
          <Link to={`/harnesses/${h.id}`} className="btn-link">상세</Link>
        </div>
      ),
    },
  ]

  const expanded = (h: any) => {
    const user = getUser(h)
    return (
      <div className="grid grid-cols-2 md:grid-cols-4 gap-6 expand-enter">
        <div>
          <div className="text-xs font-semibold text-gray-600 mb-2">하네스 정보</div>
          <div className="space-y-1 text-xs text-gray-500">
            <div>하네스 ID: <span className="font-mono">{h.harness_id?.slice(0, 30)}</span></div>
            <div>빌드 해시: <span className="font-mono">{h.binary_hash?.slice(0, 16) || '-'}</span></div>
            <div>릴리스 채널: {h.build_channel || 'stable'}</div>
            <div>정책 프로필: {h.policy_profile || '-'}</div>
            <div>등록일: {formatRelative(h.enrolled_at)}</div>
            <div>하트비트: {formatRelative(h.last_heartbeat)}{h.stale && <span className="text-yellow-600"> · ⚠ 만료</span>}</div>
            <div>등록 모드: {h.enrollment_mode || 'sso'}</div>
          </div>
        </div>
        <div>
          <div className="text-xs font-semibold text-gray-600 mb-2">사용자</div>
          {user ? (
            <div className="space-y-1 text-xs">
              <div><Link to={`/users/${user.id}`} className="text-blue-600 hover:underline font-medium">{user.name_ko || user.name}</Link></div>
              <div className="text-gray-400">{user.email}</div>
              {user.employee_id && <div className="text-gray-400">사번: {user.employee_id}</div>}
            </div>
          ) : <span className="text-xs text-gray-400">사용자 정보 없음</span>}
        </div>
        <div>
          <div className="text-xs font-semibold text-gray-600 mb-2">보안</div>
          <div className="space-y-1 text-xs text-gray-500">
            <div>위험 상태: <span className={riskBadge(h.risk_state)}>{h.risk_state}</span></div>
            <div>인증 방식: {h.enrollment_mode || 'sso'}</div>
            {h.version_blocked && <div className="text-red-600">버전 차단됨 · min {h.forced_version?.min_version}</div>}
          </div>
        </div>
        <div>
          <div className="text-xs font-semibold text-gray-600 mb-2">바로가기</div>
          <div className="space-y-1 text-xs">
            <Link to={`/harnesses/${h.id}`} className="text-blue-600 hover:underline block">상세 페이지 →</Link>
            <Link to={`/sessions?harness_id=${encodeURIComponent(h.harness_id)}`} className="text-blue-600 hover:underline block">세션 보기 →</Link>
            <Link to="/audit" className="text-blue-600 hover:underline block">감사 로그 →</Link>
          </div>
        </div>
      </div>
    )
  }

  const nonRevoked = (table.rows as any[]).filter(h => h.status !== 'revoked').length
  const staleCount = (table.rows as any[]).filter(h => h.stale).length

  return (
    <div>
      <div className="flex justify-between items-center mb-6 flex-wrap gap-2">
        <div>
          <h1 className="text-2xl font-bold">하네스 <span className="text-gray-400 text-lg font-normal">Harnesses</span></h1>
          <p className="text-xs text-gray-400 mt-1">
            {org && <span>{nonRevoked}/{org.max_harness_seats || '∞'} 좌석 · </span>}
            {staleCount > 0 && <span className="text-yellow-600">{staleCount}개 하트비트 만료 · </span>}
            하네스 ID 또는 사용자를 클릭하여 상세 이동
          </p>
        </div>
        <div className="flex gap-2 shrink-0 flex-wrap">
          <button onClick={() => { setCodeTarget(null); setIssuedCode(null); setShowForm(!showForm); setForm({ user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0', enrollment_code: '', device_hostname: '', device_os: '', device_arch: '' }) }} className="btn-primary">
            {showForm ? '취소' : '+ 하네스 등록'}
          </button>
          <button onClick={() => setCodeTarget({ id: users[0]?.id })} className="btn-secondary">🔑 등록 코드 발급</button>
          <button onClick={() => exportCSV(`harnesses_${new Date().toISOString().slice(0,10)}.csv`, ['하네스 ID', '버전', '채널', '상태', '위험도', '사용자', '하트비트', '등록일'], table.rows.map(h => [h.harness_id, h.binary_version, h.build_channel, h.status, h.risk_state, getUser(h)?.name_ko || '', h.last_heartbeat?.slice(0,16) || '-', h.enrolled_at?.slice(0,10)]))} className="btn-sm btn-secondary">📥 CSV</button>
        </div>
      </div>

      {showForm && (
        <form onSubmit={handleEnroll} className="card mb-6 space-y-4 expand-enter">
          <h2 className="text-sm font-semibold">하네스 등록 · Enroll</h2>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div><label className="label">사용자 · User</label><select className="input" value={form.user_id} onChange={e => setForm({ ...form, user_id: e.target.value })} required><option value="">선택...</option>{users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>)}</select></div>
            <div><label className="label">등록 코드 · Code (선택)</label><input className="input font-mono" value={form.enrollment_code} onChange={e => setForm({ ...form, enrollment_code: e.target.value })} placeholder="1회용 코드" /></div>
            <div><label className="label">하네스 ID</label><input className="input font-mono" value={form.harness_id} onChange={e => setForm({ ...form, harness_id: e.target.value })} placeholder="자동 생성 시 비워두기" /></div>
            <div><label className="label">바이너리 버전</label><input className="input" value={form.binary_version} onChange={e => setForm({ ...form, binary_version: e.target.value })} /></div>
            <div><label className="label">공개키 (ed25519 hex)</label><input className="input font-mono" value={form.public_key_hex} onChange={e => setForm({ ...form, public_key_hex: e.target.value })} placeholder="64자리 hex" required /></div>
            <div><label className="label">기기 호스트명</label><input className="input" value={form.device_hostname} onChange={e => setForm({ ...form, device_hostname: e.target.value })} placeholder="harness가 제공" /></div>
            <div><label className="label">기기 OS</label><input className="input" value={form.device_os} onChange={e => setForm({ ...form, device_os: e.target.value })} placeholder="darwin / linux / windows" /></div>
            <div><label className="label">기기 아키텍처</label><input className="input" value={form.device_arch} onChange={e => setForm({ ...form, device_arch: e.target.value })} placeholder="arm64 / amd64" /></div>
          </div>
          <p className="text-[10px] text-gray-400">등록 시 서명된 Peer Credential(PPC, 90일)이 발급되고 CA 폐기 목록에 연동됩니다. 조직 좌석 한도와 최소 버전 정책이 적용됩니다.</p>
          <button type="submit" className="btn-primary">등록</button>
        </form>
      )}

      {selectedHarnesses.size > 0 && (
        <div className="flex items-center gap-3 mb-4 p-3 bg-blue-50 rounded-lg flex-wrap">
          <span className="text-sm font-medium text-blue-700">{selectedHarnesses.size}개 하네스 선택됨</span>
          <button onClick={() => bulk('quarantine')} className="btn-sm btn-secondary">일괄 격리</button>
          <button onClick={() => bulk('revoke')} className="btn-sm btn-danger">일괄 폐기</button>
          <button onClick={() => setSelectedHarnesses(new Set())} className="btn-sm btn-secondary">취소</button>
        </div>
      )}

      {/* Server-side filter bar (C4) */}
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <input className="input flex-1 min-w-[200px]" placeholder="하네스 ID, 버전 검색..." value={table.search} onChange={e => table.setSearch(e.target.value)} />
        <select className="input max-w-[130px] text-xs" value={table.filters.status || ''} onChange={e => table.setFilter('status', e.target.value)}>
          <option value="">상태: 전체</option>
          <option value="enrolled">등록됨</option><option value="active">활성</option>
          <option value="quarantined">격리됨</option><option value="revoked">폐기됨</option>
        </select>
        <select className="input max-w-[130px] text-xs" value={table.filters.risk_state || ''} onChange={e => table.setFilter('risk_state', e.target.value)}>
          <option value="">위험도: 전체</option>
          <option value="normal">정상</option><option value="elevated">주의</option><option value="high">높음</option>
        </select>
        <select className="input max-w-[130px] text-xs" value={table.filters.build_channel || ''} onChange={e => table.setFilter('build_channel', e.target.value)}>
          <option value="">링: 전체</option>
          <option value="stable">stable</option><option value="beta">beta</option><option value="canary">canary</option>
        </select>
        <select className="input max-w-[150px] text-xs" value={table.filters.user || ''} onChange={e => table.setFilter('user', e.target.value)}>
          <option value="">사용자: 전체</option>
          {users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name}</option>)}
        </select>
        <select className="input max-w-[130px] text-xs" value={table.sort} onChange={e => table.setSort(e.target.value)}>
          <option value="">정렬: 등록일</option>
          <option value="binary_version">버전</option>
          <option value="risk_state">위험도</option>
          <option value="enrolled_at">등록일</option>
        </select>
      </div>

      <div className="card !p-0">
        {table.loading && table.rows.length === 0 ? (
          <div className="p-8 space-y-3 animate-pulse">
            <div className="h-4 bg-gray-100 rounded w-3/4" />
            <div className="h-4 bg-gray-100 rounded w-1/2" />
            <div className="h-4 bg-gray-100 rounded w-2/3" />
          </div>
        ) : table.rows.length === 0 ? (
          <EmptyState
            icon="⬡"
            title="첫 하네스를 등록하세요"
            message="등록 코드를 발급하면 개발자 기기가 거버넌스 피어로 합류합니다"
            action={{ label: '🔑 등록 코드 발급', onClick: () => setCodeTarget({ id: users[0]?.id }) }}
          />
        ) : (
          <ResponsiveTable
            columns={columns}
            rows={rows}
            rowKey={(h) => h.id}
            expand={(h) => expandedId === h.id ? expanded(h) : null}
          />
        )}
        <div className="flex items-center justify-between px-4 py-3 text-xs text-gray-500 border-t border-gray-100">
          <span>{(table.page - 1) * PAGE_SIZE + 1}-{Math.min(table.page * PAGE_SIZE, table.total)} / {table.total}건 {isFavorite ? '· ★ 고정 우선' : ''}</span>
          <div className="flex gap-1">
            <button onClick={() => table.setPage(table.page - 1)} disabled={table.page === 1} className="btn-sm btn-secondary">이전</button>
            <span className="px-2 py-1">{table.page} / {Math.max(Math.ceil(table.total / PAGE_SIZE), 1)}</span>
            <button onClick={() => table.setPage(table.page + 1)} disabled={table.page * PAGE_SIZE >= table.total} className="btn-sm btn-secondary">다음</button>
          </div>
        </div>
      </div>

      {/* Enrollment code modal (B3) */}
      <Modal open={!!codeTarget} title="등록 코드 발급 · Issue Enrollment Code" subtitle="1회용 · 24시간 유효" onClose={() => { setCodeTarget(null); setIssuedCode(null) }} size="sm"
        footer={<ModalFooter onCancel={() => { setCodeTarget(null); setIssuedCode(null) }} onConfirm={issueCode} confirmLabel={issuedCode ? '재발급' : '발급'} disabled={!codeTarget?.id} />}>
        <div className="space-y-3">
          <div>
            <label className="label">대상 사용자</label>
            <select className="input" value={codeTarget?.id || ''} onChange={e => setCodeTarget({ id: e.target.value })}>
              <option value="">선택...</option>
              {users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>)}
            </select>
          </div>
          {issuedCode && (
            <div className="bg-blue-50 border border-blue-200 rounded-lg p-3">
              <div className="text-xs text-gray-500 mb-1">하네스가 이 코드로 스스로 등록합니다 (관리자 키 붙여넣기 불필요):</div>
              <div className="font-mono text-sm break-all bg-white border border-blue-200 rounded p-2">{issuedCode.code}</div>
              {issuedCode.expires_at && <div className="text-[10px] text-gray-400 mt-1">만료: {issuedCode.expires_at.slice(0, 16).replace('T', ' ')}</div>}
              <button onClick={copyCode} className="btn-sm btn-secondary mt-2">📋 복사</button>
            </div>
          )}
        </div>
      </Modal>

      {/* Revoke modal — reason required (UX8/B1) */}
      <Modal open={!!revokeTarget} title="하네스 폐기 · Revoke Harness" subtitle={revokeTarget?.harness_id} onClose={() => setRevokeTarget(null)} size="sm"
        footer={<ModalFooter onCancel={() => setRevokeTarget(null)} onConfirm={handleRevoke} confirmLabel="폐기 실행" danger disabled={!revokeReason.trim()} />}>
        <div>
          <p className="text-sm text-gray-600 mb-3">이 하네스를 폐기하시겠습니까? PPC가 CA 폐기 목록에 등록되고 모든 활성 세션이 종료되며, 릴레이가 다음 접속을 거부합니다.</p>
          <label className="label">폐기 사유 · Reason (필수)</label>
          <textarea className="input" rows={3} value={revokeReason} onChange={e => setRevokeReason(e.target.value)} placeholder="예: 직원 퇴사, 기기 분실" />
        </div>
      </Modal>
    </div>
  )
}
