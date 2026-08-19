import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { Modal, ModalFooter } from '../components/Modal'
import { formatRelative, formatShortTime } from '../utils/format'
import { showToast } from '../components/Toast'
import { resolveRepoSync, classifySyncError, treeViewState, ATTEMPT_RESULT_LABELS, SyncDataset } from '../repoSync'
import { scopedListRoute } from '../relationLinks'

function authHeaders(): Record<string, string> { const token = sessionStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }

// RepositoryDetail (repositories C3) — file browser, branches +
// protection, baselines, sensitivity heatmap, sessions, findings, and
// the webhook surface.
export default function RepositoryDetail() {  const { id: paramId } = useParams<{ id: string }>()
  const id = paramId || ''
  const [repo, setRepo] = useState<any>(null)
  const [branches, setBranches] = useState<any[]>([])
  const [baselines, setBaselines] = useState<any[]>([])
  const [heatmaps, setHeatmaps] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [activeSessionCount, setActiveSessionCount] = useState(0)
  const [findings, setFindings] = useState<any[]>([])
  const [treePath, setTreePath] = useState('')
  const [tree, setTree] = useState<any[]>([])
  const [fileContent, setFileContent] = useState<string | null>(null)
  const [filePath, setFilePath] = useState('')
  const [syncing, setSyncing] = useState(false)
  const [treeLoading, setTreeLoading] = useState(true)
  const [treeError, setTreeError] = useState<string | null>(null)
  const [datasetErrors, setDatasetErrors] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)

  const loadRepo = () => {
    if (!id) return
    api.getRepository(id).then(setRepo).catch(() => setRepo(null)).finally(() => setLoading(false))
    api.repoBranches(id).then(d => { setBranches(Array.isArray(d) ? d : []); setDatasetErrors(e => ({ ...e, branches: '' })) }).catch((err: any) => setDatasetErrors(e => ({ ...e, branches: err?.message || 'load failed' })))
    api.repoBaselines(id).then(d => { setBaselines(Array.isArray(d) ? d : []); setDatasetErrors(e => ({ ...e, baselines: '' })) }).catch((err: any) => setDatasetErrors(e => ({ ...e, baselines: err?.message || 'load failed' })))
    api.repoHeatmap(id).then(d => { setHeatmaps(Array.isArray(d) ? d : []); setDatasetErrors(e => ({ ...e, heatmap: '' })) }).catch((err: any) => setDatasetErrors(e => ({ ...e, heatmap: err?.message || 'load failed' })))
    api.listSessionsPaged(`page=1&size=100&repository=${encodeURIComponent(id)}`).then((res: any) => setSessions(res?.data || [])).catch(() => {})
    // 활성 세션 카드 uses the paged endpoint's total, not the fetched page —
    // the list above caps at 100 rows and would silently undercount.
    api.listSessionsPaged(`page=1&size=1&status=active&repository=${encodeURIComponent(id)}`).then((res: any) => setActiveSessionCount(res?.total ?? 0)).catch(() => {})
    api.securityFindings({ repository: id }).then((d: any) => setFindings(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(() => { loadRepo() }, [id])

  const loadTree = (path: string) => {
    setTreePath(path)
    setTreeLoading(true)
    api.repoTree(id!, path)
      .then(d => { setTree(Array.isArray(d) ? d : []); setTreeError(null); setTreeLoading(false) })
      .catch((err: any) => { setTree([]); setTreeError(err?.message || 'unknown'); setTreeLoading(false) })
  }
  useEffect(() => { if (id && repo) loadTree('') }, [id, repo?.id])

  const openFile = (path: string) => {
    setFilePath(path)
    api.repoFile(id!, path)
      .then((d: any) => setFileContent(d?.content ?? ''))
      .catch(() => setFileContent(null))
  }

  const handleSync = async () => {
    if (syncing) return // idempotence: no duplicate jobs
    setSyncing(true)
    try {
      const res: any = await api.syncRepository(id!)
      showToast(`동기화 완료 · HEAD ${res.head?.slice(0, 8)}`, 'success')
      loadTree('')
      loadRepo()
    } catch (err: any) { showToast('동기화 실패: ' + err.message, 'error'); loadRepo() }
    finally { setSyncing(false) }
  }

  if (loading) return <div className="p-8 space-y-3 animate-pulse"><div className="h-4 bg-gray-100 rounded w-1/2" /></div>
  if (!repo) return (
    <div>
      <Link to="/repositories" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 저장소 목록</Link>
      <p className="text-gray-400 p-8 text-center">저장소를 찾을 수 없습니다</p>
    </div>
  )

  const heat = heatmaps[0]
  // Heatmap API returns risk_score (0..1); heat_score/level/findings_count
  // never existed, which produced the unexplained "민감도 점수: -" (PAT-1493).
  const heatScore = heat && heat.risk_score != null ? Math.round(heat.risk_score * 100) : null

  // Canonical sync status (PAT-1493) — same object shape as the list page,
  // plus per-dataset availability probed on this page.
  const treeUnavailableReason = treeError ? classifySyncError(treeError).label : undefined
  const datasets: SyncDataset[] = [
    { key: 'files', label: '파일 스냅샷', available: !treeError, reason: treeUnavailableReason },
    { key: 'branches', label: '브랜치', available: !datasetErrors.branches, reason: datasetErrors.branches ? '브랜치 정보를 불러올 수 없습니다' : undefined },
    { key: 'baselines', label: '베이스라인', available: !datasetErrors.baselines, reason: datasetErrors.baselines ? '베이스라인 정보를 불러올 수 없습니다' : undefined },
    { key: 'heatmap', label: '민감도 열지도', available: !datasetErrors.heatmap, reason: datasetErrors.heatmap ? '열지도 데이터를 불러올 수 없습니다' : undefined },
  ]
  const sync = resolveRepoSync(repo, { datasets })
  const treeView = treeViewState(treeLoading, tree, treeError, treePath)

  return (
    <div>
      <Link to="/repositories" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 저장소 목록</Link>

      <div className="card mb-6 flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold">{repo.name}</h1>
          <p className="text-xs text-gray-400 mt-1 font-mono">{repo.clone_url || repo.full_name || repo.id}</p>
        </div>
        <div className="flex gap-2 items-center shrink-0">
          {repo.project_id && <Link to={`/projects/${repo.project_id}`} className="btn-sm btn-secondary">프로젝트 →</Link>}
          <button onClick={handleSync} disabled={syncing} className="btn-sm btn-primary">{syncing ? '동기화 중...' : '🔄 동기화'}</button>
        </div>
      </div>

      {/* Canonical sync status (PAT-1493) — list and detail agree on phase and timestamps */}
      <div className="card mb-6">
        <div className="flex items-center gap-3 mb-2 flex-wrap">
          <h3 className="text-sm font-semibold">🔄 동기화 상태</h3>
          <span className={sync.badgeClass}>{sync.phaseLabel}</span>
          {sync.sourceRevision && <span className="text-xs text-gray-400 font-mono">리비전 {sync.sourceRevision.slice(0, 10)}</span>}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-2 text-xs text-gray-500">
          <div>마지막 성공: {formatShortTime(sync.lastSuccessAt)}</div>
          <div>
            최근 시도: {formatShortTime(sync.lastAttemptAt)}
            {sync.lastAttemptResult && ` · ${ATTEMPT_RESULT_LABELS[sync.lastAttemptResult]}`}
          </div>
          <div>소스 기준 커밋: {formatShortTime(repo.last_commit_at)}</div>
        </div>
        {sync.lastError && (
          <div className="mt-2 text-xs text-red-600">
            {classifySyncError(sync.lastError).label} <span className="text-gray-400 font-mono">({sync.lastError})</span>
          </div>
        )}
        <div className="mt-2 flex flex-wrap gap-2">
          {datasets.map(d => (
            <span key={d.key} title={d.reason || ''} className={`text-[10px] px-2 py-0.5 rounded-full border ${d.available ? 'border-green-200 bg-green-50 text-green-700' : 'border-amber-200 bg-amber-50 text-amber-700'}`}>
              {d.label} {d.available ? '사용 가능' : `사용 불가${d.reason ? ` — ${d.reason}` : ''}`}
            </span>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-5 gap-3 stat-grid mb-6">
        <StatCard label="브랜치" value={branches.length} accent="blue" />
        <StatCard label="베이스라인" value={baselines.length} accent="green" />
        <StatCard label="활성 세션" value={activeSessions.length} accent="purple" to={scopedListRoute('sessions', { parent: { entity: 'repository', id }, status: 'active' })} sub="활성 세션 목록" />
        <StatCard label="보안 발견" value={findings.length} accent="red" to={scopedListRoute('security', { parent: { entity: 'repository', id }, tab: 'findings' })} sub="저장소 발견 목록" />
        <StatCard label="민감도" value={repo.sensitivity || 'internal'} accent={repo.sensitivity === 'restricted' || repo.sensitivity === 'confidential' ? 'red' : 'yellow'} />
      </div>

      {/* File browser (C2) */}
      <div className="card mb-4">
        <div className="flex justify-between items-center mb-3">
          <h3 className="text-sm font-semibold">📂 파일 브라우저 · File Browser</h3>
          <span className="text-xs text-gray-400 font-mono">/{treePath}</span>
        </div>
        {treeView.state === 'loading' ? (
          <div className="p-8 space-y-3 animate-pulse"><div className="h-4 bg-gray-100 rounded w-3/4" /><div className="h-4 bg-gray-100 rounded w-1/2" /></div>
        ) : treeView.state === 'unavailable' ? (
          <div className="text-center py-8">
            <p className="text-sm text-gray-500 mb-1">동기화 데이터를 사용할 수 없습니다</p>
            <p className="text-xs text-gray-400 mb-3">{treeView.error.label}</p>
            <button onClick={handleSync} disabled={syncing} className="btn-primary text-sm">{syncing ? '동기화 중...' : '🔄 지금 동기화'}</button>
          </div>
        ) : treeView.state === 'empty' ? (
          <p className="text-xs text-gray-400 px-3 py-6 text-center">
            {treeView.scope === 'repository'
              ? '빈 저장소입니다 — 스냅샷에 커밋된 파일이 없습니다'
              : '빈 폴더입니다 — 이 경로에 파일이 없습니다'}
          </p>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="border border-gray-100 rounded-lg max-h-80 overflow-y-auto">
              {treePath !== '' && (
                <button onClick={() => loadTree(treePath.split('/').slice(0, -1).join('/'))} className="w-full text-left text-xs text-blue-600 hover:bg-gray-50 px-3 py-2 border-b border-gray-50">⬆ 상위 폴더</button>
              )}
              {tree.map((e: any) => (
                <button key={e.path} onClick={() => e.dir ? loadTree(e.path) : openFile(e.path)}
                  className="w-full flex items-center gap-2 text-left text-xs px-3 py-2 hover:bg-gray-50 border-b border-gray-50 last:border-0">
                  <span>{e.dir ? '📁' : '📄'}</span>
                  <span className="font-mono flex-1 truncate">{e.name}</span>
                  {!e.dir && e.size !== undefined && <span className="text-gray-400 flex-shrink-0">{e.size} B</span>}
                </button>
              ))}
            </div>
            <div className="border border-gray-100 rounded-lg">
              <div className="text-xs text-gray-400 px-3 py-2 border-b border-gray-100 font-mono truncate">{filePath || '파일을 선택하세요'}</div>
              {fileContent !== null ? (
                <pre className="text-xs font-mono p-3 max-h-72 overflow-auto whitespace-pre-wrap">{fileContent}</pre>
              ) : <p className="text-xs text-gray-400 p-3">왼쪽에서 파일을 선택하면 내용이 표시됩니다</p>}
            </div>
          </div>
        )}
      </div>

      {/* Branches + protection */}
      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">🌿 브랜치 · Branches ({branches.length})</h3>
        {branches.length === 0 ? <p className="text-xs text-gray-400">브랜치 없음</p> : (
          <div className="space-y-1">
            {branches.map((b: any) => (
              <div key={b.id || b.name} className="flex items-center gap-3 text-sm p-2 bg-gray-50 rounded">
                <span className="font-mono text-xs">{b.name}</span>
                <span className={`badge-gray text-[10px]`}>{b.protection_level || 'standard'}</span>
                {b.requires_approval && <span className="text-[10px] text-yellow-600">승인 필수</span>}
                {b.baseline_commit && <span className="text-xs text-gray-400 font-mono">기준 {b.baseline_commit.slice(0, 10)}</span>}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Baselines */}
      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">🏷 베이스라인 · Baselines ({baselines.length}) · §18.3</h3>
        {baselines.length === 0 ? <p className="text-xs text-gray-400">기록된 베이스라인 없음 — 저장소 목록에서 기록할 수 있습니다</p> : (
          <div className="space-y-1">
            {baselines.map((b: any) => (
              <div key={b.id} className="flex items-center gap-3 text-sm p-2 bg-gray-50 rounded flex-wrap">
                <span className="font-mono text-xs">{b.branch}</span>
                <span className="font-mono text-xs text-blue-600">{b.commit_sha?.slice(0, 10)}</span>
                <span className="text-xs text-gray-500 truncate flex-1">{b.commit_message || ''}</span>
                <span className="text-xs text-gray-400 flex-shrink-0">{b.committed_at ? formatRelative(b.committed_at) : formatRelative(b.created_at)}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Sensitivity heatmap (§33.5) */}
      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">🔥 민감도 열지도 · Sensitivity Heatmap (§33.5)</h3>
        {datasetErrors.heatmap ? (
          <p className="text-xs text-gray-400">열지도 데이터 사용 불가 — {datasetErrors.heatmap}</p>
        ) : heat ? (
          <div className="space-y-2">
            <div className="flex items-center gap-3">
              <div className="flex-1">
                {heatScore !== null ? (
                  <>
                    <div className="text-xs text-gray-500 mb-1">민감도 점수: {heatScore} / 100</div>
                    <div className="h-2 bg-gray-100 rounded overflow-hidden">
                      <div className={`h-full ${heatScore >= 70 ? 'bg-red-500' : heatScore >= 40 ? 'bg-yellow-500' : 'bg-green-500'}`} style={{ width: `${Math.min(heatScore, 100)}%` }} />
                    </div>
                  </>
                ) : (
                  <div className="text-xs text-gray-500 mb-1">민감도 점수 산정 불가 — 위험 점수가 계산되지 않았습니다</div>
                )}
              </div>
              <span className={`badge-${heatScore !== null && heatScore >= 70 ? 'red' : heatScore !== null && heatScore >= 40 ? 'yellow' : 'green'}`}>{heat.sensitivity || repo.sensitivity}</span>
            </div>
            <div className="text-xs text-gray-400">
              산정 출처: 저장소 민감도({heat.sensitivity || repo.sensitivity}) 기본 점수 + 보안 발견 {heat.security_findings ?? 0}건 · AI 세션 {heat.ai_sessions ?? 0} · 조회 시점 실시간 산정
            </div>
          </div>
        ) : <p className="text-xs text-gray-400">열지도 데이터 없음 — 저장소가 열지도 집계에 포함되지 않았습니다</p>}
      </div>

      {/* Sessions + findings */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">◐ 세션 · Sessions ({sessions.length})</h3>
          {sessions.length === 0 ? <p className="text-xs text-gray-400">세션 없음</p> : (
            <div className="space-y-1">
              {sessions.slice(0, 8).map((s: any) => (
                <div key={s.id} className="text-xs flex items-center gap-2">
                  <Link to={`/sessions/${s.session_id || s.id}`} className="text-blue-600 hover:underline flex-1 truncate">{s.title || '제목 없음'}</Link>
                  <span className={s.status === 'active' ? 'text-green-600' : 'text-gray-400'}>{s.status}</span>
                </div>
              ))}
            </div>
          )}
        </div>
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">🛡 보안 발견 · Findings ({findings.length})</h3>
          {findings.length === 0 ? <p className="text-xs text-gray-400">발견 없음</p> : (
            <div className="space-y-1">
              {findings.slice(0, 8).map((f: any) => (
                <div key={f.id} className="text-xs flex items-center gap-2">
                  <Link to={`/findings/${f.id}`} className="text-blue-600 hover:underline flex-1 truncate">{f.title_ko || f.title}</Link>
                  <span className={f.severity === 'critical' || f.severity === 'high' ? 'text-red-600' : 'text-yellow-600'}>{f.severity}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
