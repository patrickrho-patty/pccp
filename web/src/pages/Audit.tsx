import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useServerTable, buildQuery, ServerQuery } from '../hooks/useServerTable'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import {
  auditEventView, auditCategoryOf, auditCategory, AUDIT_CATEGORY_OPTIONS,
  groupAuditBursts, AuditView,
} from '../evidenceView'

// Audit page (web/17 plan): server-side query (A), tamper-evidence
// verification (B), legal holds (C), payload drill-down (D), SIEM
// forwarding + evidence bundle + live tail (E).
//
// PAT-1503: rows render human-readable Korean summaries derived from the
// shared evidence registry (evidenceView.ts) instead of raw event/action/
// resource keys. Faceted filters come from the canonical event taxonomy,
// repeated bursts are grouped without losing underlying records, details
// are accessible dialog semantics with exact actor/object links, and the
// raw payload + hash-chain data stays in expandable technical evidence.

const FILTER_KEYS = ['category', 'actor', 'result', 'integrity'] as const

export default function Audit() {
  const fetchAudit = (q: ServerQuery) =>
    api.listAuditPaged(buildQuery(q)).then((res: any) => {
      if (Array.isArray(res)) return res
      return { data: res.data ?? [], total: res.total ?? 0, page: res.page, size: res.size }
    })
  const table = useServerTable<any>(fetchAudit, { size: 50 })

  const [verify, setVerify] = useState<any>(null)
  const [holds, setHolds] = useState<any[]>([])
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [payloadTarget, setPayloadTarget] = useState<any>(null)
  const [expandedBursts, setExpandedBursts] = useState<Set<number>>(new Set())
  const [holdOpen, setHoldOpen] = useState(false)
  const [holdForm, setHoldForm] = useState({ resource_type: 'session', resource_id: '', reason: '' })
  const [siemOpen, setSiemOpen] = useState(false)
  const [siemForm, setSiemForm] = useState({ webhook: '', secret: '' })

  // Shareable, refresh/back-safe filters: seed from URL on mount and
  // mirror every change back into the URL without a reload.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    for (const k of FILTER_KEYS) {
      const v = params.get(k)
      if (v) table.setFilter(k, v)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    for (const k of FILTER_KEYS) {
      const v = table.filters[k]
      if (v) params.set(k, v)
      else params.delete(k)
    }
    const qs = params.toString()
    window.history.replaceState(null, '', window.location.pathname + (qs ? `?${qs}` : ''))
  }, [table.filters])

  const load = () => {
    api.listAuditHolds().then((d: any[]) => setHolds(Array.isArray(d) ? d : [])).catch(() => setHolds([]))
    api.auditSIEMConfig().then((c: any) => setSiemForm({ webhook: c?.webhook || '', secret: '' })).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const runVerify = async () => {
    try {
      const res = await api.verifyAuditChain()
      setVerify(res)
      showToast(res?.valid !== false ? '해시 체인 검증 완료' : '체인 불일치 발견!', res?.valid !== false ? 'success' : 'error')
    } catch (e: any) { showToast(e?.message || '검증 실패', 'error') }
  }

  const toggleSelect = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const downloadBundle = async () => {
    if (selectedIds.size === 0) return
    try {
      const token = sessionStorage.getItem('pccp_token')
      const resp = await fetch('/api/audit/evidence-bundle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
        body: JSON.stringify({ ids: [...selectedIds] }),
      })
      if (!resp.ok) throw new Error('bundle failed')
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'audit-evidence-bundle.json'
      a.click()
      URL.revokeObjectURL(url)
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const placeHold = async () => {
    if (!holdForm.resource_id.trim()) {
      showToast('리소스 ID가 필요합니다', 'error')
      return
    }
    try {
      await api.placeAuditHold(holdForm.resource_type, holdForm.resource_id, holdForm.reason)
      showToast('법적 보류 설정 완료', 'success')
      setHoldOpen(false)
      setHoldForm({ resource_type: 'session', resource_id: '', reason: '' })
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const liftHold = async (h: any) => {
    try {
      await api.liftAuditHold(h.id, '관리자 해제')
      showToast('보류 해제 완료', 'success')
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const saveSIEM = async () => {
    try {
      await api.putAuditSIEMConfig(siemForm.webhook, siemForm.secret)
      showToast('SIEM 전달 설정 완료 — 새 이벤트가 주기적으로 전달됩니다', 'success')
      setSiemOpen(false)
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  // Group repeated system bursts (same event type + actor + minute) into a
  // single collapsible row; every underlying record stays intact.
  const { rows: groupedRows } = groupAuditBursts(table.rows || [])
  const uncollapsedTotal = groupedRows.reduce((n, r) => n + (r.count || 1), 0)

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-start justify-between gap-2 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">감사 로그 · Audit</h2>
          <p className="text-[11px] text-gray-400">서버 측 필터/페이지네이션 · 해시 체인 무결성 · 법적 보류</p>
        </div>
        <div className="flex gap-2 flex-wrap">
          {selectedIds.size > 0 && (
            <button className="btn-sm btn-secondary" onClick={downloadBundle}>증거 번들 ({selectedIds.size})</button>
          )}
          <button className="btn-sm btn-secondary" onClick={() => setHoldOpen(true)}>법적 보류</button>
          <button className="btn-sm btn-secondary" onClick={() => setSiemOpen(true)}>SIEM 전달</button>
          <button className="btn-sm btn-primary" onClick={runVerify}>체인 검증</button>
        </div>
      </div>

      {verify && (
        <div className={`card p-3 text-[11px] ${verify.valid === false ? 'text-red-600' : 'text-green-700'}`}>
          체인 검증: {verify.valid === false ? '불일치!' : '무결함'} — {verify.checked || verify.total || verify.events || 0} 이벤트 확인
        </div>
      )}

      {/* Faceted filters derived from the canonical event taxonomy (PAT-1503) */}
      <div className="flex gap-2 flex-wrap items-center">
        <input className="input text-xs w-56" placeholder="검색 (액션/리소스)..." value={table.search}
          onChange={e => table.setSearch(e.target.value)} />
        <select className="input text-xs w-28" value={table.filters.category || ''}
          onChange={e => table.setFilter('category', e.target.value)} aria-label="이벤트 분류 필터">
          <option value="">전체 분류</option>
          {AUDIT_CATEGORY_OPTIONS.map(c => (
            <option key={c.id} value={c.id}>{c.icon} {c.labelKo}</option>
          ))}
        </select>
        <input className="input text-xs w-40" placeholder="주체 (actor ID/유형)..."
          value={table.filters.actor || ''} onChange={e => table.setFilter('actor', e.target.value)} aria-label="주체 필터" />
        <select className="input text-xs w-28" value={table.filters.result || ''}
          onChange={e => table.setFilter('result', e.target.value)} aria-label="결과 필터">
          <option value="">전체 결과</option>
          <option value="success">성공</option>
          <option value="denied">거부</option>
          <option value="failed">실패</option>
        </select>
        <select className="input text-xs w-28" value={table.filters.integrity || ''}
          onChange={e => table.setFilter('integrity', e.target.value)} aria-label="무결성/보류 필터">
          <option value="">전체 무결성</option>
          <option value="hold">법적 보류</option>
          <option value="degraded">무결성 저하</option>
          <option value="verified">체인 검증됨</option>
        </select>
        {table.loading && <span className="text-[10px] text-gray-400 animate-pulse">로딩...</span>}
      </div>

      <div className="space-y-1">
        {groupedRows.length === 0 && <EmptyState icon="📜" title="감사 이벤트가 없습니다" />}
        {groupedRows.map((g: any, idx: number) => {
          const v: AuditView = auditEventView(g)
          const isBurst = (g.count || 1) > 1
          const expanded = expandedBursts.has(idx)
          return (
            <div key={g.id + '-' + idx} className="border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">
              <div className="flex items-center gap-2 text-[11px]">
                <input type="checkbox" checked={selectedIds.has(g.id)} onChange={() => toggleSelect(g.id)}
                  aria-label={`${v.title} 선택`} />
                <span className="text-gray-400 w-6 text-center shrink-0">{v.categoryIcon}</span>
                <span className="text-gray-700 flex-1 truncate">{v.title}</span>
                {v.legalHold && <span className="text-[9px] px-1.5 py-0.5 rounded bg-purple-50 text-purple-700 border border-purple-200" title="법적 보류">⛔ 보류</span>}
                {v.integrity === 'degraded' && <span className="text-[9px] px-1.5 py-0.5 rounded bg-red-50 text-red-700 border border-red-200" title="무결성 저하">⚠ 체인 불일치</span>}
                <span className={`text-[10px] px-2 py-0.5 rounded-full border ${v.color}`}>{v.icon} {v.outcome}</span>
                <span className="text-gray-400 w-28 shrink-0">{(g.occurred_at || '').slice(0, 16).replace('T', ' ')}</span>
                {isBurst && (
                  <button className="text-[10px] px-2 py-0.5 rounded-full bg-gray-100 text-gray-600 hover:bg-gray-200"
                    onClick={() => setExpandedBursts(prev => {
                      const next = new Set(prev)
                      if (next.has(idx)) next.delete(idx); else next.add(idx)
                      return next
                    })}
                    aria-expanded={expanded} aria-label={`${v.title} 반복 ${g.count}건 접기/펼치기`}>
                    {expanded ? '접기' : `× ${g.count}`}
                  </button>
                )}
                <button className="text-[10px] px-2 py-0.5 rounded hover:bg-blue-50 text-blue-600" onClick={() => setPayloadTarget(expanded ? g.items[g.items.length - 1] : g)}>상세</button>
              </div>
              {isBurst && expanded && (
                <div className="mt-1 ml-10 space-y-0.5 border-l-2 border-gray-100 pl-3">
                  {g.items.map((ev: any) => {
                    const sub = auditEventView(ev)
                    return (
                      <div key={ev.id} className="flex items-center gap-2 text-[10px] text-gray-500">
                        <span className="text-gray-400 w-28 shrink-0">{(ev.occurred_at || '').slice(0, 16).replace('T', ' ')}</span>
                        <span className="text-gray-600 flex-1 truncate">{sub.title}</span>
                        <span className={`text-[9px] px-1.5 py-0.5 rounded-full border ${sub.color}`}>{sub.outcome}</span>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {table.total > table.size && (
        <div className="flex items-center justify-between text-[11px] text-gray-500">
          <span>총 {table.total}건{uncollapsedTotal < table.total ? ` · 반복 그룹 후 ${uncollapsedTotal}행` : ''}</span>
          <div className="flex gap-1">
            <button className="btn-sm btn-secondary" disabled={table.page <= 1} onClick={() => table.setPage(p => Math.max(1, p - 1))}>이전</button>
            <span className="px-2 py-1">{table.page} / {Math.ceil(table.total / table.size)}</span>
            <button className="btn-sm btn-secondary" disabled={table.page >= Math.ceil(table.total / table.size)}
              onClick={() => table.setPage(p => p + 1)}>다음</button>
          </div>
        </div>
      )}

      {holds.length > 0 && (
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">법적 보류 ({holds.length})</h3>
          {holds.map((h: any) => (
            <div key={h.id} className="flex justify-between text-[11px] border-b border-gray-50 py-1">
              <span className="text-gray-700">{h.resource_type}:{h.resource_id} — {h.reason}</span>
              <span className="text-gray-400">{h.status}</span>
              {h.status === 'active' && <button className="text-[10px] text-red-600" onClick={() => liftHold(h)}>해제</button>}
            </div>
          ))}
        </div>
      )}

      {/* Payload drill-down (D) — accessible dialog, exact links, expandable technical evidence */}
      <Modal open={!!payloadTarget} title={`이벤트 상세 — ${payloadTarget ? auditCategory(auditCategoryOf(payloadTarget.event_type)).labelKo + ' ' + auditCategoryOf(payloadTarget.event_type) : ''}`}
        onClose={() => setPayloadTarget(null)} size="lg"
        footer={<ModalFooter onCancel={() => setPayloadTarget(null)} onConfirm={() => setPayloadTarget(null)} confirmLabel="닫기" />}>
        {payloadTarget && <AuditDetail event={payloadTarget} />}
      </Modal>

      {/* Legal hold (C) */}
      <Modal open={holdOpen} title="법적 보류 설정 (§40.5)"
        onClose={() => setHoldOpen(false)}
        footer={<ModalFooter onCancel={() => setHoldOpen(false)} onConfirm={placeHold} confirmLabel="설정" />}>
        <div className="space-y-2">
          <select className="input text-xs w-full" value={holdForm.resource_type} onChange={e => setHoldForm({ ...holdForm, resource_type: e.target.value })}>
            <option value="session">session</option>
            <option value="user">user</option>
            <option value="repository">repository</option>
            <option value="audit_event">audit_event</option>
          </select>
          <input className="input text-xs w-full" placeholder="리소스 ID" value={holdForm.resource_id} onChange={e => setHoldForm({ ...holdForm, resource_id: e.target.value })} />
          <textarea className="input text-xs w-full" rows={2} placeholder="사유" value={holdForm.reason} onChange={e => setHoldForm({ ...holdForm, reason: e.target.value })} />
        </div>
      </Modal>

      {/* SIEM config (E) */}
      <Modal open={siemOpen} title="SIEM 전달 설정 (§32.4)"
        onClose={() => setSiemOpen(false)}
        footer={<ModalFooter onCancel={() => setSiemOpen(false)} onConfirm={saveSIEM} confirmLabel="저장" />}>
        <div className="space-y-2">
          <p className="text-[11px] text-gray-500">새 감사 이벤트가 HMAC 서명(X-PCCP-Signature)과 함께 주기적으로 전달됩니다.</p>
          <input className="input text-xs w-full" placeholder="SIEM 웹훅 URL" value={siemForm.webhook} onChange={e => setSiemForm({ ...siemForm, webhook: e.target.value })} />
          <input className="input text-xs w-full" type="password" placeholder="HMAC 시크릿" value={siemForm.secret} onChange={e => setSiemForm({ ...siemForm, secret: e.target.value })} />
        </div>
      </Modal>
    </div>
  )
}

/** Human-readable audit detail with exact actor/object links and
 *  expandable raw payload + hash-chain evidence (PAT-1503). */
function AuditDetail({ event }: { event: any }) {
  const v = auditEventView(event)
  const [showRaw, setShowRaw] = useState(false)
  return (
    <div className="space-y-2 text-[11px]">
      <div className={`text-[11px] px-2 py-1.5 rounded-lg border ${v.color} flex items-center gap-2`}>
        <span>{v.icon}</span><span>{v.title}</span>
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
        <div><span className="text-gray-400 block text-[10px]">분류</span><span>{v.categoryIcon} {v.categoryLabelKo} · {v.categoryId}</span></div>
        <div><span className="text-gray-400 block text-[10px]">결과</span><span className={v.color}>{v.outcome}</span></div>
        <div>
          <span className="text-gray-400 block text-[10px]">주체 (actor)</span>
          {v.actorRoute
            ? <Link to={v.actorRoute} className="text-blue-600 hover:underline">{v.actorLabel} {event.actor_id ? <span className="font-mono">{event.actor_id}</span> : ''}</Link>
            : <span>{v.actorLabel} {event.actor_id ? <span className="font-mono">{event.actor_id}</span> : ''}</span>}
        </div>
        <div>
          <span className="text-gray-400 block text-[10px]">대상 (object)</span>
          {v.resourceRoute
            ? <Link to={v.resourceRoute} className="text-blue-600 hover:underline">{v.resourceLabel}{event.resource_id ? <span className="font-mono"> {event.resource_id}</span> : ''}</Link>
            : <span>{v.resourceLabel}{event.resource_id ? <span className="font-mono"> {event.resource_id}</span> : ''}</span>}
        </div>
        <div><span className="text-gray-400 block text-[10px]">시각</span><span>{event.occurred_at || '—'}</span></div>
        <div><span className="text-gray-400 block text-[10px]">출처</span><span>{(event.ip_address ? `IP ${event.ip_address}` : '') + (event.user_agent ? ' · UA' : '') || '내부'}</span></div>
        <div><span className="text-gray-400 block text-[10px]">무결성</span>
          <span className={v.integrity === 'verified' ? 'text-green-700' : v.integrity === 'degraded' ? 'text-red-700' : 'text-gray-500'}>
            {v.integrity === 'verified' ? '✅ 체인 검증됨' : v.integrity === 'degraded' ? '⚠ 체인 불일치' : '미검증'}
            {typeof v.chainSeq === 'number' ? ` · seq ${v.chainSeq}` : ''}
          </span>
        </div>
        <div><span className="text-gray-400 block text-[10px]">법적 보류</span><span>{v.legalHold ? '⛔ 활성' : '해당 없음'}</span></div>
      </div>
      <div>
        <button className="text-[10px] text-blue-600 hover:underline" onClick={() => setShowRaw(s => !s)}
          aria-expanded={showRaw}>기술 증거 (raw payload / hash chain) {showRaw ? '▲' : '▼'}</button>
        {showRaw && (
          <div className="mt-1 space-y-2">
            <div className="bg-gray-50 rounded p-2">
              <div className="text-gray-400 mb-1 text-[10px]">체인 / 해시</div>
              <pre className="text-[10px] font-mono overflow-auto whitespace-pre-wrap break-all">
                {[
                  ['event_digest', event.event_digest],
                  ['prev_event_digest', event.prev_event_digest],
                  ['chain_seq', event.chain_seq],
                  ['archive_state', event.archive_state],
                ].map(([k, val]) => `${k}: ${val ?? '—'}`).join('\n')}
              </pre>
            </div>
            <div className="bg-gray-50 rounded p-2">
              <div className="text-gray-400 mb-1 text-[10px]">상세 페이로드 (JSON)</div>
              <pre className="bg-gray-50 rounded p-2 overflow-auto max-h-60 text-[10px] font-mono whitespace-pre-wrap">
                {(() => { try { return JSON.stringify(JSON.parse(event.details || '{}'), null, 2) } catch { return event.details || '—' } })()}
              </pre>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
