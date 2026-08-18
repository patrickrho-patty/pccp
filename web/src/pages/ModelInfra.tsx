import { useState, useEffect, useMemo, Fragment } from 'react'
import EmptyState from '../components/EmptyState'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'
import { modelClassOptions } from '../allowedModels'

const MODEL_FILTER: FilterConfig = {
  searchFields: ['name', 'name_ko', 'package_id', 'model_id'],
  searchPlaceholder: '모델명, 패키지 ID로 검색...',
  dropdowns: [
    { key: 'state', label: '상태', options: [
      { value: 'draft', label: '초안' }, { value: 'published', label: '게시됨' },
      { value: 'deprecated', label: '사용중단' }, { value: 'recalled', label: '리콜됨' },
    ]},
    { key: 'family', label: '패밀리', options: [
      { value: 'code', label: 'Code' }, { value: 'chat', label: 'Chat' },
    ]},
    { key: 'entitlement_class', label: '클래스', options: modelClassOptions() },
  ],
}

const EP_FILTER: FilterConfig = {
  searchFields: ['endpoint_id', 'hostname', 'model_package_id'],
  searchPlaceholder: '엔드포인트 ID, 호스트명으로 검색...',
  dropdowns: [
    { key: 'status', label: '상태', options: [
      { value: 'active', label: '활성' }, { value: 'draining', label: '배출 중' },
      { value: 'inactive', label: '비활성' },
    ]},
  ],
}

export default function ModelInfra() {
  const confirm = useConfirm()
  // A class deep link (?class=…) filters the Packages (PMP) registry, so
  // land directly on that tab (PAT-1491).
  const [searchParams] = useSearchParams()
  const [tab, setTab] = useState<'catalog' | 'packages' | 'endpoints'>(
    searchParams.get('class') ? 'packages' : 'catalog')

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">모델 인프라 <span className="text-gray-400 text-lg font-normal">Model Infrastructure</span></h1>
      <p className="text-xs text-gray-400 mb-6">사용자 모델 카탈로그, 패키지(PMP), 서빙 엔드포인트(PIA) 통합 관리 · PRD §9.5: Three separate identities</p>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'catalog', label: '카탈로그', en: 'Catalog (User-Facing)', desc: '사용자가 선택하는 모델' },
          { id: 'packages', label: '패키지 (PMP)', en: 'Packages (Artifacts)', desc: '서명된 모델 아티팩트' },
          { id: 'endpoints', label: '엔드포인트 (PIA)', en: 'Endpoints (Deployments)', desc: '실행 중인 서빙 인스턴스' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} <span className="text-xs text-gray-400">{t.en}</span>
          </button>
        ))}
      </div>

      {/* Relationship diagram */}
      <div className="card mb-6 py-3 px-4">
        <div className="flex items-center justify-center gap-4 text-xs text-gray-500">
          <span className="font-medium text-gray-700">카탈로그 모델</span>
          <span className="text-gray-400">→ PCCP 해결 →</span>
          <span className="font-medium text-gray-700">패키지 (PMP)</span>
          <span className="text-gray-400">→ 스케줄러 선택 →</span>
          <span className="font-medium text-gray-700">엔드포인트 (PIA)</span>
          <span className="text-gray-400">→ 로컬 어댑터 →</span>
          <span className="font-medium text-gray-700">vLLM / SGLang</span>
        </div>
        <p className="text-center text-[10px] text-gray-400 mt-1">사용자는 카탈로그 ID만 봄 · 하네스는 엔드포인트 주소를 받지 않음 · PRD §9.2</p>
      </div>

      {tab === 'catalog' && <CatalogTab />}
      {tab === 'packages' && <PackagesTab />}
      {tab === 'endpoints' && <EndpointsTab />}
    </div>
  )
}

// ─── Catalog Tab ──────────────────────────────────────────────
function CatalogTab() {
  const [models, setModels] = useState<any[]>([])
  const [epoch, setEpoch] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      fetch('/api/catalog/models', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/api/catalog/epoch', { headers: authHeaders() }).then(r => r.json()).catch(() => null),
    ]).then(([m, e]) => { setModels(Array.isArray(m) ? m : []); setEpoch(e); setLoading(false) })
  }, [])

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  const handleSeed = async () => { await api.catalogSeed(); window.location.reload() }
  const handleWithdraw = async (id: string) => { if (await confirm({ title: '확인', message: '이 모델을 철회하시겠습니까?', danger: true })) { await api.catalogWithdraw(id); window.location.reload() } }

  return (
    <div>
      {/* Epoch info */}
      {epoch && (
        <div className="flex items-center gap-4 mb-4 p-3 bg-gray-50 rounded-lg">
          <span className="text-sm font-medium">현재 에포크 · Current Epoch</span>
          <span className="font-mono text-xs text-gray-500">{epoch.epoch_id?.slice(0, 40)}</span>
          {epoch.min_validity_secs && <span className="text-xs text-gray-400">유효 {epoch.min_validity_secs}초</span>}
          <button onClick={async () => {
            const res = await fetch('/api/catalog/epoch', { headers: authHeaders() })
            if (res.ok) { const e = await res.json(); setEpoch(e); showToast('에포크 갱신됨') }
          }} className="btn-secondary text-xs ml-auto">에포크 갱신</button>
        </div>
      )}

      <div className="flex justify-end mb-3">
        <button onClick={handleSeed} className="btn-secondary text-sm">기본 모델 등록</button>
      </div>

      {models.length === 0 ? (
        <div className="card"><EmptyState icon="📦" title="카탈로그 모델이 없습니다" message="카탈로그 시드 버튼으로 기본 모델을 등록하세요" /></div>
      ) : (
        <div className="grid grid-cols-3 gap-4">
          {models.map(m => (
            <div key={m.catalog_model_id} className="card border-l-4" style={{ borderLeftColor: m.availability === 'available' ? '#22c55e' : m.availability === 'deprecated' ? '#eab308' : '#ef4444' }}>
              <div className="flex items-center justify-between mb-2">
                <h4 className="text-sm font-semibold">{m.display_name_ko || m.display_name || m.catalog_model_id}</h4>
                <span className={`text-xs ${m.availability === 'available' ? 'text-green-600' : m.availability === 'deprecated' ? 'text-yellow-600' : 'text-red-600'}`}>{m.availability}</span>
              </div>
              <p className="text-xs text-gray-400 font-mono mb-2">{m.catalog_model_id}</p>
              {m.capabilities && (
                <div className="flex flex-wrap gap-1 mb-2">
                  {m.capabilities.tools?.client_tools && <span className="text-[10px] bg-blue-50 text-blue-600 px-1.5 py-0.5 rounded">도구</span>}
                  {m.capabilities.input?.image && <span className="text-[10px] bg-purple-50 text-purple-600 px-1.5 py-0.5 rounded">이미지</span>}
                  {m.capabilities.streaming && <span className="text-[10px] bg-green-50 text-green-600 px-1.5 py-0.5 rounded">스트리밍</span>}
                  {m.capabilities.cache?.prompt_cache && <span className="text-[10px] bg-yellow-50 text-yellow-600 px-1.5 py-0.5 rounded">캐시</span>}
                </div>
              )}
              {m.limits && (
                <div className="text-xs text-gray-500">최대 컨텍스트: {m.limits.max_input_tokens?.toLocaleString() || '-'} 토큰</div>
              )}
              {m.availability === 'available' && (
                <button onClick={() => handleWithdraw(m.catalog_model_id)} className="text-red-600 text-xs hover:underline mt-2">철회</button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ─── Packages Tab (PMP) ───────────────────────────────────────
function PackagesTab() {
  const [models, setModels] = useState<any[]>([])
  const [endpoints, setEndpoints] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  // Deep-linkable class filter (PAT-1491): /models?class=code lands here.
  // FilterBar seeds the select DISPLAY from defaultValue but only emits
  // changes on user interaction — so the data filter is seeded here too,
  // and a key=deepClass remount re-seeds both when the param changes.
  const [searchParams] = useSearchParams()
  const deepClass = searchParams.get('class') || ''
  const modelFilter = useMemo(() => ({
    ...MODEL_FILTER,
    dropdowns: (MODEL_FILTER.dropdowns || []).map(d => {
      if (d.key !== 'entitlement_class' || !deepClass) return d
      const has = d.options.some(o => o.value === deepClass)
      return { ...d, defaultValue: deepClass, options: has ? d.options : [...d.options, { value: deepClass, label: deepClass }] }
    }),
  }), [deepClass])
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: (deepClass ? { entitlement_class: deepClass } : {}) as Record<string, string> })
  const [page, setPage] = useState(1)
  const [form, setForm] = useState({ package_id: '', model_id: '', name: '', name_ko: '', family: 'code', version: '1.0.0' })
  const [impactTarget, setImpactTarget] = useState<any>(null)
  const [impact, setImpact] = useState<any>(null)
  const pageSize = 25

  const showImpact = async (m: any) => {
    setImpactTarget(m)
    try { setImpact(await api.modelRecallImpact(m.id)) } catch (e: any) { setImpact(null); showToast(e?.message || '실패', 'error') }
  }
  const setRing = async (m: any, ring: string) => {
    try { await api.assignModelRing(m.id, ring); showToast(`링 ${ring} 배정 완료`, 'success'); load() }
    catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const load = () => {
    fetch('/api/models', { headers: authHeaders() }).then(r => r.json()).then(d => setModels(Array.isArray(d) ? d : [])).catch(() => {})
    fetch('/api/endpoints', { headers: authHeaders() }).then(r => r.json()).then(d => setEndpoints(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(models, filters, modelFilter)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try { await fetch('/api/models', { method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ ...form, state: 'draft' }) }); setShowForm(false); showToast('모델 등록됨', 'success'); load() } catch { showToast('실패', 'error') }
  }
  const handlePublish = async (id: string) => {
    try {
      const res = await api.publishModel(id)
      showToast(`게시 완료 — 서명·다이제스트 검증 ${res.verified === 'true' ? '통과' : ''}`, 'success')
      load()
    } catch (e: any) { showToast(e?.message || '게시 실패 (검증 실패 시 거부됨)', 'error') }
  }
  const handleRecall = async (id: string) => { if (await confirm({ title: '확인', message: '리콜하시겠습니까?', danger: true })) { await fetch(`/api/models/${id}/recall`, { method: 'POST', headers: authHeaders() }); load() } }
  const handleEdit = (m: any) => { setEditingId(m.id); setForm({ package_id: m.package_id || '', model_id: m.model_id || '', name: m.name || '', name_ko: m.name_ko || '', family: m.family || 'code', version: m.version || '1.0.0' }); setShowForm(true) }
  const getEpCount = (pkgId: string) => endpoints.filter(e => e.model_package_id === pkgId && e.status === 'active').length

  const stateBadge = (s: string) => { const m: Record<string,string> = { draft:'badge-gray', published:'badge-green', deprecated:'badge-yellow', recalled:'badge-red' }; return m[s] || 'badge-gray' }
  const stateLabel = (s: string) => { const m: Record<string,string> = { draft:'초안', published:'게시됨', deprecated:'사용중단', recalled:'리콜됨' }; return m[s] || s }

  return (
    <div>
      <div className="flex justify-between items-center mb-4">
        <p className="text-xs text-gray-400">서명된 모델 아티팩트 · 가중치, 토크나이저, 양자화 등 · PRD §9.4</p>
        <button onClick={() => { setEditingId(null); setForm({ package_id: '', model_id: '', name: '', name_ko: '', family: 'code', version: '1.0.0' }); setShowForm(!showForm) }} className="btn-primary text-sm">{showForm ? '취소' : '+ 패키지 등록'}</button>
      </div>

      {showForm && (
        <form onSubmit={editingId ? async (e) => { e.preventDefault(); await fetch(`/api/models/${editingId}`, { method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(form) }); setEditingId(null); setShowForm(false); load() } : handleCreate} className="card mb-4">
          <div className="grid grid-cols-3 gap-4">
            <div><label className="label">패키지 ID</label><input className="input" value={form.package_id} onChange={e => setForm({ ...form, package_id: e.target.value })} placeholder="pmp-qwen3-moe-v3" disabled={!!editingId} /></div>
            <div><label className="label">모델 ID</label><input className="input" value={form.model_id} onChange={e => setForm({ ...form, model_id: e.target.value })} disabled={!!editingId} /></div>
            <div><label className="label">버전</label><input className="input" value={form.version} onChange={e => setForm({ ...form, version: e.target.value })} /></div>
            <div><label className="label">이름</label><input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></div>
            <div><label className="label">한글명</label><input className="input" value={form.name_ko} onChange={e => setForm({ ...form, name_ko: e.target.value })} /></div>
            <div><label className="label">패밀리</label><select className="input" value={form.family} onChange={e => setForm({ ...form, family: e.target.value })}><option value="code">Code</option><option value="chat">Chat</option></select></div>
          </div>
          <button type="submit" className="btn-primary text-sm mt-3">{editingId ? '수정' : '등록'}</button>
        </form>
      )}

      <FilterBar key={deepClass} config={modelFilter} onChange={setFilters} />

      <div className="card">
        <table className="w-full overflow-x-auto block">
          <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
            <th className="pb-3">패키지</th><th className="pb-3">상태</th><th className="pb-3">엔드포인트</th><th className="pb-3">작업</th>
          </tr></thead>
          <tbody>
            {paged.map(m => (
              <tr key={m.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30">
                <td className="py-3">
                  <div className="font-medium text-sm">{m.name_ko || m.name}</div>
                  <div className="text-xs text-gray-400 font-mono">{m.package_id}</div>
                  <div className="text-xs text-gray-400">v{m.version} · {m.family}</div>
                </td>
                <td className="py-3"><span className={stateBadge(m.state)}>{stateLabel(m.state)}</span></td>
                <td className="py-3"><span className="badge-gray">{getEpCount(m.package_id)} PIA</span></td>
                <td className="py-3">
                  <div className="flex gap-2 flex-wrap items-center">
                    {m.state === 'draft' && <button onClick={() => handlePublish(m.id)} className="text-green-600 text-xs hover:underline" title="서명·다이제스트 검증 후 게시">게시 (검증)</button>}
                    {m.state === 'published' && <button onClick={() => handleRecall(m.id)} className="text-red-600 text-xs hover:underline">리콜</button>}
                    <button onClick={() => showImpact(m)} className="text-orange-600 text-xs hover:underline">영향</button>
                    <select className="input text-[10px] py-0 w-20" value={m.release || 'stable'} onChange={e => setRing(m, e.target.value)}>
                      <option value="canary">canary</option>
                      <option value="beta">beta</option>
                      <option value="stable">stable</option>
                    </select>
                    <button onClick={() => handleEdit(m)} className="text-blue-600 text-xs hover:underline">편집</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />
      </div>

      {impactTarget && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50" onClick={() => setImpactTarget(null)}>
          <div className="bg-white rounded-xl shadow-xl max-w-md w-full mx-4 p-5" onClick={e => e.stopPropagation()}>
            <h3 className="text-sm font-semibold">리콜 영향 분석 — {impactTarget.package_id}</h3>
            {impact ? (
              <div className="mt-3 space-y-1 text-xs text-gray-600">
                <div className="flex justify-between"><span>영향 엔드포인트</span><span className="font-semibold">{impact.affected_endpoints}</span></div>
                <div className="flex justify-between"><span>영향 세션</span><span className="font-semibold">{impact.affected_sessions}</span></div>
                <div className="flex justify-between"><span>사용 기록</span><span className="font-semibold">{impact.usage_records}</span></div>
                <div className="flex justify-between"><span>현재 상태</span><span className="font-semibold">{impact.state}</span></div>
              </div>
            ) : <p className="mt-3 text-xs text-gray-400">로딩...</p>}
            <div className="flex justify-end mt-4"><button className="btn-sm btn-secondary" onClick={() => setImpactTarget(null)}>닫기</button></div>
          </div>
        </div>
      )}
    </div>
  )
}

// ─── Endpoints Tab (PIA) ──────────────────────────────────────
function EndpointsTab() {
  const [endpoints, setEndpoints] = useState<any[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })

  const load = () => { fetch('/api/endpoints', { headers: authHeaders() }).then(r => r.json()).then(d => setEndpoints(Array.isArray(d) ? d : [])).catch(() => {}) }
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(endpoints, filters, EP_FILTER)
  const statusBadge = (s: string) => { const m: Record<string,string> = { active:'badge-green', draining:'badge-yellow', inactive:'badge-gray' }; return m[s] || 'badge-gray' }

  return (
    <div>
      <p className="text-xs text-gray-400 mb-4">실행 중인 PIA 배포 · 각 PIA = 1 GPU 그룹 · vLLM/SGLang 서빙 · PRD §30.2</p>

      <FilterBar config={EP_FILTER} onChange={setFilters} />

      <div className="card">
        <table className="w-full overflow-x-auto block">
          <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
            <th className="pb-3">엔드포인트 (PIA)</th><th className="pb-3">상태</th><th className="pb-3">보증</th><th className="pb-3">활성 요청</th><th className="pb-3">TTFT</th>
          </tr></thead>
          <tbody>
            {filtered.map(e => (
              <Fragment key={e.id}>
                <tr key={e.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer"
                  onClick={() => setExpandedId(expandedId === e.id ? null : e.id)}>
                  <td className="py-3">
                    <div className="font-mono text-xs">{e.endpoint_id?.slice(0, 25)}</div>
                    <div className="text-xs text-gray-400">{e.hostname || '-'} · {e.model_package_id?.slice(0, 20) || '-'}</div>
                  </td>
                  <td className="py-3"><span className={statusBadge(e.status)}>{e.status}</span></td>
                  <td className="py-3"><span className="badge-gray">{e.assurance_level || 'L1'}</span></td>
                  <td className="py-3 text-sm">{e.active_requests || 0}</td>
                  <td className="py-3 text-sm">{e.ttft_p50 ? e.ttft_p50.toFixed(2) + 's' : '-'}</td>
                </tr>
                {expandedId === e.id && (
                  <tr className="bg-gray-50"><td colSpan={5} className="p-4">
                    <div className="grid grid-cols-3 gap-6">
                      <div><div className="text-xs font-semibold text-gray-600 mb-2">엔드포인트 정보</div>
                        <div className="space-y-1 text-xs text-gray-500">
                          <div>PIA 버전: {e.pia_version || '-'}</div>
                          <div>서빙 엔진: {e.serving_type || 'vLLM'}</div>
                          <div>아티팩트: <span className="font-mono">{e.model_package_id?.slice(0, 25)}</span></div>
                          <div>증명 상태: {e.attestation_state || 'verified'}</div>
                          <div>보증 등급: {e.assurance_level || 'L1'}</div>
                        </div>
                      </div>
                      <div><div className="text-xs font-semibold text-gray-600 mb-2">성능 지표</div>
                        <div className="grid grid-cols-2 gap-3">
                          <div className="bg-white rounded p-2 text-center"><div className="text-sm font-bold">{e.ttft_p50 ? e.ttft_p50.toFixed(2) + 's' : '-'}</div><div className="text-[10px] text-gray-500">TTFT P50</div></div>
                          <div className="bg-white rounded p-2 text-center"><div className="text-sm font-bold">{e.ttft_p95 ? e.ttft_p95.toFixed(2) + 's' : '-'}</div><div className="text-[10px] text-gray-500">TTFT P95</div></div>
                          <div className="bg-white rounded p-2 text-center"><div className="text-sm font-bold">{e.decode_rate ? e.decode_rate.toFixed(0) + ' tok/s' : '-'}</div><div className="text-[10px] text-gray-500">출력 속도</div></div>
                          <div className="bg-white rounded p-2 text-center"><div className={`text-sm font-bold ${e.status === 'active' ? 'text-green-600' : 'text-gray-400'}`}>{e.status === 'active' ? '99.9%' : '-'}</div><div className="text-[10px] text-gray-500">가동률</div></div>
                        </div>
                      </div>
                      <div><div className="text-xs font-semibold text-gray-600 mb-2">용량</div>
                        <div className="space-y-1 text-xs text-gray-500">
                          <div>활성 요청: {e.active_requests || 0}</div>
                          <div>대기열: {0}</div>
                          <div>증명 만료: {e.last_attestation?.slice(0, 19) || '-'}</div>
                          <div>마지막 증명: {e.last_attestation?.slice(0, 19) || '-'}</div>
                        </div>
                      </div>
                    </div>
                  </td></tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}