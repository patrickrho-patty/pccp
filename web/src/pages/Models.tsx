import { useState, useEffect, Fragment } from 'react'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['name', 'name_ko', 'package_id', 'model_id'],
  searchPlaceholder: '모델명, 패키지 ID로 검색...',
  dropdowns: [
    { key: 'state', label: '상태', options: [
      { value: 'draft', label: '초안' }, { value: 'published', label: '게시됨' },
      { value: 'deprecated', label: '사용중단' }, { value: 'recalled', label: '리콜됨' },
    ]},
    { key: 'family', label: '패밀리', options: [
      { value: 'code', label: 'Code' }, { value: 'chat', label: 'Chat' }, { value: 'vision', label: 'Vision' },
    ]},
  ],
}

export default function Models() {
  const [models, setModels] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [form, setForm] = useState({ package_id: '', model_id: '', name: '', name_ko: '', family: 'code', version: '1.0.0' })

  const [endpoints, setEndpoints] = useState<any[]>([])
  const load = () => {
    fetch('/api/models', { headers: authHeaders() }).then(r => r.json()).then(data => setModels(Array.isArray(data) ? data : [])).catch(() => setModels([]))
    fetch('/api/endpoints', { headers: authHeaders() }).then(r => r.json()).then(data => setEndpoints(Array.isArray(data) ? data : [])).catch(() => setEndpoints([]))
  }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(models, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const res = await fetch('/api/models', { method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ ...form, state: 'draft' }) }); if (!res.ok) throw new Error('등록 실패'); setShowForm(false); setForm({ package_id: '', model_id: '', name: '', name_ko: '', family: 'code', version: '1.0.0' }); load() } catch (err: any) { alert(err.message) }
  }
  const handlePublish = async (id: string) => { try { await fetch(`/api/models/${id}/publish`, { method: 'POST', headers: authHeaders() }); load() } catch {} }
  const handleRecall = async (id: string) => { if (confirm('리콜하시겠습니까? 모든 엔드포인트 리스가 무효화됩니다.')) { try { await fetch(`/api/models/${id}/recall`, { method: 'POST', headers: authHeaders() }); load() } catch {} } }
  const handleEdit = (m: any) => { setEditingId(m.id); setForm({ package_id: m.package_id || '', model_id: m.model_id || '', name: m.name || '', name_ko: m.name_ko || '', family: m.family || 'code', version: m.version || '1.0.0' }); setShowForm(true) }
  const handleUpdate = async (e: React.FormEvent) => { e.preventDefault(); if (!editingId) return; try { await fetch(`/api/models/${editingId}`, { method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ name: form.name, name_ko: form.name_ko }) }); setEditingId(null); setShowForm(false); load() } catch { alert('수정 실패') } }

  const getEndpointCount = (pkgId: string) => endpoints.filter(e => e.model_package_id === pkgId && e.status === 'active').length

  const stateBadge = (s: string) => { const m: Record<string,string> = { draft:'badge-gray', published:'badge-green', deprecated:'badge-yellow', recalled:'badge-red' }; return m[s] || 'badge-gray' }
  const stateLabel = (s: string) => { const m: Record<string,string> = { draft:'초안', published:'게시됨', deprecated:'사용중단', recalled:'리콜됨' }; return m[s] || s }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">모델 패키지 <span className="text-gray-400 text-lg font-normal">Model Packages</span></h1>
        <button onClick={() => { if (editingId) { setEditingId(null); setForm({ package_id: '', model_id: '', name: '', name_ko: '', family: 'code', version: '1.0.0' }) } setShowForm(!showForm) }} className="btn-primary">{showForm ? '취소' : '+ 모델 등록'}</button>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4">
          <h2 className="text-sm font-semibold">{editingId ? '모델 수정 · Edit Model' : '새 모델 패키지 · New Model Package'}</h2>
          <div className="grid grid-cols-2 gap-4">
            {!editingId && (<><div><label className="label">패키지 ID · Package ID</label><input className="input font-mono text-xs" value={form.package_id} onChange={e => setForm({ ...form, package_id: e.target.value })} placeholder="pmp_qwen3_moe_v1" required /></div><div><label className="label">모델 ID · Model ID</label><input className="input" value={form.model_id} onChange={e => setForm({ ...form, model_id: e.target.value })} placeholder="qwen3-moe" required /></div></>)}
            <div><label className="label">이름 · Name</label><input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} required /></div>
            <div><label className="label">한글 이름</label><input className="input" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} placeholder="큐웰3 MoE" /></div>
            {!editingId && (<><div><label className="label">패밀리 · Family</label><select className="input" value={form.family} onChange={e => setForm({ ...form, family: e.target.value })}><option value="code">Code</option><option value="chat">Chat</option><option value="vision">Vision</option></select></div><div><label className="label">버전 · Version</label><input className="input" value={form.version} onChange={e => setForm({ ...form, version: e.target.value })} /></div></>)}
          </div>
          <button type="submit" className="btn-primary">{editingId ? '수정 · Save' : '생성 · Create'}</button>
        </form>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        {paged.length === 0 ? (
          <div className="text-center py-8"><p className="text-gray-400">{filters.search ? '검색 결과가 없습니다' : '등록된 모델이 없습니다'}</p></div>
        ) : (
          <table className="w-full">
            <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
              <th className="pb-3">모델명</th><th className="pb-3">패키지 ID</th><th className="pb-3">버전</th><th className="pb-3">상태</th><th className="pb-3">엔드포인트</th><th className="pb-3">보증</th><th className="pb-3 text-right">작업</th>
            </tr></thead>
            <tbody>
              {paged.map(m => (
                <Fragment key={m.id || m.key || i}>
                  <tr key={m.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer" onClick={() => setExpandedId(expandedId === m.id ? null : m.id)}>
                    <td className="py-3">
                    <div className="font-medium text-sm">{m.name_ko || m.name}</div>
                    <div className="text-xs text-gray-400">{m.model_id}</div>
                    <div className="flex gap-1 mt-1">
                      {m.capabilities && typeof m.capabilities === 'string' && m.capabilities.includes('tool') && <span className="text-[10px] bg-blue-50 text-blue-600 px-1 rounded">도구</span>}
                      {m.capabilities && typeof m.capabilities === 'string' && m.capabilities.includes('image') && <span className="text-[10px] bg-purple-50 text-purple-600 px-1 rounded">이미지</span>}
                      {m.capabilities && typeof m.capabilities === 'string' && m.capabilities.includes('stream') && <span className="text-[10px] bg-green-50 text-green-600 px-1 rounded">스트리밍</span>}
                      {m.capabilities && typeof m.capabilities === 'string' && m.capabilities.includes('cache') && <span className="text-[10px] bg-yellow-50 text-yellow-600 px-1 rounded">캐시</span>}
                    </div>
                  </td>
                    <td className="py-3 font-mono text-xs">{m.package_id}</td>
                    <td className="py-3 text-sm">{m.version}</td>
                    <td className="py-3"><span className={stateBadge(m.state)}>{stateLabel(m.state)}</span></td>
                    <td className="py-3 text-sm">{getEndpointCount(m.package_id)} <span className="text-xs text-gray-400">활성</span></td>
                    <td className="py-3"><span className="badge-blue">{m.minimum_endpoint_assurance || 'L1'}</span></td>
                    <td className="py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex gap-2 justify-end">
                        <button onClick={() => handleEdit(m)} className="text-blue-600 text-xs hover:underline">수정</button>
                        {m.state === 'draft' && <button onClick={() => handlePublish(m.id)} className="text-green-600 text-xs hover:underline">게시</button>}
                        {m.state !== 'recalled' && <button onClick={() => handleRecall(m.id)} className="text-red-600 text-xs hover:underline">리콜</button>}
                      </div>
                    </td>
                  </tr>
                  {expandedId === m.id && (
                    <tr className="bg-gray-50"><td colSpan={7} className="p-4">
                      <div className="grid grid-cols-3 gap-4 text-sm">
                        <div><span className="text-gray-500">패밀리:</span> {m.family}</div>
                        <div><span className="text-gray-500">매니페스트:</span> <code className="text-xs">{m.manifest_digest?.slice(0, 30) || '-'}</code></div>
                        <div><span className="text-gray-500">서명 키:</span> {m.signature_key_id?.slice(0, 20) || '-'}</div>
                        <div><span className="text-gray-500">가중치 루트:</span> <code className="text-xs">{m.weights_merkle_root?.slice(0, 30) || '-'}</code></div>
                        <div><span className="text-gray-500">토크나이저:</span> <code className="text-xs">{m.tokenizer_digest?.slice(0, 30) || '-'}</code></div>
                        <div><span className="text-gray-500">컨테이너:</span> <code className="text-xs">{m.container_digest?.slice(0, 30) || '-'}</code></div>
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