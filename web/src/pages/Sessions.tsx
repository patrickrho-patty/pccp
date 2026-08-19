import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useServerTable, buildQuery, ServerQuery } from '../hooks/useServerTable'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { EntitySelect } from '../components/EntitySelect'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { ResponsiveTable, Column } from '../components/ResponsiveTable'
import { showToast } from '../components/Toast'
import { useRowNav } from '../hooks/useRowNav'
import { allowedSessionActions, SESSION_STATUS_META } from '../sessionState'

// Sessions page (web/02 plan): governed AI coding sessions. Server-side
// list (B4), deep-link inspector (B5), bulk lifecycle (UX7), catalog
// model select (UX3), consolidated detail fetch (UX6), favorites (UX13),
// status legend (UX14), visibility badge (B8).
// Canonical state definitions live in sessionState.ts (PAT-1496) — the
// same vocabulary Live uses.
const STATUS_META = SESSION_STATUS_META
const sessionFilterKeys = ['status', 'user', 'project', 'model', 'harness_id', 'repository', 'range'] as const

export default function Sessions() {
  const { favorites, sortPinnedFirst } = useFavorites('sessions')

  const fetchSessions = (q: ServerQuery) =>
    api.listSessionsPaged(buildQuery(q)).then((res: any) => {
      if (Array.isArray(res)) return res
      return { data: res.data ?? [], total: res.total ?? 0, page: res.page, size: res.size }
    })

  const [searchParams, setSearchParams] = useSearchParams()
  const sessionFilterKey = searchParams.toString()
  const initialFilters = Object.fromEntries(sessionFilterKeys.map(key => [key, searchParams.get(key) || '']).filter(([, value]) => value))
  const table = useServerTable<any>(fetchSessions, { size: 25, initialFilters })

  const [users, setUsers] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [catalogModels, setCatalogModels] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({
    user_id: '', harness_id: '', project_id: '', repository_id: '', branch: '',
    baseline_id: '', title: '', task_purpose: '', model_class: '',
  })
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [bulkAction, setBulkAction] = useState('')
  const [bulkReason, setBulkReason] = useState('')

  useEffect(() => {
    api.listUsers().then(d => setUsers(Array.isArray(d) ? d : []))
    api.listProjects().then(d => setProjects(Array.isArray(d) ? d : []))
    api.catalogModels().then(d => setCatalogModels(Array.isArray(d) ? d : []))
    api.listHarnesses().then(d => setHarnesses(Array.isArray(d) ? d : []))
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(sessionFilterKey)
    for (const key of sessionFilterKeys) {
      const value = params.get(key) || ''
      if ((table.filters[key] || '') !== value) table.setFilter(key, value)
    }
  }, [sessionFilterKey])

  const rows = sortPinnedFirst(table.rows, s => s.id)
  const { selectedIndex } = useRowNav(rows.length, () => {})

  const toggleSelect = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const setFilterAndURL = (key: string, value: string) => {
    table.setFilter(key, value)
    setSearchParams(previous => {
      const next = new URLSearchParams(previous)
      value ? next.set(key, value) : next.delete(key)
      return next
    }, { replace: true })
  }

  const openSession = async () => {
	if (!form.user_id || !form.harness_id) {
		showToast('사용자와 하네스를 선택하세요', 'error')
      return
    }
    try {
      await api.openSession(form)
      showToast('세션 생성 완료', 'success')
      setShowForm(false)
      table.reload()
    } catch (e: any) {
      showToast(e?.message || '생성 실패', 'error')
    }
  }

  const act = async (id: string, action: string) => {
    try {
		const res = await api.sessionAction(id, action)
		if (res?.cleanup_failures?.length) showToast(`세션 상태는 변경되었으나 샌드박스 ${res.cleanup_failures.length}개를 정리하지 못했습니다`, 'error')
		else showToast('완료', 'success')
      table.reload()
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
    }
  }

  const runBulk = async () => {
    if (!bulkAction || selectedIds.size === 0) return
    if (!bulkReason.trim()) {
      showToast('일괄 작업 사유를 입력하세요', 'error')
      return
    }
    try {
      const res = await api.bulkSessions([...selectedIds], bulkAction, bulkReason)
		const skipped = Array.isArray(res.skipped) ? res.skipped : []
		const cleanupFailures = Array.isArray(res.cleanup_failures) ? res.cleanup_failures : []
		if (skipped.length || cleanupFailures.length) {
			showToast(`일괄 처리 ${res.affected}건 완료 · ${skipped.length}건 건너뜀 · 정리 실패 ${cleanupFailures.length}건`, 'error')
			setSelectedIds(new Set(skipped))
		} else {
			showToast(`일괄 처리 완료 (${res.affected}건)`, 'success')
			setSelectedIds(new Set())
		}
      setBulkAction('')
      setBulkReason('')
      table.reload()
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
    }
  }

  const columns: Column<any>[] = [
    {
      key: 'select', header: '선택', className: 'w-12',
      render: (s) => <input type="checkbox" aria-label={`${s.title || s.session_id} 선택`} checked={selectedIds.has(s.id)} onChange={() => toggleSelect(s.id)} onClick={event => event.stopPropagation()} />,
      cardLabel: '선택',
    },
    {
      key: 'title', header: '세션',
      render: (s) => (
        <div className="flex items-center gap-2">
          <FavoriteStar entity="sessions" id={s.id} />
          <div className="min-w-0">
            <Link to={`/sessions/${s.id}`} className="text-xs font-semibold hover:underline text-gray-800 truncate block">
              {s.title || '제목 없음'}
            </Link>
            <div className="text-[10px] text-gray-400 font-mono truncate">{s.session_id}</div>
          </div>
        </div>
      ),
      cardLabel: '세션',
    },
    {
		key: 'who', header: '사용자 / 하네스',
      render: (s) => (
        <div className="text-[11px] space-y-0.5">
          <Link className="text-gray-600 hover:underline block" to={`/users/${s.user_id}`}>
            {users.find(u => u.id === s.user_id)?.name_ko || s.user_id?.slice(0, 8) || '—'}
          </Link>
          {s.harness_id ? <Link className="text-blue-600 hover:underline block" to={`/fleet?harness_id=${encodeURIComponent(s.harness_id)}`}>{s.harness_id.slice(0, 12)}</Link> : <div className="text-gray-400">—</div>}
        </div>
      ),
		cardLabel: '사용자',
    },
    {
      key: 'model', header: '모델',
      render: (s) => s.model_class ? <Link className="text-[11px] text-blue-600 hover:underline" to={`/models?class=${encodeURIComponent(s.model_class)}`}>{s.model_class}</Link> : <span className="text-[11px] text-gray-400">—</span>,
      cardLabel: '모델',
    },
    {
      key: 'status', header: '상태',
      render: (s) => {
        const meta = STATUS_META[s.status] || STATUS_META.pending
        return (
          <span className={`text-[10px] px-2 py-0.5 rounded-full border ${meta.badge}`}>
            {meta.dot} {meta.ko}
          </span>
        )
      },
      cardLabel: '상태',
    },
    {
      key: 'profile', header: '보호',
      render: (s) => (
        <div className="text-[10px] space-y-0.5">
          <span className="text-gray-600">{s.protection_profile || 'P0'}</span>
          {s.lease_id ? (
            <div className="text-green-600" title={s.lease_id}>lease ✓</div>
          ) : (
            <div className="text-red-500">lease ✗</div>
          )}
        </div>
      ),
      cardLabel: '보호',
    },
    {
      key: 'actions', header: '',
      render: (s) => (
        <div className="flex gap-1" onClick={e => e.stopPropagation()}>
          {allowedSessionActions(s.status).includes('pause') && <button className="text-[10px] px-2 py-1 rounded hover:bg-amber-50 text-amber-600" onClick={() => act(s.id, 'pause')}>일시정지</button>}
          {allowedSessionActions(s.status).includes('resume') && <button className="text-[10px] px-2 py-1 rounded hover:bg-green-50 text-green-600" onClick={() => act(s.id, 'resume')}>재개</button>}
          {allowedSessionActions(s.status).includes('close') && <button className="btn-xs-secondary" onClick={() => act(s.id, 'close')}>종료</button>}
          <Link className="btn-xs-secondary" to={`/sessions/${s.id}`}>검사</Link>
        </div>
      ),
      cardLabel: '작업',
    },
  ]

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">AI 세션 · Sessions</h2>
          <p className="text-[11px] text-gray-400">
            {Object.entries(STATUS_META).map(([k, v]) => <span key={k} className="mr-2">{v.dot} {v.ko}</span>)}
          </p>
        </div>
        <div className="flex gap-2 flex-wrap">
          {selectedIds.size > 0 && (
            <>
              <select className="input text-xs" value={bulkAction} onChange={e => setBulkAction(e.target.value)}>
                <option value="">일괄 작업...</option>
                <option value="close">종료</option>
                <option value="pause">일시정지</option>
                <option value="terminate">강제종료</option>
              </select>
              {bulkAction && <input className="input text-xs w-56" value={bulkReason} onChange={event => setBulkReason(event.target.value)} placeholder="작업 사유 (필수)" />}
              <button className="btn-sm btn-secondary" onClick={runBulk}>적용 ({selectedIds.size})</button>
            </>
          )}
          <button className="btn-sm btn-primary" onClick={() => setShowForm(true)}>+ 새 세션</button>
        </div>
      </div>

      <div className="flex gap-2 flex-wrap items-center">
        <input className="input text-xs w-56" placeholder="제목 / 세션 ID 검색..."
          value={table.search} onChange={e => table.setSearch(e.target.value)} />
        <select className="input text-xs w-24" value={table.filters.status || ''}
          onChange={e => setFilterAndURL('status', e.target.value)}>
          <option value="">전체 상태</option>
          {Object.entries(STATUS_META).map(([k, v]) => <option key={k} value={k}>{v.ko}</option>)}
        </select>
        <select className="input text-xs w-28" value={table.filters.model || ''}
          onChange={e => setFilterAndURL('model', e.target.value)}>
          <option value="">전체 모델</option>
          {catalogModels.map((m: any) => <option key={m.id || m.package_id} value={m.model_class || m.name}>{m.name || m.model_class}</option>)}
        </select>
        <select className="input text-xs w-32" value={table.filters.user || ''}
          onChange={e => setFilterAndURL('user', e.target.value)}>
			<option value="">전체 사용자</option>
          {users.map((u: any) => <option key={u.id} value={u.id}>{u.name_ko || u.name}</option>)}
        </select>
        <select className="input text-xs w-32" value={table.filters.project || ''}
          onChange={e => setFilterAndURL('project', e.target.value)}>
          <option value="">전체 프로젝트</option>
          {projects.map((p: any) => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}
        </select>
        <select className="input text-xs w-24" value={table.filters.range || ''}
          onChange={e => setFilterAndURL('range', e.target.value)}>
          <option value="">전체 기간</option>
          <option value="24h">24시간</option>
          <option value="7d">7일</option>
          <option value="30d">30일</option>
        </select>
        {table.loading && <span className="text-[10px] text-gray-400 animate-pulse">로딩...</span>}
        {table.error && <span className="text-[10px] text-red-500">{table.error}</span>}
      </div>

      <ResponsiveTable
        columns={columns}
        rows={rows}
        rowKey={s => s.id}
        empty={<EmptyState icon="💬" title="세션이 없습니다"
          message="새로운 AI 코딩 세션을 열어보세요." action={{ label: '+ 새 세션', onClick: () => setShowForm(true) }} />}
      />

      {table.total > table.size && (
        <div className="flex items-center justify-between text-[11px] text-gray-500">
          <span>총 {table.total}건</span>
          <div className="flex gap-1">
            <button className="btn-sm btn-secondary" disabled={table.page <= 1} onClick={() => table.setPage(p => Math.max(1, p - 1))}>이전</button>
            <span className="px-2 py-1">{table.page} / {Math.ceil(table.total / table.size)}</span>
            <button className="btn-sm btn-secondary" disabled={table.page >= Math.ceil(table.total / table.size)}
              onClick={() => table.setPage(p => p + 1)}>다음</button>
          </div>
        </div>
      )}

      <Modal open={showForm} title="새 세션" onClose={() => setShowForm(false)}
        footer={<ModalFooter onCancel={() => setShowForm(false)} onConfirm={openSession} confirmLabel="세션 열기" />}>
        <div className="space-y-3">
          <div>
			<label className="text-[10px] text-gray-500">사용자</label>
            <EntitySelect entity="user" value={form.user_id} onChange={v => setForm({ ...form, user_id: v })} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">하네스</label>
            <select className="input text-xs w-full" value={form.harness_id} onChange={e => setForm({ ...form, harness_id: e.target.value })}>
              <option value="">하네스 선택...</option>
              {harnesses.map((h: any) => <option key={h.id} value={h.harness_id || h.id}>{h.name} ({h.harness_id})</option>)}
            </select>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">프로젝트</label>
            <EntitySelect entity="project" value={form.project_id} onChange={v => setForm({ ...form, project_id: v })} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">저장소</label>
            <EntitySelect entity="repository" value={form.repository_id} onChange={v => setForm({ ...form, repository_id: v })} />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">브랜치</label>
              <input className="input text-xs w-full" value={form.branch} onChange={e => setForm({ ...form, branch: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">베이스라인 ID</label>
              <input className="input text-xs w-full" placeholder="저장소 세션은 필수" value={form.baseline_id} onChange={e => setForm({ ...form, baseline_id: e.target.value })} />
            </div>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">모델 (활성 카탈로그)</label>
            <select className="input text-xs w-full" value={form.model_class} onChange={e => setForm({ ...form, model_class: e.target.value })}>
              <option value="">모델 선택...</option>
              {catalogModels.map((m: any) => <option key={m.id || m.package_id} value={m.model_class || m.name}>{m.name || m.model_class}</option>)}
            </select>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">제목</label>
            <input className="input text-xs w-full" value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">목적</label>
            <input className="input text-xs w-full" value={form.task_purpose} onChange={e => setForm({ ...form, task_purpose: e.target.value })} />
          </div>
        </div>
      </Modal>
    </div>
  )
}
