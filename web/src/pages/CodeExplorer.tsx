import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { classifySyncError } from '../repoSync'

const ATTRIBUTION_KO: Record<string, string> = {
  AI_GENERATED: 'AI 생성', AI_THEN_HUMAN_EDITED: 'AI 후 사람 수정',
  HUMAN_THEN_AI_ASSISTED: '사람 후 AI 지원', HUMAN_WRITTEN: '사람 작성',
}
const ATTRIBUTION_BADGE: Record<string, string> = {
  AI_GENERATED: 'bg-purple-50 text-purple-700 border-purple-200',
  AI_THEN_HUMAN_EDITED: 'bg-blue-50 text-blue-700 border-blue-200',
  HUMAN_THEN_AI_ASSISTED: 'bg-cyan-50 text-cyan-700 border-cyan-200',
  HUMAN_WRITTEN: 'bg-gray-100 text-gray-600 border-gray-200',
}

export default function CodeExplorer() {
  const [repos, setRepos] = useState<any[]>([])
  const [selectedRepo, setSelectedRepo] = useState<string>('')
  const [tree, setTree] = useState<any[]>([])
  const [treePath, setTreePath] = useState('')
  const [spans, setSpans] = useState<any[]>([])
  const [spanTotal, setSpanTotal] = useState(0)
  const [attribution, setAttribution] = useState<any[]>([])
  const [attributionFilter, setAttributionFilter] = useState('')
  const [spanPage, setSpanPage] = useState(1)
  const [fileFilter, setFileFilter] = useState('')
  const [blastTarget, setBlastTarget] = useState<any>(null)
  const [blast, setBlast] = useState<any>(null)
  const [tab, setTab] = useState<'attribution' | 'spans' | 'files'>('attribution')
  const [treeErr, setTreeErr] = useState('')
  const [syncing, setSyncing] = useState(false)

  const selectedRepoRow = repos.find((r: any) => r.id === selectedRepo)

  // Sync recovery action (PAT-1493) — reachable from the error state itself.
  const handleSync = async () => {
    if (!selectedRepo || syncing) return
    setSyncing(true)
    try {
      await api.syncRepository(selectedRepo)
      showToast('동기화 완료', 'success')
      loadTree(selectedRepo, treePath)
    } catch (e: any) { showToast('동기화 실패: ' + (e?.message || ''), 'error') }
    finally { setSyncing(false) }
  }

  useEffect(() => {
    api.listRepositories().then((d: any) => {
      const list = Array.isArray(d) ? d : (d?.data || [])
      setRepos(list)
      if (list.length && !selectedRepo) setSelectedRepo(list[0].id)
    }).catch(() => {})
  }, [])

  const loadTree = (repoId: string, path?: string) => {
    if (!repoId) return
    api.repoTree(repoId, path || '').then((d: any[]) => {
      setTree(Array.isArray(d) ? d : [])
      setTreeErr('')
    }).catch((e: any) => { setTree([]); setTreeErr(e?.message || '파일 브라우저 사용 불가 — 저장소를 먼저 동기화하세요 (Repositories)') })
  }
  const loadSpans = (repoId: string, page: number, file?: string, attr?: string) => {
    if (!repoId) return
    const q = new URLSearchParams({ repository: repoId, page: String(page), size: '50' })
    if (file) q.set('file', file)
    if (attr) q.set('attribution', attr)
    api.codeExplorerSpans(q.toString()).then((res: any) => {
      if (Array.isArray(res)) { setSpans(res); setSpanTotal(res.length) }
      else { setSpans(res.data || []); setSpanTotal(res.total || 0) }
    }).catch(() => { setSpans([]); setSpanTotal(0) })
  }
  const loadAttribution = (repoId: string) => {
    if (!repoId) return
    api.codeExplorerAttribution(repoId).then((d: any[]) => setAttribution(Array.isArray(d) ? d : [])).catch(() => setAttribution([]))
  }

  useEffect(() => {
    if (!selectedRepo) return
    loadTree(selectedRepo)
    loadSpans(selectedRepo, 1)
    loadAttribution(selectedRepo)
  }, [selectedRepo])

  const openBlast = async (filePath: string) => {
    setBlastTarget(filePath)
    setBlast(null)
    try {
      const res = await api.codeExplorerBlast(selectedRepo, filePath)
      setBlast(res)
    } catch (e: any) { setBlast(null); showToast(e?.message || '영향 분석 실패', 'error') }
  }

  const filteredAttribution = attribution.filter(a => !attributionFilter || a.state === attributionFilter)

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">코드 탐색기 · Code Explorer</h2>
          <p className="text-[11px] text-gray-400">파일 브라우저 → 라인 단위 출처(provenance) → 귀속 상태 → 변경 영향 분석</p>
        </div>
        <select className="input text-xs w-64" value={selectedRepo} onChange={e => { setSelectedRepo(e.target.value); setTreePath(''); setSpanPage(1); setFileFilter(''); setAttributionFilter('') }}>
          <option value="">저장소 선택...</option>
          {repos.map((r: any) => <option key={r.id} value={r.id}>{r.name || r.full_name}</option>)}
        </select>
      </div>

      {treeErr && (
        <div className="card p-3 border border-amber-200 bg-amber-50 flex items-center justify-between gap-2 flex-wrap">
          <div className="text-[11px] text-amber-700">
            파일 스냅샷 사용 불가 — {classifySyncError(treeErr).label}
            {attribution.length > 0 && (
              <span className="block text-amber-600 mt-0.5">
                귀속 데이터는 마지막 동기화({selectedRepoRow?.last_sync_at ? selectedRepoRow.last_sync_at.slice(0, 16).replace('T', ' ') : '기록 없음'}) 기준이며 현재 소스와 다를 수 있습니다.
              </span>
            )}
          </div>
          <div className="flex gap-2 shrink-0">
            <button className="btn-sm btn-primary" onClick={handleSync} disabled={syncing}>{syncing ? '동기화 중...' : '🔄 지금 동기화'}</button>
            {selectedRepo && <Link className="btn-sm btn-secondary" to={`/repositories/${selectedRepo}`}>저장소 상세 →</Link>}
          </div>
        </div>
      )}

      <div className="flex gap-1 border-b border-gray-200">
        {[
          { id: 'attribution', label: '귀속 (Attribution)' },
          { id: 'spans', label: '스팬 (Spans)' },
          { id: 'files', label: '파일 브라우저' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-3 py-2 text-xs ${tab === t.id ? 'border-b-2 border-blue-600 text-blue-600 font-semibold' : 'text-gray-500'}`}>
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'attribution' && (
        <div className="space-y-2">
          <div className="flex gap-2 items-center shrink-0">
            <select className="input text-xs w-40" value={attributionFilter} onChange={e => setAttributionFilter(e.target.value)}>
              <option value="">전체 귀속</option>
              {Object.entries(ATTRIBUTION_KO).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
          </div>
          {filteredAttribution.length === 0 && <EmptyState icon="🧬" title="출처 데이터 없음"
            message="하네스가 스팬을 보고하면 파일별 귀속이 여기에 나타납니다." />}
          {filteredAttribution.map((a: any) => (
            <div key={a.file_path} className="card p-2 flex items-center justify-between gap-2 text-[11px]">
              <button className="text-gray-700 font-mono truncate hover:underline text-left" onClick={() => { setTab('files'); loadTree(selectedRepo, a.file_path.split('/').slice(0, -1).join('/')) }}>
                {a.file_path}
              </button>
              <div className="flex items-center gap-2 shrink-0">
                <span className={`text-[10px] px-2 py-0.5 rounded-full border ${ATTRIBUTION_BADGE[a.state] || ''}`}>{ATTRIBUTION_KO[a.state] || a.state}</span>
                <span className="text-gray-400">{a.span_count} 스팬 · 신뢰 {((a.confidence || 0) * 100).toFixed(0)}%</span>
                <button className="text-[10px] px-2 py-1 rounded hover:bg-orange-50 text-orange-600" onClick={() => openBlast(a.file_path)}>영향</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {tab === 'spans' && (
        <div className="space-y-2">
          <div className="flex gap-2 flex-wrap">
            <input className="input text-xs w-56" placeholder="파일 경로 필터" value={fileFilter}
              onChange={e => setFileFilter(e.target.value)} />
            <button className="btn-sm btn-secondary" onClick={() => loadSpans(selectedRepo, 1, fileFilter, attributionFilter)}>적용</button>
          </div>
          {spans.length === 0 && <EmptyState icon="🧩" title="스팬 없음" message="라인 단위 출처 스팬이 여기에 표시됩니다." />}
          {spans.map((s: any) => (
            <div key={s.id} className="card p-2 flex items-center justify-between gap-2 text-[11px]">
              <div className="min-w-0">
                <div className="text-gray-700 font-mono truncate">{s.file_path}:{s.start_line}-{s.end_line} {s.symbol_name && <span className="text-gray-400">· {s.symbol_name}</span>}</div>
                <div className="text-[10px] text-gray-400">
                  {s.symbol_language || ''} · 세션 {s.session_id?.slice(0, 10) || '—'} · 모델 {s.model_package_id?.slice(0, 10) || '—'}
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <span className={`text-[10px] px-2 py-0.5 rounded-full border ${ATTRIBUTION_BADGE[s.attribution_state] || ''}`}>{ATTRIBUTION_KO[s.attribution_state] || s.attribution_state}</span>
                <Link className="text-[10px] text-blue-600 hover:underline" to={`/sessions/${s.session_id || ''}`}>세션</Link>
              </div>
            </div>
          ))}
          {spanTotal > 50 && (
            <div className="flex gap-1 justify-end text-[11px]">
              <button className="btn-sm btn-secondary" disabled={spanPage <= 1} onClick={() => { setSpanPage(p => Math.max(1, p - 1)); loadSpans(selectedRepo, spanPage - 1, fileFilter, attributionFilter) }}>이전</button>
              <span className="px-2 py-1 text-gray-500">{spanPage}</span>
              <button className="btn-sm btn-secondary" disabled={spans.length < 50} onClick={() => { setSpanPage(p => p + 1); loadSpans(selectedRepo, spanPage + 1, fileFilter, attributionFilter) }}>다음</button>
            </div>
          )}
        </div>
      )}

      {tab === 'files' && (
        <div className="space-y-2">
          <div className="text-[11px] text-gray-500 font-mono">/{treePath || ''}</div>
          <div className="space-y-1">
            {tree.map((entry: any) => (
              <div key={entry.path || entry.name} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1">
                {entry.is_dir ? (
                  <button className="text-gray-700 hover:underline font-mono" onClick={() => { setTreePath(entry.path || entry.name); loadTree(selectedRepo, entry.path || entry.name) }}>
                    📁 {entry.name}
                  </button>
                ) : (
                  <span className="text-gray-600 font-mono">📄 {entry.name}</span>
                )}
                {!entry.is_dir && (
                  <button className="text-[10px] text-orange-600 hover:underline" onClick={() => openBlast((treePath ? treePath + '/' : '') + entry.name)}>영향 분석</button>
                )}
              </div>
            ))}
            {tree.length === 0 && !treeErr && <p className="text-[11px] text-gray-400">디렉토리가 비어 있습니다</p>}
          </div>
        </div>
      )}

      {blastTarget && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50" onClick={() => setBlastTarget(null)}>
          <div className="bg-white rounded-xl shadow-xl max-w-lg w-full mx-4 p-5" onClick={e => e.stopPropagation()}>
            <h3 className="text-sm font-semibold">변경 영향 분석 — {blastTarget}</h3>
            {blast ? (
              <div className="mt-3 space-y-2 text-xs">
                <div className="text-gray-500">위험 점수: <span className="font-semibold">{blast.risk_score?.score ?? blast.risk_score?.total ?? '—'}</span></div>
                {blast.risk_score?.factors && (
                  <div className="space-y-1">
                    {blast.risk_score.factors.map((f: any, i: number) => (
                      <div key={i} className="text-gray-600">• {f.name || f.factor}: +{f.weight || f.score || 0}</div>
                    ))}
                  </div>
                )}
                <div className="text-gray-500">변경 심볼: {blast.span_count} 스팬</div>
                {blast.graph?.suggested_reviewers?.length > 0 && (
                  <div className="text-gray-500">추천 리뷰어: {blast.graph.suggested_reviewers.join(', ')}</div>
                )}
              </div>
            ) : <p className="mt-3 text-xs text-gray-400">분석 중...</p>}
            <div className="flex justify-end mt-4"><button className="btn-sm btn-secondary" onClick={() => setBlastTarget(null)}>닫기</button></div>
          </div>
        </div>
      )}
    </div>
  )
}
