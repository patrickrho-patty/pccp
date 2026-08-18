import { useEffect, useState } from 'react'
import { api } from '../api'
import { useServerTable, buildQuery, ServerQuery } from '../hooks/useServerTable'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'

// Audit page (web/17 plan): server-side query (A), tamper-evidence
// verification (B), legal holds (C), payload drill-down (D), SIEM
// forwarding + evidence bundle + live tail (E).

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
  const [holdOpen, setHoldOpen] = useState(false)
  const [holdForm, setHoldForm] = useState({ resource_type: 'session', resource_id: '', reason: '' })
  const [siemOpen, setSiemOpen] = useState(false)
  const [siemForm, setSiemForm] = useState({ webhook: '', secret: '' })

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

      <div className="flex gap-2 flex-wrap items-center">
        <input className="input text-xs w-56" placeholder="검색 (이벤트/액션/리소스)..." value={table.search}
          onChange={e => table.setSearch(e.target.value)} />
        <select className="input text-xs w-28" value={table.filters.type || ''}
          onChange={e => table.setFilter('type', e.target.value)}>
          <option value="">전체 유형</option>
          <option value="cp.user.updated">user</option>
          <option value="cp.session.opened">session</option>
          <option value="cp.fleet.action">fleet</option>
          <option value="cp.model.published">model</option>
        </select>
        <select className="input text-xs w-28" value={table.filters.result || ''}
          onChange={e => table.setFilter('result', e.target.value)}>
          <option value="">전체 결과</option>
          <option value="success">성공</option>
          <option value="failed">실패</option>
        </select>
        {table.loading && <span className="text-[10px] text-gray-400 animate-pulse">로딩...</span>}
      </div>

      <div className="space-y-1">
        {table.rows.length === 0 && <EmptyState icon="📜" title="감사 이벤트가 없습니다" />}
        {table.rows.map((e: any) => (
          <div key={e.id} className="flex items-center gap-2 text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">
            <input type="checkbox" checked={selectedIds.has(e.id)} onChange={() => toggleSelect(e.id)} />
            <span className="text-gray-500 w-40 truncate font-mono">{e.event_type}</span>
            <span className="text-gray-700 flex-1 truncate">{e.action}</span>
            <span className="text-gray-400 w-24 truncate">{e.resource_type}:{e.resource_id?.slice(0, 8)}</span>
            <span className={`w-16 ${e.result === 'success' ? 'text-green-600' : 'text-red-500'}`}>{e.result}</span>
            <span className="text-gray-400 w-28">{(e.occurred_at || '').slice(0, 16)}</span>
            <button className="text-[10px] px-2 py-0.5 rounded hover:bg-blue-50 text-blue-600" onClick={() => setPayloadTarget(e)}>상세</button>
          </div>
        ))}
      </div>

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

      {/* Payload drill-down (D) */}
      <Modal open={!!payloadTarget} title={`이벤트 상세 — ${payloadTarget?.event_type || ''}`}
        onClose={() => setPayloadTarget(null)} size="lg"
        footer={<ModalFooter onCancel={() => setPayloadTarget(null)} onConfirm={() => setPayloadTarget(null)} confirmLabel="닫기" />}>
        <div className="space-y-2 text-[11px]">
          <div className="flex justify-between"><span className="text-gray-400">리소스</span><span>{payloadTarget?.resource_type}:{payloadTarget?.resource_id}</span></div>
          <div className="flex justify-between"><span className="text-gray-400">액션</span><span>{payloadTarget?.action}</span></div>
          <div className="flex justify-between"><span className="text-gray-400">시각</span><span>{payloadTarget?.occurred_at}</span></div>
          <div>
            <div className="text-gray-400 mb-1">상세 페이로드 (JSON)</div>
            <pre className="bg-gray-50 rounded p-2 overflow-auto max-h-60 text-[10px] font-mono whitespace-pre-wrap">
              {(() => { try { return JSON.stringify(JSON.parse(payloadTarget?.details || '{}'), null, 2) } catch { return payloadTarget?.details || '—' } })()}
            </pre>
          </div>
        </div>
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
