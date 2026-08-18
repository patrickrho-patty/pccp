import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useServerTable, buildQuery, ServerQuery } from '../hooks/useServerTable'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { ResponsiveTable, Column } from '../components/ResponsiveTable'
import { showToast } from '../components/Toast'

// Fleet page (web/09 plan): live fleet operations — containment happens
// here. Server-side inventory query (A12), bulk actions (A5), 2-step
// scoped lockdown with impact preview (A11), required reasons, change
// freeze (A3), force-version floor (A4), per-harness action history (A6),
// forensic snapshot download (A7), approvals queue (A9), live status.

const STATUS_KO: Record<string, string> = {
  pending: '대기', enrolled: '등록됨', active: '활성', quarantined: '격리', revoked: '해지',
}
const STATUS_BADGE: Record<string, string> = {
  enrolled: 'bg-green-50 text-green-700 border-green-200',
  active: 'bg-green-50 text-green-700 border-green-200',
  pending: 'bg-gray-100 text-gray-500 border-gray-200',
  quarantined: 'bg-amber-50 text-amber-700 border-amber-200',
  revoked: 'bg-red-50 text-red-700 border-red-200',
}
const RISK_KO: Record<string, string> = { low: '낮음', elevated: '주의', high: '높음', critical: '심각' }

const DESTRUCTIVE = new Set([
  'revoke_harness_certificate', 'quarantine_device', 'terminate_session', 'emergency_lockdown',
  'isolate_sandbox', 'suspend_model', 'invalidate_privileges', 'pause_execution', 'require_upgrade',
])

export default function Fleet() {
  const { favorites, sortPinnedFirst } = useFavorites('fleet')

  const fetchInventory = (q: ServerQuery) =>
    api.listFleetInventory(buildQuery(q)).then((res: any) => {
      if (Array.isArray(res)) return res
      return { data: res.data ?? [], total: res.total ?? 0, page: res.page, size: res.size }
    })
  // Deep link (PAT-1496): /fleet?harness=<id> seeds the search with the
  // exact harness — the server-side search matches HarnessID. Passing it
  // as the table's initialSearch keeps the visible input and the data
  // filter in sync from the first render.
  const [searchParams] = useSearchParams()
  const deepHarness = searchParams.get('harness') || ''
  const table = useServerTable<any>(fetchInventory, { size: 25, initialSearch: deepHarness })

  const [status, setStatus] = useState<any>(null)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [actionTarget, setActionTarget] = useState<any>(null) // {harness, action}
  const [reason, setReason] = useState('')
  const [bulkAction, setBulkAction] = useState('')
  const [lockdownOpen, setLockdownOpen] = useState(false)
  const [lockdownScope, setLockdownScope] = useState('org')
  const [impact, setImpact] = useState<any>(null)
  const [lockdownReason, setLockdownReason] = useState('')
  const [lockdownStep, setLockdownStep] = useState(1)
  const [freezeOpen, setFreezeOpen] = useState(false)
  const [freezeForm, setFreezeForm] = useState({ reason: '', reason_ko: '', affected_repos: '' })
  const [versionOpen, setVersionOpen] = useState(false)
  const [versionForm, setVersionForm] = useState({ min_version: '', release_ring: 'stable', deadline: '', reason: '' })
  const [approvals, setApprovals] = useState<any[]>([])
  const [historyTarget, setHistoryTarget] = useState<any>(null)
  const [history, setHistory] = useState<any[]>([])

  const refreshStatus = useCallback(() => {
    api.fleetStatus().then(setStatus).catch(() => {})
    api.fleetApprovals().then(d => setApprovals(Array.isArray(d) ? d : [])).catch(() => {})
  }, [])
  useEffect(() => {
    refreshStatus()
    const t = setInterval(refreshStatus, 15000)
    return () => clearInterval(t)
  }, [refreshStatus])

  const rows = sortPinnedFirst(table.rows, h => h.harness.id)

  const toggleSelect = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const runAction = async () => {
    if (!actionTarget) return
    if (!reason.trim()) {
      showToast('모든 파괴적 작업에는 사유가 필요합니다', 'error')
      return
    }
    try {
      await api.fleetAction({
        harness_id: actionTarget.harness.harness_id || actionTarget.harness.id,
        action: actionTarget.action,
        reason,
      })
      showToast('작업 실행 완료', 'success')
      setActionTarget(null)
      setReason('')
      table.reload()
      refreshStatus()
    } catch (e: any) {
      showToast(e?.message || '작업 실패', 'error')
    }
  }

  const runBulk = async () => {
    if (!bulkAction || selectedIds.size === 0) return
    if (!reason.trim()) {
      showToast('사유가 필요합니다', 'error')
      return
    }
    try {
      const res = await api.fleetBulkAction([...selectedIds], bulkAction, reason)
      showToast(`일괄 작업 완료 (${res.executed}건${res.failed ? `, 실패 ${res.failed}` : ''})`, res.failed ? 'error' : 'success')
      setSelectedIds(new Set())
      setBulkAction('')
      setReason('')
      table.reload()
      refreshStatus()
    } catch (e: any) {
      showToast(e?.message || '실패', 'error')
    }
  }

  const previewLockdown = async (scope: string) => {
    try {
      const res = await api.fleetImpact('emergency_lockdown', scope)
      setImpact(res)
    } catch { setImpact(null) }
  }
  const openLockdown = async () => {
    setLockdownStep(1)
    setLockdownScope('org')
    await previewLockdown('org')
    setLockdownOpen(true)
  }
  const confirmLockdown = async () => {
    if (!lockdownReason.trim()) {
      showToast('사유가 필요합니다', 'error')
      return
    }
    try {
      await api.fleetAction({ harness_id: '', action: 'emergency_lockdown', reason: lockdownReason })
      showToast('긴급 잠금 실행 완료 — 모든 세션이 종료되었습니다', 'success')
      setLockdownOpen(false)
      setLockdownReason('')
      table.reload()
      refreshStatus()
    } catch (e: any) {
      showToast(e?.message || '잠금 실패', 'error')
    }
  }

  const runFreeze = async () => {
    if (!freezeForm.reason.trim()) {
      showToast('사유가 필요합니다', 'error')
      return
    }
    try {
      await api.fleetFreeze(freezeForm.reason, freezeForm.reason_ko, freezeForm.affected_repos.split(',').map(s => s.trim()).filter(Boolean))
      showToast('변경 중단 모드 시작', 'success')
      setFreezeOpen(false)
      setFreezeForm({ reason: '', reason_ko: '', affected_repos: '' })
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const runForceVersion = async () => {
    if (!versionForm.min_version.trim()) {
      showToast('최소 버전이 필요합니다', 'error')
      return
    }
    try {
      await api.fleetForceVersion(versionForm.min_version, versionForm.release_ring, versionForm.deadline, versionForm.reason)
      showToast('버전 하한 설정 완료', 'success')
      setVersionOpen(false)
      setVersionForm({ min_version: '', release_ring: 'stable', deadline: '', reason: '' })
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const loadHistory = async (h: any) => {
    setHistoryTarget(h)
    try {
      const d = await api.fleetActionHistory(h.harness_id || h.id)
      setHistory(Array.isArray(d) ? d : [])
    } catch { setHistory([]) }
  }

  const downloadSnapshot = async (h: any) => {
    const token = localStorage.getItem('pccp_token')
    try {
      const resp = await fetch(`/api/fleet/harnesses/${h.id}/snapshot`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      if (!resp.ok) throw new Error('snapshot failed')
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `harness-forensic.json`
      a.click()
      URL.revokeObjectURL(url)
    } catch (e: any) { showToast(e?.message || '스냅샷 실패', 'error') }
  }

  const columns: Column<any>[] = [
    {
      key: 'harness', header: '하네스',
      render: (item) => {
        const h = item.harness
        return (
          <div className="flex items-center gap-2">
            <FavoriteStar entity="fleet" id={h.id} />
            <div className="min-w-0">
              <Link to={`/harnesses/${h.id}`} className="text-xs font-semibold hover:underline text-gray-800 truncate block">
                {h.name || h.harness_id}
              </Link>
              <div className="text-[10px] text-gray-400 font-mono truncate">{h.harness_id}</div>
              <div className="text-[10px] text-gray-400">{h.binary_version || '버전 없음'}{item.is_active ? ' · 온라인' : ' · 오프라인'}</div>
            </div>
          </div>
        )
      },
      cardLabel: '하네스',
    },
    {
      key: 'user', header: '개발자',
      render: (item) => item.user ? (
        <Link to={`/users/${item.user.id}`} className="text-[11px] text-gray-600 hover:underline">
          {item.user.name_ko || item.user.name}
        </Link>
      ) : <span className="text-[11px] text-gray-400">—</span>,
      cardLabel: '개발자',
    },
    {
      key: 'status', header: '상태',
      render: (item) => (
        <span className={`text-[10px] px-2 py-0.5 rounded-full border ${STATUS_BADGE[item.harness.status] || ''}`}>
          {STATUS_KO[item.harness.status] || item.harness.status}
        </span>
      ),
      cardLabel: '상태',
    },
    {
      key: 'risk', header: '위험',
      render: (item) => (
        <span className={`text-[10px] px-2 py-0.5 rounded-full border ${item.harness.risk_state === 'high' || item.harness.risk_state === 'critical' ? 'bg-red-50 text-red-700 border-red-200' : 'bg-gray-100 text-gray-500 border-gray-200'}`}>
          {RISK_KO[item.harness.risk_state] || item.harness.risk_state || '—'}
        </span>
      ),
      cardLabel: '위험',
    },
    {
      key: 'counts', header: '세션 / 승인 / 발견',
      render: (item) => (
        <div className="text-[11px] space-x-2">
          <Link className="text-blue-600 hover:underline" to={`/sessions?user=${item.user?.id || ''}`}>세션 {(item.sessions || []).length}</Link>
          <span className="text-gray-600">승인 {item.open_approvals || 0}</span>
          <span className="text-gray-600">발견 {item.security_findings || 0}</span>
        </div>
      ),
      cardLabel: '집계',
    },
    {
      key: 'actions', header: '작업',
      render: (item) => {
        const h = item.harness
        return (
          <div className="flex gap-1 flex-wrap" onClick={e => e.stopPropagation()}>
            {h.status !== 'revoked' && (
              <button className="text-[10px] px-2 py-1 rounded hover:bg-amber-50 text-amber-600"
                onClick={() => { setActionTarget({ harness: h, action: 'quarantine_device' }); setReason('') }}>격리</button>
            )}
            {h.status !== 'revoked' && (
              <button className="btn-xs-danger"
                onClick={() => { setActionTarget({ harness: h, action: 'revoke_harness_certificate' }); setReason('') }}>인증 해지</button>
            )}
            <button className="btn-xs-secondary" onClick={() => loadHistory(h)}>이력</button>
            <button className="btn-xs-secondary" onClick={() => downloadSnapshot(h)}>스냅샷</button>
          </div>
        )
      },
      cardLabel: '작업',
    },
  ]

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">플릿 관리 · Fleet</h2>
          <p className="text-[11px] text-gray-400">
            {status ? `전체 ${status.total} · 활성 ${status.active} · 격리 ${status.quarantined} · 하트비트 지연 ${status.stale_heartbeats}` : '...'}
            <span className="ml-2 text-green-600">● 15초 자동 갱신</span>
          </p>
        </div>
        <div className="flex gap-2 flex-wrap">
          <button className="btn-sm btn-secondary" onClick={() => setFreezeOpen(true)}>변경 중단</button>
          <button className="btn-sm btn-secondary" onClick={() => setVersionOpen(true)}>버전 하한</button>
          <button className="btn-sm btn-danger" onClick={openLockdown}>⚠ 긴급 잠금</button>
        </div>
      </div>

      {/* Filters + bulk */}
      <div className="flex gap-2 flex-wrap items-center">
        <input className="input text-xs w-56" placeholder="이름 / ID / 이메일 검색..."
          value={table.search} onChange={e => table.setSearch(e.target.value)} />
        <select className="input text-xs w-28" value={table.filters.status || ''}
          onChange={e => table.setFilter('status', e.target.value)}>
          <option value="">전체 상태</option>
          {Object.entries(STATUS_KO).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <select className="input text-xs w-24" value={table.filters.risk || ''}
          onChange={e => table.setFilter('risk', e.target.value)}>
          <option value="">전체 위험</option>
          {Object.entries(RISK_KO).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <select className="input text-xs w-28" value={table.filters.version || ''}
          onChange={e => table.setFilter('version', e.target.value)}>
          <option value="">전체 버전</option>
          <option value="stale">버전 미기재/지연</option>
        </select>
        {selectedIds.size > 0 && (
          <>
            <select className="input text-xs" value={bulkAction} onChange={e => setBulkAction(e.target.value)}>
              <option value="">일괄 작업 ({selectedIds.size})...</option>
              <option value="quarantine_device">격리</option>
              <option value="revoke_harness_certificate">인증 해지</option>
              <option value="pause_execution">실행 일시정지</option>
              <option value="require_upgrade">업그레이드 요구</option>
            </select>
            {bulkAction && (
              <>
                <input className="input text-xs w-40" placeholder="사유 (필수)" value={reason} onChange={e => setReason(e.target.value)} />
                <button className="btn-sm btn-primary" onClick={runBulk}>일괄 적용</button>
              </>
            )}
          </>
        )}
        {table.loading && <span className="text-[10px] text-gray-400 animate-pulse">로딩...</span>}
        {table.error && <span className="text-[10px] text-red-500">{table.error}</span>}
      </div>

      <ResponsiveTable
        columns={columns}
        rows={rows}
        rowKey={item => item.harness.id}
        empty={<EmptyState icon="🛰️" title="하네스가 없습니다"
          message="등록된 하네스가 나타나면 여기에서 관리할 수 있습니다." />}
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

      {/* Pending approvals (A9) */}
      {approvals.length > 0 && (
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">대기 중 승인 ({approvals.length})</h3>
          <div className="space-y-1">
            {approvals.slice(0, 10).map((a: any) => (
              <div key={a.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
                <span className="text-gray-700">{a.approval_type} — {a.session_id?.slice(0, 12) || ''}</span>
                <span className="text-gray-400">{a.decision}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Per-harness action history (A6) */}
      <Modal open={!!historyTarget} title={`작업 이력 — ${historyTarget?.harness?.name || historyTarget?.harness_id || ''}`}
        onClose={() => setHistoryTarget(null)} size="lg"
        footer={<ModalFooter onCancel={() => setHistoryTarget(null)} onConfirm={() => setHistoryTarget(null)} confirmLabel="닫기" />}>
        <div className="space-y-1 max-h-80 overflow-auto">
          {history.length === 0 && <p className="text-[11px] text-gray-400">작업 이력 없음</p>}
          {history.map((e: any) => (
            <div key={e.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700">{e.action}</span>
              <span className="text-gray-400 max-w-48 truncate">{e.details}</span>
              <span className="text-gray-400">{(e.occurred_at || '').slice(0, 16)}</span>
            </div>
          ))}
        </div>
      </Modal>

      {/* Single action reason modal */}
      <Modal open={!!actionTarget}
        title={`${actionTarget?.action || ''} — ${actionTarget?.harness?.name || actionTarget?.harness?.harness_id || ''}`}
        onClose={() => setActionTarget(null)}
        footer={<ModalFooter onCancel={() => setActionTarget(null)} onConfirm={runAction} confirmLabel="실행" danger />}>
        <div className="space-y-2">
          <p className="text-[11px] text-gray-500">파괴적 작업입니다. 사유는 감사 로그에 영구 기록되며, 릴레이로 전파됩니다.</p>
          <textarea className="input text-xs w-full" rows={3} placeholder="사유 (필수)"
            value={reason} onChange={e => setReason(e.target.value)} />
        </div>
      </Modal>

      {/* 2-step lockdown (A11) */}
      <Modal open={lockdownOpen} title="긴급 잠금 — 2단계 확인"
        onClose={() => setLockdownOpen(false)}
        footer={lockdownStep === 1 ? (
          <ModalFooter onCancel={() => setLockdownOpen(false)} onConfirm={async () => { setLockdownStep(2); await previewLockdown(lockdownScope) }} confirmLabel="다음" danger />
        ) : (
          <ModalFooter onCancel={() => { setLockdownOpen(false); setLockdownStep(1) }} onConfirm={confirmLockdown} confirmLabel="잠금 확정" danger />
        )}>
        {lockdownStep === 1 ? (
          <div className="space-y-2">
            <p className="text-[11px] text-gray-500">전 조직의 모든 하네스와 활성 세션이 즉시 종료됩니다. 먼저 범위를 확인하세요.</p>
            <select className="input text-xs w-full" value={lockdownScope}
              onChange={async e => { setLockdownScope(e.target.value); await previewLockdown(e.target.value) }}>
              <option value="org">전 조직</option>
              <option value="project">프로젝트</option>
            </select>
            {impact && (
              <div className="text-[11px] text-red-600 bg-red-50 rounded p-2">
                영향 예측: 하네스 {impact.affected_harnesses} · 활성 세션 {impact.affected_sessions}
              </div>
            )}
          </div>
        ) : (
          <div className="space-y-2">
            <p className="text-[11px] text-red-600 font-semibold">확정 시 되돌릴 수 없습니다 (이력은 남습니다).</p>
            <textarea className="input text-xs w-full" rows={3} placeholder="사유 (필수)"
              value={lockdownReason} onChange={e => setLockdownReason(e.target.value)} />
          </div>
        )}
      </Modal>

      {/* Change freeze (A3) */}
      <Modal open={freezeOpen} title="변경 중단 모드 (§33.13)"
        onClose={() => setFreezeOpen(false)}
        footer={<ModalFooter onCancel={() => setFreezeOpen(false)} onConfirm={runFreeze} confirmLabel="시작" danger />}>
        <div className="space-y-2">
          <div>
            <label className="text-[10px] text-gray-500">사유 (EN)</label>
            <input className="input text-xs w-full" value={freezeForm.reason} onChange={e => setFreezeForm({ ...freezeForm, reason: e.target.value })} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">사유 (KO)</label>
            <input className="input text-xs w-full" value={freezeForm.reason_ko} onChange={e => setFreezeForm({ ...freezeForm, reason_ko: e.target.value })} />
          </div>
          <div>
            <label className="text-[10px] text-gray-500">대상 저장소 (쉼표 구분, 비우면 전체)</label>
            <input className="input text-xs w-full" value={freezeForm.affected_repos} onChange={e => setFreezeForm({ ...freezeForm, affected_repos: e.target.value })} />
          </div>
        </div>
      </Modal>

      {/* Force version (A4) */}
      <Modal open={versionOpen} title="하네스 버전 하한 (§33.10)"
        onClose={() => setVersionOpen(false)}
        footer={<ModalFooter onCancel={() => setVersionOpen(false)} onConfirm={runForceVersion} confirmLabel="설정" />}>
        <div className="space-y-2">
          <div>
            <label className="text-[10px] text-gray-500">최소 버전 (예: 0.9.0)</label>
            <input className="input text-xs w-full" value={versionForm.min_version} onChange={e => setVersionForm({ ...versionForm, min_version: e.target.value })} />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">릴리스 링</label>
              <select className="input text-xs w-full" value={versionForm.release_ring} onChange={e => setVersionForm({ ...versionForm, release_ring: e.target.value })}>
                <option value="stable">stable</option>
                <option value="beta">beta</option>
                <option value="canary">canary</option>
              </select>
            </div>
            <div>
              <label className="text-[10px] text-gray-500">마감일</label>
              <input className="input text-xs w-full" type="date" value={versionForm.deadline} onChange={e => setVersionForm({ ...versionForm, deadline: e.target.value })} />
            </div>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">사유</label>
            <input className="input text-xs w-full" value={versionForm.reason} onChange={e => setVersionForm({ ...versionForm, reason: e.target.value })} />
          </div>
        </div>
      </Modal>
    </div>
  )
}
