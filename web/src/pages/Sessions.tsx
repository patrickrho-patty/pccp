import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useServerTable, buildQuery, ServerQuery } from '../hooks/useServerTable'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { EntitySelect } from '../components/EntitySelect'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { ResponsiveTable, Column } from '../components/ResponsiveTable'
import { showToast } from '../components/Toast'
import { useRowNav } from '../hooks/useRowNav'

// Sessions page (web/02 plan): governed AI coding sessions. Server-side
// list (B4), deep-link inspector (B5), bulk lifecycle (UX7), catalog
// model select (UX3), consolidated detail fetch (UX6), favorites (UX13),
// status legend (UX14), visibility badge (B8).

const STATUS_META: Record<string, { ko: string; badge: string; dot: string }> = {
  pending:    { ko: '대기',   badge: 'bg-gray-100 text-gray-600 border-gray-200',   dot: '⚪' },
  active:     { ko: '활성',   badge: 'bg-green-50 text-green-700 border-green-200', dot: '🟢' },
  idle:       { ko: '유휴',   badge: 'bg-yellow-50 text-yellow-700 border-yellow-200', dot: '🟡' },
  paused:     { ko: '일시정지', badge: 'bg-amber-50 text-amber-700 border-amber-200', dot: '⏸️' },
  closed:     { ko: '종료',   badge: 'bg-gray-100 text-gray-500 border-gray-200',    dot: '✅' },
  terminated: { ko: '강제종료', badge: 'bg-red-50 text-red-700 border-red-200',       dot: '🔴' },
}

export default function Sessions() {
  const { favorites, sortPinnedFirst } = useFavorites('sessions')

  const fetchSessions = (q: ServerQuery) =>
    api.listSessionsPaged(buildQuery(q)).then((res: any) => {
      if (Array.isArray(res)) return res
      return { data: res.data ?? [], total: res.total ?? 0, page: res.page, size: res.size }
    })

  const table = useServerTable<any>(fetchSessions, { size: 25 })

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

  useEffect(() => {
    api.listUsers().then(d => setUsers(Array.isArray(d) ? d : []))
    api.listProjects().then(d => setProjects(Array.isArray(d) ? d : []))
    api.catalogModels().then(d => setCatalogModels(Array.isArray(d) ? d : []))
    api.listHarnesses().then(d => setHarnesses(Array.isArray(d) ? d : []))
  }, [])

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

  const openSession = async () => {
    if (!form.user_id || !form.harness_id) {
      showToast('개발자와 하네스를 선택하세요', 'error')
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
      await api.sessionAction(id, action)
      showToast('완료', 'success')
      table.reload()
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
    }
  }

  const runBulk = async () => {
    if (!bulkAction || selectedIds.size === 0) return
    try {
      const res = await api.bulkSessions([...selectedIds], bulkAction)
      showToast(`일괄 처리 완료 (${res.affected}건)`, 'success')
      setSelectedIds(new Set())
      setBulkAction('')
      table.reload()
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
    }
  }

  const columns: Column<any>[] = [
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
      key: 'who', header: '개발자 / 하네스',
      render: (s) => (
        <div className="text-[11px] space-y-0.5">
          <Link className="text-gray-600 hover:underline block" to={`/users/${s.user_id}`}>
            {users.find(u => u.id === s.user_id)?.name_ko || s.user_id?.slice(0, 8) || '—'}
          </Link>
          <div className="text-gray-400">{s.harness_id?.slice(0, 12) || '—'}</div>
        </div>
      ),
      cardLabel: '개발자',
    },
    {
      key: 'model', header: '모델',
      render: (s) => <span className="text-[11px] text-gray-600">{s.model_class || '—'}</span>,
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
          {s.status === 'active' && <button className="text-[10px] px-2 py-1 rounded hover:bg-amber-50 text-amber-600" onClick={() => act(s.id, 'pause')}>일시정지</button>}
          {(s.status === 'paused' || s.status === 'idle') && <button className="text-[10px] px-2 py-1 rounded hover:bg-green-50 text-green-600" onClick={() => act(s.id, 'resume')}>재개</button>}
          {s.status !== 'closed' && s.status !== 'terminated' && <button className="btn-xs-secondary" onClick={() => act(s.id, 'close')}>종료</button>}
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
          onChange={e => table.setFilter('status', e.target.value)}>
          <option value="">전체 상태</option>
          {Object.entries(STATUS_META).map(([k, v]) => <option key={k} value={k}>{v.ko}</option>)}
        </select>
        <select className="input text-xs w-28" value={table.filters.model || ''}
          onChange={e => table.setFilter('model', e.target.value)}>
          <option value="">전체 모델</option>
          {catalogModels.map((m: any) => <option key={m.id || m.package_id} value={m.model_class || m.name}>{m.name || m.model_class}</option>)}
        </select>
        <select className="input text-xs w-32" value={table.filters.user || ''}
          onChange={e => table.setFilter('user', e.target.value)}>
          <option value="">전체 개발자</option>
          {users.map((u: any) => <option key={u.id} value={u.id}>{u.name_ko || u.name}</option>)}
        </select>
        <select className="input text-xs w-32" value={table.filters.project || ''}
          onChange={e => table.setFilter('project', e.target.value)}>
          <option value="">전체 프로젝트</option>
          {projects.map((p: any) => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}
        </select>
        <select className="input text-xs w-24" value={table.filters.range || ''}
          onChange={e => table.setFilter('range', e.target.value)}>
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
            <label className="text-[10px] text-gray-500">개발자</label>
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
