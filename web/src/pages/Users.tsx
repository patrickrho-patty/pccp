import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useServerTable, buildQuery, ServerQuery } from '../hooks/useServerTable'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { StatCard } from '../components/StatCard'
import { EntitySelect } from '../components/EntitySelect'
import { Modal, ModalFooter } from '../components/Modal'
import ConfirmDialog from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import { ResponsiveTable, Column } from '../components/ResponsiveTable'
import { exportCSV } from '../utils/csv'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'
import { useRowNav } from '../hooks/useRowNav'
import { userActions, userActionSpec, applyUserLifecycle, canIssueEnrollment, STATUS_KO, STATUS_BADGE, UserLifecycleAction } from '../userLifecycle'

// Users page (web/01 plan): managed user population — governed
// subjects, NOT console operators. Server-side list (B3), business-unit
// picker (A1), harness binding (A2), enrollment codes (A3), seats (A4),
// structured contractors (A5), audit + reasons (B1), offboard workflow
// (B2), detail links (B4), entitlements (B5), usage (B6), SCIM/CSV (B7),
// SSO status (B8).

const AUTH_METHODS = [
  { value: 'local', label: 'Local' },
  { value: 'oidc', label: 'OIDC' },
  { value: 'saml', label: 'SAML 2.0' },
  { value: 'ldap', label: 'LDAP / AD' },
  { value: 'scim', label: 'SCIM' },
]

const STATUS_OPTIONS = [
  { value: 'active', label: '활성' },
  { value: 'suspended', label: '정지' },
  { value: 'offboarded', label: '퇴사' },
]

function initials(name: string) {
  return (name || '?').slice(0, 2).toUpperCase()
}

export default function Users() {
  const confirm = useConfirm()
  const { favorites, sortPinnedFirst } = useFavorites('users')

  const fetchUsers = (q: ServerQuery) =>
    api.listUsersPaged(buildQuery(q)).then(res => {
      if (Array.isArray(res)) return res
      return { data: res.data ?? [], total: res.total ?? 0, page: res.page, size: res.size }
    })

  const table = useServerTable<any>(fetchUsers, {
    size: 25,
    sortFields: ['name', 'name_ko', 'email', 'last_login'],
  })

  const [businessUnits, setBusinessUnits] = useState<any[]>([])
  const [roles, setRoles] = useState<any[]>([])
  const roleLabelById = useMemo(() => {
    const m = new Map<string, string>()
    for (const r of roles) m.set(r.id, r.name_ko || r.name)
    return m
  }, [roles])
  const [seatUsage, setSeatUsage] = useState<any>(null)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState({
    email: '', name: '', name_ko: '', title: '', auth_method: 'local', business_unit_id: '', employee_id: '',
  })
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [reasonTarget, setReasonTarget] = useState<{ id: string; name: string; email: string; action: UserLifecycleAction } | null>(null)
  const [reasonText, setReasonText] = useState('')
  const [enrollTarget, setEnrollTarget] = useState<any>(null)
  const [enrollCode, setEnrollCode] = useState<{ code: string; expires_at: string } | null>(null)
  const [bulkUnitOpen, setBulkUnitOpen] = useState(false)
  const [bulkUnitId, setBulkUnitId] = useState('')
  const [importOpen, setImportOpen] = useState(false)
  const [importResult, setImportResult] = useState<any>(null)
  const [importApplying, setImportApplying] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const loadMeta = () => {
    api.listBusinessUnits().then(d => setBusinessUnits(Array.isArray(d) ? d : []))
    api.listRoles().then(d => setRoles(Array.isArray(d) ? d : []))
    api.getSeatUsage().then(s => setSeatUsage(s)).catch(() => setSeatUsage(null))
  }
  useEffect(() => { loadMeta() }, [])

  const rows = sortPinnedFirst(table.rows, u => u.id)
  const { selectedIndex } = useRowNav(rows.length, (i) => setExpandedId(expandedId === rows[i].id ? null : rows[i].id))

  const toggleSelect = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const openCreate = () => {
    setEditingId(null)
    setForm({ email: '', name: '', name_ko: '', title: '', auth_method: 'local', business_unit_id: '', employee_id: '' })
    setShowForm(true)
  }
  const openEdit = (u: any) => {
    setEditingId(u.id)
    setForm({
      email: u.email || '', name: u.name || '', name_ko: u.name_ko || '', title: u.title || '',
      auth_method: u.auth_method || 'local', business_unit_id: u.business_unit_id || '', employee_id: u.employee_id || '',
    })
    setShowForm(true)
  }

  const saveForm = async () => {
    try {
      if (editingId) {
        await api.updateUser(editingId, form)
        showToast('저장 완료 · Saved', 'success')
      } else {
        await api.createUser(form)
        showToast('사용자 생성 완료 · Created', 'success')
      }
      setShowForm(false)
      table.reload()
      loadMeta()
    } catch (e: any) {
      showToast(e?.message || '저장 실패', 'error')
    }
  }

  const runReasonAction = async () => {
    if (!reasonTarget) return
    if (!reasonText.trim()) {
      showToast('사유를 입력해주세요 (감사 로그에 기록됩니다)', 'error')
      return
    }
    try {
      if (reasonTarget.id === '__bulk__') {
        let done = 0, failed = 0
        for (const uid of selectedIds) {
          try { await applyUserLifecycle(reasonTarget.action, uid, reasonText); done++ } catch { failed++ }
        }
        showToast(`일괄 처리 완료 — 성공 ${done}${failed ? `, 실패 ${failed}` : ''}`, failed ? 'error' : 'success')
        setSelectedIds(new Set())
      } else {
        await applyUserLifecycle(reasonTarget.action, reasonTarget.id, reasonText)
        showToast('완료 · Done', 'success')
      }
      setReasonTarget(null)
      setReasonText('')
      table.reload()
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
      table.reload() // a 409 usually means stale row state — refresh it
    }
  }

  const issueEnrollment = async (u: any) => {
    try {
      const res = await api.issueEnrollmentCode(u.id)
      setEnrollCode(typeof res === 'string' ? { code: res, expires_at: '' } : (res as any))
      setEnrollTarget(u)
    } catch (e: any) {
      showToast(e?.message || '발급 실패', 'error')
    }
  }

  const bulkAssignUnit = async () => {
    try {
      for (const id of selectedIds) {
        await api.updateUser(id, { business_unit_id: bulkUnitId })
      }
      showToast(`부서 일괄 배정 완료 (${selectedIds.size})`, 'success')
      setBulkUnitOpen(false)
      setSelectedIds(new Set())
      table.reload()
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
    }
  }

  const runImport = async (apply: boolean) => {
    const file = fileRef.current?.files?.[0]
    if (!file) {
      showToast('CSV 파일을 선택하세요', 'error')
      return
    }
    setImportApplying(true)
    try {
      const res = await api.importUsersCSV(file, apply)
      setImportResult(res)
      if (apply) {
        showToast(`가져오기 완료 (${res.imported}명)`, 'success')
        table.reload()
      }
    } catch (e: any) {
      showToast(e?.message || '가져오기 실패', 'error')
    } finally {
      setImportApplying(false)
    }
  }

  const exportSelected = () => {
    const sel = selectedIds.size ? rows.filter(r => selectedIds.has(r.id)) : rows
    exportCSV('users.csv',
      ['email', 'name', 'name_ko', 'title', 'auth_method', 'status', 'employee_id', 'business_unit_id'],
      sel.map(u => [
        u.email, u.name, u.name_ko, u.title,
        u.auth_method, u.status, u.employee_id || '',
        u.business_unit_id || '',
      ]))
  }

  const columns: Column<any>[] = [
    {
      key: 'name', header: '사용자',
      render: (u) => (
        <div className="flex items-center gap-2">
          <div className={`w-7 h-7 rounded-full flex items-center justify-center text-[10px] font-bold ${u.status === 'offboarded' ? 'bg-gray-200 text-gray-500' : 'bg-blue-100 text-blue-700'}`}>
            {initials(u.name_ko || u.name)}
          </div>
          <div className="min-w-0">
            <Link to={`/users/${u.id}`} className="text-xs font-semibold hover:underline text-gray-800 truncate block">
              {u.name_ko || u.name} <span className="text-gray-400 font-normal">({u.name})</span>
            </Link>
            <div className="text-[10px] text-gray-400 truncate">{u.email}</div>
          </div>
          {u.contractor_info && <span title="계약직" className="text-[10px] px-1 rounded bg-purple-50 text-purple-600 border border-purple-200">계약</span>}
        </div>
      ),
      cardLabel: '사용자',
    },
    {
      key: 'auth', header: '인증',
      render: (u) => (
        <div className="text-[11px]">
          <div className="text-gray-600">{(AUTH_METHODS.find(m => m.value === u.auth_method) || {} as any).label || u.auth_method}</div>
          {u.auth_method !== 'local' && u.auth_method && (
            <div className={u.external_id ? 'text-green-600' : 'text-amber-600'}>
              {u.external_id ? 'OIDC 연결됨' : '미연결'}
            </div>
          )}
          {u.last_login_at && <div className="text-gray-400">최근 로그인 {u.last_login_at.slice(0, 10)}</div>}
        </div>
      ),
      cardLabel: '인증',
    },
    {
      key: 'unit', header: '부서',
      render: (u) => {
        const bu = businessUnits.find(b => b.id === u.business_unit_id)
        return <span className="text-[11px] text-gray-600">{bu ? bu.name_ko || bu.name : (u.business_unit_id ? u.business_unit_id.slice(0, 8) : '—')}</span>
      },
      cardLabel: '부서',
    },
    {
      key: 'title', header: '직함',
      render: (u) => <span className="text-[11px] text-gray-600">{u.title || u.title_ko || '—'}</span>,
      cardLabel: '직함',
    },
    {
      key: 'status', header: '상태',
      render: (u) => (
        <span className={`text-[10px] px-2 py-0.5 rounded-full border ${STATUS_BADGE[u.status] || STATUS_BADGE.offboarded}`}>
          {STATUS_KO[u.status] || u.status}
        </span>
      ),
      cardLabel: '상태',
    },
    {
      key: 'access', header: '권한 / 하네스',
      render: (u) => {
        const labels = (u.role_ids || []).map((rid: string) => roleLabelById.get(rid)).filter(Boolean)
        return <div className="text-[11px] text-gray-600"><div>{labels.join(', ') || '권한 없음'}</div><div className="text-gray-400">하네스 {u.harness_count ?? 0}대</div></div>
      },
      cardLabel: '권한 / 하네스',
    },
    {
      key: 'actions', header: '',
      render: (u) => (
        <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
          <FavoriteStar entity="users" id={u.id} />
          {canIssueEnrollment(u.status) && (
            <button className="btn-xs-secondary" title="초대 코드 발급"
              onClick={() => issueEnrollment(u)}>초대 코드</button>
          )}
          {userActions(u.status).map(a => (
            <button key={a.action}
              className={a.action === 'offboard' ? 'btn-xs-danger'
                : a.action === 'resume' ? 'text-[10px] px-2 py-1 rounded hover:bg-green-50 text-green-700'
                : 'text-[10px] px-2 py-1 rounded hover:bg-amber-50 text-amber-600'}
              onClick={() => { setReasonTarget({ id: u.id, name: u.name_ko || u.name, email: u.email, action: a.action }); setReasonText('') }}>{a.label}</button>
          ))}
          {u.status !== 'offboarded' && (
            <button className="btn-xs-secondary"
              onClick={() => openEdit(u)}>수정</button>
          )}
        </div>
      ),
      cardLabel: '작업',
    },
  ]

  const seats = seatUsage || { users: 0, max_users: 0, harnesses: 0, max_harnesses: 0 }

  return (
    <div className="p-6 space-y-4 page-enter">
      {/* Seats (A4) */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard label="사용자" value={seats.users ?? table.total} accent="blue"
          to="/users" sub={seats.max_users ? `좌석 ${seats.max_users}` : undefined} />
        <StatCard label="하네스" value={seats.harnesses ?? '—'} accent="green"
          to="/harnesses" sub={seats.max_harnesses ? `좌석 ${seats.max_harnesses}` : undefined} />
        <StatCard label="활성 세션" value={seats.active_sessions ?? '—'} accent="purple" to="/sessions" />
        <StatCard label="계약직" value={rows.filter(u => !!u.contractor_info).length} accent="orange"
          to="/users" sub="구조화된 계약 프로필" />
      </div>

      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">사용자 · Users</h2>
          <p className="text-[11px] text-gray-400">관리 대상 사용자 (하네스 사용자) — 콘솔 운영자와 별개입니다.</p>
        </div>
        <div className="flex gap-2 flex-wrap">
          <button className="btn-sm btn-secondary" onClick={() => setImportOpen(true)}>CSV 가져오기</button>
          <button className="btn-sm btn-secondary" onClick={exportSelected}>내보내기</button>
          {selectedIds.size > 0 && (
            <>
              <button className="btn-sm btn-secondary" onClick={() => setBulkUnitOpen(true)}>
                부서 일괄 배정 ({selectedIds.size})
              </button>
              <button className="btn-sm btn-danger"
                onClick={() => { setReasonTarget({ id: '__bulk__', name: `선택 ${selectedIds.size}명`, email: '', action: 'suspend' }); setReasonText('') }}>
                일괄 정지 ({selectedIds.size})
              </button>
            </>
          )}
          <button className="btn-sm btn-primary" onClick={openCreate}>+ 새 사용자</button>
        </div>
      </div>

      {/* Filters + sort (server-side) */}
      <div className="flex gap-2 flex-wrap items-center">
        <input
          className="input text-xs w-56"
          placeholder="이름 / 이메일 / 사번 검색..."
          value={table.search}
          onChange={e => table.setSearch(e.target.value)}
        />
        <select className="input text-xs w-28" value={table.filters.status || ''}
          onChange={e => table.setFilter('status', e.target.value)}>
          <option value="">전체 상태</option>
          {STATUS_OPTIONS.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
        </select>
        <select className="input text-xs w-28" value={table.filters.auth_method || ''}
          onChange={e => table.setFilter('auth_method', e.target.value)}>
          <option value="">전체 인증</option>
          {AUTH_METHODS.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
        </select>
        <select className="input text-xs w-32" value={table.filters.business_unit || ''}
          onChange={e => table.setFilter('business_unit', e.target.value)}>
          <option value="">전체 부서</option>
          {businessUnits.map(b => <option key={b.id} value={b.id}>{b.name_ko || b.name}</option>)}
        </select>
        <select className="input text-xs w-32" value={table.filters.role || ''}
          onChange={e => table.setFilter('role', e.target.value)}>
          <option value="">전체 권한</option>
          {roles.map(r => <option key={r.id} value={r.id}>{r.name_ko || r.name}</option>)}
        </select>
        <select className="input text-xs w-28" value={table.sort}
          onChange={e => table.setSort(e.target.value)}>
          <option value="">정렬: 최신순</option>
          <option value="name">이름</option>
          <option value="name_ko">한글 이름</option>
          <option value="email">이메일</option>
          <option value="last_login">최근 로그인</option>
        </select>
        {table.loading && <span className="text-[10px] text-gray-400 animate-pulse">로딩...</span>}
        {table.error && <span className="text-[10px] text-red-500">{table.error}</span>}
      </div>

      <ResponsiveTable
        columns={columns}
        rows={rows}
        rowKey={u => u.id}
        expand={(u) => (
          <div className="px-3 py-2 text-[11px] text-gray-500 space-y-1">
            <div>사번: {u.employee_id || '—'} · 권한: {(u.role_ids || []).map((rid: string) => roleLabelById.get(rid)).filter(Boolean).join(', ') || '—'}</div>
            <div className="flex gap-2 shrink-0 flex-wrap">
              <Link className="text-blue-600 hover:underline" to={`/users/${u.id}`}>상세 페이지</Link>
              <Link className="text-blue-600 hover:underline" to={`/users/${u.id}?tab=usage`}>사용량</Link>
              <Link className="text-blue-600 hover:underline" to={`/users/${u.id}?tab=harnesses`}>하네스</Link>
            </div>
          </div>
        )}
        empty={<EmptyState icon="👤" title="첫 사용자를 초대하세요"
          message="초대 코드로 하네스를 등록하거나 CSV로 일괄 가져올 수 있습니다."
          action={{ label: '+ 새 사용자', onClick: openCreate }} />}
      />

      {/* Pagination */}
      {table.total > table.size && (
        <div className="flex items-center justify-between text-[11px] text-gray-500">
          <span>총 {table.total}명</span>
          <div className="flex gap-1">
            <button className="btn-sm btn-secondary" disabled={table.page <= 1} onClick={() => table.setPage(p => Math.max(1, p - 1))}>이전</button>
            <span className="px-2 py-1">{table.page} / {Math.ceil(table.total / table.size)}</span>
            <button className="btn-sm btn-secondary" disabled={table.page >= Math.ceil(table.total / table.size)}
              onClick={() => table.setPage(p => p + 1)}>다음</button>
          </div>
        </div>
      )}

      {/* Create/Edit form (A1 business-unit picker) */}
      <Modal open={showForm} title={editingId ? '사용자 수정' : '새 사용자'} onClose={() => setShowForm(false)}
        footer={<ModalFooter onCancel={() => setShowForm(false)} onConfirm={saveForm} confirmLabel={editingId ? '저장' : '생성'} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">이름</label>
              <input className="input text-xs w-full" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">한글 이름</label>
              <input className="input text-xs w-full" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} />
            </div>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">이메일</label>
            <input className="input text-xs w-full" value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">직함</label>
              <input className="input text-xs w-full" value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">사번</label>
              <input className="input text-xs w-full" value={form.employee_id} onChange={e => setForm({ ...form, employee_id: e.target.value })} />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">인증 방식</label>
              <select className="input text-xs w-full" value={form.auth_method} onChange={e => setForm({ ...form, auth_method: e.target.value })}>
                {AUTH_METHODS.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
              </select>
            </div>
            <div>
              <label className="text-[10px] text-gray-500">부서</label>
              <EntitySelect entity="business_unit" value={form.business_unit_id}
                onChange={v => setForm({ ...form, business_unit_id: v })} />
            </div>
          </div>
        </div>
      </Modal>

      {/* Reason modal (B1) — driven by the shared lifecycle mapping (PAT-1489) */}
      <Modal open={!!reasonTarget} title={reasonTarget ? userActionSpec(reasonTarget.action).title : ''}
        onClose={() => setReasonTarget(null)}
        footer={<ModalFooter onCancel={() => setReasonTarget(null)} onConfirm={runReasonAction}
          confirmLabel={reasonTarget ? userActionSpec(reasonTarget.action).label : ''}
          danger={reasonTarget ? userActionSpec(reasonTarget.action).danger : false} />}>
        <div className="space-y-2">
          {reasonTarget && (
            <p className="text-xs text-gray-700">
              대상: <span className="font-semibold">{reasonTarget.name}</span>
              {reasonTarget.email ? ` (${reasonTarget.email})` : ''}
            </p>
          )}
          <p className="text-xs text-gray-500">
            {reasonTarget ? `${userActionSpec(reasonTarget.action).effect} 사유를 남겨주세요.` : ''}
          </p>
          <textarea className="input text-xs w-full" rows={3} placeholder="사유 (감사 로그에 기록됩니다)"
            value={reasonText} onChange={e => setReasonText(e.target.value)} />
        </div>
      </Modal>

      {/* Enrollment code result (A3) */}
      <Modal open={!!enrollTarget} title="초대 코드 발급" onClose={() => { setEnrollTarget(null); setEnrollCode(null) }}
        footer={<ModalFooter onCancel={() => { setEnrollTarget(null); setEnrollCode(null) }} onConfirm={() => { setEnrollTarget(null); setEnrollCode(null) }} confirmLabel="닫기" />}>
        <div className="space-y-2">
          <p className="text-xs text-gray-500">{enrollTarget?.name_ko || enrollTarget?.name} ({enrollTarget?.email}) 님의 1회용 등록 코드입니다.</p>
          {enrollCode ? (
            <>
              <div className="font-mono text-lg text-center py-3 bg-gray-50 rounded-lg tracking-widest">{enrollCode.code}</div>
              {enrollCode.expires_at && <p className="text-[10px] text-gray-400 text-center">만료: {enrollCode.expires_at}</p>}
            </>
          ) : (
            <div className="text-xs text-gray-400 text-center py-3">발급 중...</div>
          )}
        </div>
      </Modal>

      {/* Bulk business-unit assign */}
      <Modal open={bulkUnitOpen} title={`부서 일괄 배정 (${selectedIds.size}명)`} onClose={() => setBulkUnitOpen(false)}
        footer={<ModalFooter onCancel={() => setBulkUnitOpen(false)} onConfirm={bulkAssignUnit} confirmLabel="배정" />}>
        <EntitySelect entity="business_unit" value={bulkUnitId} onChange={setBulkUnitId} />
      </Modal>

      {/* CSV import (B7) */}
      <Modal open={importOpen} title="CSV 가져오기" onClose={() => { setImportOpen(false); setImportResult(null) }}
        footer={
          <div className="flex gap-2 justify-end">
            <button className="btn-sm btn-secondary" onClick={() => { setImportOpen(false); setImportResult(null) }}>닫기</button>
            <button className="btn-sm btn-secondary" disabled={importApplying} onClick={() => runImport(false)}>미리보기</button>
            <button className="btn-sm btn-primary" disabled={importApplying || !importResult} onClick={() => runImport(true)}>적용</button>
          </div>
        }>
        <div className="space-y-2">
          <p className="text-[11px] text-gray-500">형식: <code className="bg-gray-100 px-1">email,name</code> 헤더의 CSV. 동일 이메일은 건너뜁니다 (SCIM 멱등).</p>
          <input ref={fileRef} type="file" accept=".csv,text/csv" className="text-xs" />
          {importResult && (
            <div className="text-[11px] space-y-1">
              <div className="font-semibold">
                {importResult.dry_run ? '미리보기' : '적용 완료'} — {importResult.imported}건 {importResult.dry_run ? '가져올 예정' : '가져옴'}
              </div>
              <div className="max-h-40 overflow-auto">
                {importResult.rows?.map((r: any) => (
                  <div key={r.line} className="flex justify-between">
                    <span>#{r.line} {r.email} {r.name}</span>
                    <span className={r.error ? 'text-red-500' : 'text-gray-400'}>{r.error || r.status}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </Modal>
    </div>
  )
}
