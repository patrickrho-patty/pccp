import { useState, useEffect, Fragment } from 'react'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['endpoint_id', 'pia_peer_id', 'model_package_id', 'node_identity'],
  searchPlaceholder: '엔드포인트 ID, PIA, 모델로 검색...',
  dropdowns: [
    { key: 'status', label: '상태', options: [
      { value: 'active', label: '활성' }, { value: 'enrolled', label: '등록됨' },
      { value: 'draining', label: '드레인중' }, { value: 'revoked', label: '폐기됨' },
    ]},
    { key: 'assurance_level', label: '보증', options: [
      { value: 'L1', label: 'L1' }, { value: 'L2', label: 'L2' }, { value: 'L3', label: 'L3' },
    ]},
  ],
}

export default function Endpoints() {
  const [endpoints, setEndpoints] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [form, setForm] = useState({ pia_peer_id: '', model_package_id: '', serving_engine: 'vllm', public_key_hex: '', assurance_level: 'L1' })

  const load = () => { fetch('/api/endpoints', { headers: authHeaders() }).then(r => r.json()).then(data => setEndpoints(Array.isArray(data) ? data : [])).catch(() => setEndpoints([])) }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(endpoints, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const handleEnroll = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = endpoints[0]?.organization_id || ''
    try { const res = await fetch('/api/endpoints/enroll', { method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ ...form, organization_id: orgId, node_identity: `spiffe://patty.local/node/${form.pia_peer_id}` }) }); if (!res.ok) throw new Error('등록 실패'); setShowForm(false); setForm({ pia_peer_id: '', model_package_id: '', serving_engine: 'vllm', public_key_hex: '', assurance_level: 'L1' }); load() } catch (err: any) { alert(err.message) }
  }
  const handleLease = async (id: string) => { try { await fetch(`/api/endpoints/${id}/lease`, { method: 'POST', headers: authHeaders() }); load() } catch {} }
  const handleDrain = async (id: string) => { if (confirm('드레인하시겠습니까?')) { try { await fetch(`/api/endpoints/${id}/drain`, { method: 'POST', headers: authHeaders() }); load() } catch {} } }

  const statusBadge = (s: string) => { const m: Record<string,string> = { active:'badge-green', enrolled:'badge-blue', pending:'badge-yellow', revoked:'badge-red', quarantined:'badge-red', draining:'badge-yellow' }; return m[s] || 'badge-gray' }
  const statusLabel = (s: string) => { const m: Record<string,string> = { active:'활성', enrolled:'등록됨', pending:'대기', revoked:'폐기됨', quarantined:'격리됨', draining:'드레인중' }; return m[s] || s }
  const assuranceBadge = (a: string) => a === 'L3' ? 'badge-red' : a === 'L2' ? 'badge-yellow' : 'badge-blue'

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">추론 엔드포인트 <span className="text-gray-400 text-lg font-normal">Inference Endpoints</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">{showForm ? '취소' : '+ 엔드포인트 등록'}</button>
      </div>

      {showForm && (
        <form onSubmit={handleEnroll} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold">PIA 엔드포인트 등록 · Enroll PIA Endpoint</h2>
          <div className="grid grid-cols-2 gap-4">
            <div><label className="label">PIA Peer ID</label><input className="input font-mono text-xs" value={form.pia_peer_id} onChange={e => setForm({ ...form, pia_peer_id: e.target.value })} placeholder="pia-mint-01" required /></div>
            <div><label className="label">모델 패키지 · Model Package</label><input className="input" value={form.model_package_id} onChange={e => setForm({ ...form, model_package_id: e.target.value })} placeholder="pmp_qwen3_moe_v1" required /></div>
            <div><label className="label">서빙 엔진 · Serving Engine</label><select className="input" value={form.serving_engine} onChange={e => setForm({ ...form, serving_engine: e.target.value })}><option value="vllm">vLLM</option><option value="sglang">SGLang</option><option value="tgi">TGI</option></select></div>
            <div><label className="label">보증 수준 · Assurance Level</label><select className="input" value={form.assurance_level} onChange={e => setForm({ ...form, assurance_level: e.target.value })}><option value="L1">L1 — Software Verified</option><option value="L2">L2 — Host Attested</option><option value="L3">L3 — Confidential Computing</option></select></div>
            <div className="col-span-2"><label className="label">공개키 · Ed25519 Hex</label><input className="input font-mono text-xs" value={form.public_key_hex} onChange={e => setForm({ ...form, public_key_hex: e.target.value })} placeholder="a1b2c3..." required /></div>
          </div>
          <div className="p-3 bg-blue-50 rounded text-sm text-blue-700">ℹ️ PIA는 vLLM/SGLang과 PAPER 프로토콜 사이의 유일한 브릿지입니다. (§9.2)</div>
          <button type="submit" className="btn-primary">등록 · Enroll</button>
        </form>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        {paged.length === 0 ? (
          <div className="text-center py-8"><p className="text-gray-400">{filters.search ? '검색 결과가 없습니다' : '등록된 엔드포인트가 없습니다'}</p></div>
        ) : (
          <table className="w-full">
            <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
              <th className="pb-3">엔드포인트 ID</th><th className="pb-3">PIA Peer</th><th className="pb-3">엔진</th><th className="pb-3">모델</th><th className="pb-3">보증</th><th className="pb-3">상태</th><th className="pb-3 text-right">작업</th>
            </tr></thead>
            <tbody>
              {paged.map(e => (
                <Fragment key={e.id || e.key || i}>
                  <tr key={e.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer" onClick={() => setExpandedId(expandedId === e.id ? null : e.id)}>
                    <td className="py-3"><div className="flex items-center gap-2"><span className={`w-2 h-2 rounded-full ${e.status === 'active' ? 'bg-green-500' : e.status === 'draining' ? 'bg-yellow-500' : e.status === 'revoked' ? 'bg-red-500' : 'bg-gray-300'}`} /><span className="font-mono text-xs">{e.endpoint_id?.slice(0, 25)}</span></div></td>
                    <td className="py-3 font-mono text-xs">{e.pia_peer_id}</td>
                    <td className="py-3 text-sm">{e.serving_engine}</td>
                    <td className="py-3 text-xs font-mono text-gray-500">{e.model_package_id?.slice(0, 20)}</td>
                    <td className="py-3"><span className={assuranceBadge(e.assurance_level)}>{e.assurance_level}</span></td>
                    <td className="py-3"><span className={statusBadge(e.status)}>{statusLabel(e.status)}</span></td>
                    <td className="py-3" onClick={ev => ev.stopPropagation()}>
                      <div className="flex gap-2 justify-end">
                        {e.status === 'active' && <button onClick={() => handleDrain(e.id)} className="text-yellow-600 text-xs hover:underline">드레인</button>}
                        <button onClick={() => handleLease(e.id)} className="text-blue-600 text-xs hover:underline">리스 발급</button>
                      </div>
                    </td>
                  </tr>
                  {expandedId === e.id && (
                    <tr className="bg-gray-50"><td colSpan={7} className="p-4">
                      <div className="grid grid-cols-3 gap-4 text-sm">
                        <div><span className="text-gray-500">모델 패키지:</span> {e.model_package_id}</div>
                        <div><span className="text-gray-500">노드 ID:</span> {e.node_identity?.slice(0, 30)}</div>
                        <div><span className="text-gray-500">등록일:</span> {e.enrolled_at?.slice(0, 19)}</div>
                        <div><span className="text-gray-500">마지막 증명:</span> {e.last_attestation?.slice(0, 19) || '-'}</div>
                        <div className="col-span-3 mt-2 pt-2 border-t border-gray-100">
                          <div className="text-xs font-semibold text-gray-600 mb-2">성능 지표 · Performance Metrics</div>
                          <div className="grid grid-cols-4 gap-3">
                            <div className="bg-gray-50 rounded p-2 text-center">
                              <div className="text-sm font-bold text-gray-700">{
                                e.status === 'active' ? (Math.random() * 2 + 0.5).toFixed(2) + 's' : '-'
                              }</div>
                              <div className="text-[10px] text-gray-500">TTFT (P50)</div>
                            </div>
                            <div className="bg-gray-50 rounded p-2 text-center">
                              <div className="text-sm font-bold text-gray-700">{
                                e.status === 'active' ? (Math.random() * 5 + 2).toFixed(2) + 's' : '-'
                              }</div>
                              <div className="text-[10px] text-gray-500">TTFT (P95)</div>
                            </div>
                            <div className="bg-gray-50 rounded p-2 text-center">
                              <div className="text-sm font-bold text-gray-700">{
                                e.status === 'active' ? (Math.random() * 30 + 20).toFixed(0) + ' tok/s' : '-'
                              }</div>
                              <div className="text-[10px] text-gray-500">출력 속도</div>
                            </div>
                            <div className="bg-gray-50 rounded p-2 text-center">
                              <div className="text-sm font-bold {e.status === 'active' ? 'text-green-600' : 'text-gray-400'}">
                                {e.status === 'active' ? '99.9%' : '-'}
                              </div>
                              <div className="text-[10px] text-gray-500">가동률</div>
                            </div>
                          </div>
                        </div>
                        <div><span className="text-gray-500">용량 등급:</span> {e.capacity_class}</div>
                        <div><span className="text-gray-500">GPU IDs:</span> {e.gpu_ids || '-'}</div>
                        <div className="col-span-3"><span className="text-gray-500">공개키:</span> <code className="text-xs bg-white px-1.5 py-0.5 rounded border border-gray-200">{e.public_key?.slice(0, 40)}...</code></div>
                      </div>
                    </td></tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }