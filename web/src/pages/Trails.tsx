import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'

type Cluster = { session_id: string; node_count: number; last_at: string }
type TNode = {
  source_type: string; source_id: string; node_type: string; node_type_ko: string
  label_ko: string; status: string; status_ko: string; session_id: string
  user_id: string; harness_id: string; repository_id: string
  occurred_at: string; integrity_digest: string
}
type TEdge = { from: string; to: string; type: string; evidence: string }
type Graph = {
  nodes: TNode[]; edges: TEdge[]; node_type_distribution: Record<string, number>
  collapsed_groups: number; budget: number; truncated: boolean
}

const TYPE_COLOR: Record<string, string> = {
  goal: '#6366f1', execution: '#0ea5e9', decision: '#f59e0b',
  change: '#10b981', effect: '#8b5cf6', exception: '#ef4444', outcome: '#14b8a6',
}
const TYPE_SHAPE: Record<string, string> = {
  goal: 'circle', execution: 'rect', decision: 'diamond', change: 'rect',
  effect: 'hexagon', exception: 'triangle', outcome: 'circle',
}
const EDGE_KO: Record<string, string> = {
  initiated: '시작됨', delegated: '위임됨', authorized: '승인됨',
  blocked: '차단됨', produced: '생성', caused: '유발', rolled_back: '롤백됨',
}

// Deterministic layered layout: nodes flow top→bottom by time, left→right
// by type — same query window always renders identically (PAT-1450).
function layout(nodes: TNode[], width = 900): Array<TNode & { x: number; y: number }> {
  const sorted = [...nodes].sort((a, b) => (a.occurred_at < b.occurred_at ? -1 : 1))
  const lanes = [TYPE_ORDER.goal, TYPE_ORDER.execution, TYPE_ORDER.decision, TYPE_ORDER.change, TYPE_ORDER.effect, TYPE_ORDER.exception, TYPE_ORDER.outcome]
  return sorted.map((n, i) => {
    const lane = Math.max(0, lanes.indexOf(TYPE_ORDER[n.node_type] ?? n.node_type))
    return {
      ...n,
      x: 80 + (lane % 4) * (width - 160) / 3.4 + (i % 2) * 18,
      y: 40 + (i * 62) % 560,
    }
  })
}
const TYPE_ORDER: Record<string, string> = {
  goal: 'a', execution: 'b', decision: 'c', change: 'd', effect: 'e', exception: 'f', outcome: 'g',
}

export default function Trails() {
  const [overview, setOverview] = useState<{ clusters: Cluster[]; node_type_distribution: Record<string, number> } | null>(null)
  const [scope, setScope] = useState<{ kind: string; ref: string; label: string } | null>(null)
  const [graph, setGraph] = useState<Graph | null>(null)
  const [selected, setSelected] = useState<TNode | null>(null)
  const [detail, setDetail] = useState<any>(null)
  const [zoom, setZoom] = useState(1)
  const [typeFilter, setTypeFilter] = useState<Record<string, boolean>>({})
  const [listMode, setListMode] = useState(false)

  const loadOverview = () => {
    api.trailsOverview().then(setOverview).catch((e: any) => showToast(e.message))
  }
  useEffect(() => { loadOverview() }, [])

  const enterScope = (kind: string, ref: string, label: string) => {
    setScope({ kind, ref, label })
    setSelected(null)
    setDetail(null)
    api.trailsGraph(kind, ref).then((g: Graph) => { setGraph(g); setZoom(1) }).catch((e: any) => showToast(e.message))
  }

  const openNode = (n: TNode) => {
    setSelected(n)
    api.trailsNode(n.source_type, n.source_id).then(setDetail).catch((e: any) => showToast(e.message))
  }

  const laid = useMemo(() => (graph ? layout(applyFilter(graph.nodes, typeFilter)) : []), [graph, typeFilter])
  const nodeIndex = useMemo(() => new Map(laid.map((n) => [`${n.source_type}:${n.source_id}`, n])), [laid])

  const neighbors = (key: string) =>
    api.trailsNeighbors(key.split(':')[0], key.split(':')[1]).then((r: any) =>
      showToast(`${key} 인접 노드 ${r.neighbors?.length ?? 0}개 (깊이 ≤3, 순환 안전)`))

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Trails <span className="text-xs text-gray-400 ml-2">PAT-1450 · 인과 그래프 탐색</span></h1>
          <p className="text-sm text-gray-500 mt-1">기록된 관계로만 증명되는 인과 경로를 보여줍니다. 시간적 인접은 인과로 표시되지 않으며, 내보내기 기능은 제공되지 않습니다.</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={() => api.trailsRebuild().then((r: any) => { showToast(`투영 재구축: 노드 ${r.nodes_derived} · 간선 ${r.edges_derived}`); loadOverview() }).catch((e: any) => showToast(e.message))}>투영 재구축</button>
          <button className="btn-secondary text-sm" onClick={() => setListMode(!listMode)}>{listMode ? '그래프 보기' : '구조화 목록 보기'}</button>
        </div>
      </div>

      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm">
        <button className="text-sky-600 hover:underline" onClick={() => { setScope(null); setGraph(null); loadOverview() }}>조직 개요</button>
        {scope && <><span className="text-gray-400">/</span><span className="font-medium">{scope.label}</span></>}
      </div>

      {/* Overview: aggregated clusters */}
      {!scope && overview && (
        <div className="card">
          <div className="p-4 border-b flex items-center justify-between">
            <h2 className="font-semibold">세션 트레일 클러스터 (집계 진입점)</h2>
            <div className="flex gap-1 flex-wrap">
              {Object.entries(overview.node_type_distribution || {}).map(([t, n]) => (
                <span key={t} className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100">{t} {n}</span>
              ))}
            </div>
          </div>
          {overview.clusters?.length === 0 && <p className="p-4 text-sm text-gray-500">트레일이 없습니다. 투영 재구축을 실행하세요.</p>}
          {overview.clusters?.map((c) => (
            <button key={c.session_id} className="w-full text-left p-3 border-t hover:bg-gray-50 flex items-center justify-between"
              onClick={() => enterScope('session', c.session_id, `세션 ${c.session_id.slice(0, 12)}…`)}>
              <span className="font-mono text-xs">세션 {c.session_id.slice(0, 18)}…</span>
              <span className="text-xs text-gray-500">노드 {c.node_count} · 마지막 {c.last_at ? new Date(c.last_at).toLocaleString('ko-KR') : '-'}</span>
            </button>
          ))}
        </div>
      )}

      {/* Scope graph */}
      {scope && graph && (
        <>
          <div className="card p-3 flex items-center justify-between gap-3 flex-wrap">
            <div className="flex gap-1.5 flex-wrap items-center">
              {(Object.keys(TYPE_COLOR) as string[]).map((t) => (
                <label key={t} className="flex items-center gap-1 text-xs">
                  <input type="checkbox" checked={typeFilter[t] ?? true}
                    onChange={(e) => setTypeFilter({ ...typeFilter, [t]: e.target.checked })} />
                  <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{ background: TYPE_COLOR[t] }} />
                  {t}
                </label>
              ))}
            </div>
            <div className="flex items-center gap-2 text-xs">
              <span className="text-gray-400">축소 그룹 {graph.collapsed_groups}개</span>
              {graph.truncated && <span className="text-amber-600">예산 초과 — 잘림 (노드 상한 {graph.budget})</span>}
              <button className="btn-secondary text-xs" onClick={() => setZoom(zoom === 1 ? 1.6 : 1)}>{zoom === 1 ? '확대' : '축소'}</button>
            </div>
          </div>

          {listMode ? (
            /* Structured list alternative — an interactive view, not an export */
            <div className="card max-h-[520px] overflow-auto">
              <table className="w-full text-xs">
                <thead className="text-gray-500 text-left sticky top-0 bg-white"><tr>
                  <th className="py-2 px-3">시각</th><th>유형</th><th>레이블</th><th>상태</th><th>주체</th><th></th>
                </tr></thead>
                <tbody>
                  {laid.map((n) => (
                    <tr key={`${n.source_type}:${n.source_id}`} className="border-t">
                      <td className="py-1.5 px-3 text-gray-500">{new Date(n.occurred_at).toLocaleString('ko-KR')}</td>
                      <td><span className="px-1.5 py-0.5 rounded" style={{ background: TYPE_COLOR[n.node_type] + '22' }}>{n.node_type_ko}</span></td>
                      <td>{n.label_ko}</td>
                      <td>{n.status_ko}</td>
                      <td className="font-mono text-gray-400">{n.user_id || n.harness_id || '—'}</td>
                      <td><button className="text-sky-600" onClick={() => openNode(n)}>상세</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="card overflow-auto" role="application" aria-label="인과 그래프 캔버스 (키보드는 구조화 목록 보기를 사용하세요)">
              <svg width="100%" height="600" viewBox={`0 0 900 600`} style={{ transform: `scale(${zoom})`, transformOrigin: 'top left' }}>
                {/* Edges */}
                {graph.edges.map((e, i) => {
                  const from = nodeIndex.get(e.from) ?? nodeIndex.get(e.from.replace('action-set', 'session'))
                  const to = nodeIndex.get(e.to)
                  if (!from || !to) return null
                  return (
                    <g key={i}>
                      <line x1={from.x + 40} y1={from.y + 12} x2={to.x + 40} y2={to.y + 12} stroke="#cbd5e1" strokeWidth={1.2} markerEnd="url(#arrow)" />
                      <title>{`${EDGE_KO[e.type] || e.type} — 근거: ${e.evidence}`}</title>
                    </g>
                  )
                })}
                <defs>
                  <marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto">
                    <path d="M0,0 L0,6 L7,3 z" fill="#cbd5e1" />
                  </marker>
                </defs>
                {/* Nodes */}
                {laid.map((n) => {
                  const key = `${n.source_type}:${n.source_id}`
                  const isSel = selected && `${selected.source_type}:${selected.source_id}` === key
                  return (
                    <g key={key} transform={`translate(${n.x},${n.y})`} onClick={() => openNode(n)} style={{ cursor: 'pointer' }}
                      role="button" aria-label={`${n.node_type_ko} ${n.label_ko} ${n.status_ko}`}>
                      {TYPE_SHAPE[n.node_type] === 'diamond' ? (
                        <polygon points="40,0 80,14 40,28 0,14" fill={TYPE_COLOR[n.node_type]} opacity={isSel ? 1 : 0.85} />
                      ) : TYPE_SHAPE[n.node_type] === 'triangle' ? (
                        <polygon points="40,0 80,28 0,28" fill={TYPE_COLOR[n.node_type]} opacity={isSel ? 1 : 0.85} />
                      ) : (
                        <rect x="0" y="0" width="80" height="26" rx="13" fill={TYPE_COLOR[n.node_type]} opacity={isSel ? 1 : 0.85} />
                      )}
                      <text x="40" y="17" textAnchor="middle" fontSize="10" fill="#fff">{n.node_type_ko}</text>
                      <text x="40" y="40" textAnchor="middle" fontSize="9" fill="#475569">{n.label_ko.slice(0, 18)}</text>
                    </g>
                  )
                })}
              </svg>
            </div>
          )}

          {/* Node detail panel */}
          {selected && (
            <div className="card p-4">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="px-1.5 py-0.5 rounded text-xs" style={{ background: TYPE_COLOR[selected.node_type] + '22' }}>{selected.node_type_ko}</span>
                    <span className="font-medium text-sm">{selected.label_ko}</span>
                    <span className={`text-[11px] px-1.5 py-0.5 rounded ${selected.status === 'ok' ? 'bg-emerald-100 text-emerald-700' : 'bg-red-100 text-red-700'}`}>{selected.status_ko}</span>
                  </div>
                  <div className="text-xs text-gray-500 mt-2 space-y-0.5">
                    <div>시각 {new Date(selected.occurred_at).toLocaleString('ko-KR')}</div>
                    {detail?.user_id && <div>주체 사용자 <span className="font-mono">{detail.user_id}</span> · 하네스 <span className="font-mono">{detail.harness_id || '—'}</span> (이벤트 시점 기준)</div>}
                    {detail?.session_id && <div>세션 <span className="font-mono">{detail.session_id.slice(0, 18)}…</span></div>}
                    {detail?.action_type && <div>조치 유형 {detail.action_type} · 정책 판정 {detail.verdict || '—'}</div>}
                    {detail?.branch && <div>브랜치 {detail.branch} · 귀속 {detail.attribution} · {detail.lines_added}+/{detail.lines_removed}-</div>}
                    <div>무결성 다이제스트 <span className="font-mono">{selected.integrity_digest?.slice(0, 16)}…</span></div>
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <button className="btn-secondary text-xs" onClick={() => neighbors(`${selected.source_type}:${selected.source_id}`)}>인접 탐색</button>
                  {selected.session_id && (
                    <button className="btn-secondary text-xs" onClick={() => enterScope('session', selected.session_id, `세션 ${selected.session_id.slice(0, 12)}…`)}>세션으로 이동</button>
                  )}
                </div>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function applyFilter(nodes: TNode[], filter: Record<string, boolean>): TNode[] {
  if (Object.keys(filter).length === 0) return nodes
  return nodes.filter((n) => filter[n.node_type] ?? true)
}
