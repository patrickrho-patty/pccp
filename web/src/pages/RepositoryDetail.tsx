import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { Modal, ModalFooter } from '../components/Modal'
import { formatRelative } from '../utils/format'
import { showToast } from '../components/Toast'

function authHeaders(): Record<string, string> { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }

// RepositoryDetail (repositories C3) — file browser, branches +
// protection, baselines, sensitivity heatmap, sessions, findings, and
// the webhook surface.
export default function RepositoryDetail() {
  const { id } = useParams<{ id: string }>()
  const [repo, setRepo] = useState<any>(null)
  const [branches, setBranches] = useState<any[]>([])
  const [baselines, setBaselines] = useState<any[]>([])
  const [heatmaps, setHeatmaps] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [findings, setFindings] = useState<any[]>([])
  const [treePath, setTreePath] = useState('')
  const [tree, setTree] = useState<any[]>([])
  const [fileContent, setFileContent] = useState<string | null>(null)
  const [filePath, setFilePath] = useState('')
  const [syncing, setSyncing] = useState(false)
  const [notSynced, setNotSynced] = useState(false)
  const [loading, setLoading] = useState(true)

  const loadRepo = () => {
    if (!id) return
    api.getRepository(id).then(setRepo).catch(() => setRepo(null)).finally(() => setLoading(false))
    api.repoBranches(id).then(d => setBranches(Array.isArray(d) ? d : [])).catch(() => {})
    api.repoBaselines(id).then(d => setBaselines(Array.isArray(d) ? d : [])).catch(() => {})
    api.repoHeatmap().then(d => setHeatmaps((Array.isArray(d) ? d : []).filter((h: any) => h.repository_id === id))).catch(() => {})
    api.listSessions().then((d: any[]) => setSessions((Array.isArray(d) ? d : []).filter((s: any) => s.repository_id === id))).catch(() => {})
    api.securityFindings().then((d: any) => setFindings((Array.isArray(d) ? d : []).filter((f: any) => sessions.some((s: any) => s.session_id === f.session_id)))).catch(() => {})
  }
  useEffect(() => { loadRepo() }, [id])

  const loadTree = (path: string) => {
    setTreePath(path)
    api.repoTree(id!, path)
      .then(d => { setTree(Array.isArray(d) ? d : []); setNotSynced(false) })
      .catch(() => setNotSynced(true))
  }
  useEffect(() => { if (id && repo) loadTree('') }, [id, repo?.id])

  const openFile = (path: string) => {
    setFilePath(path)
    api.repoFile(id!, path)
      .then((d: any) => setFileContent(d?.content ?? ''))
      .catch(() => setFileContent(null))
  }

  const handleSync = async () => {
    setSyncing(true)
    try {
      const res: any = await api.syncRepository(id!)
      showToast(`동기화 완료 · HEAD ${res.head?.slice(0, 8)}`, 'success')
      setNotSynced(false)
      loadTree('')
      loadRepo()
    } catch (err: any) { showToast('동기화 실패: ' + err.message, 'error') }
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
  const activeSessions = sessions.filter((s: any) => s.status === 'active')

  return (
    <div>
      <Link to="/repositories" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 저장소 목록</Link>

      <div className="card mb-6 flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold">{repo.name}</h1>
          <p className="text-xs text-gray-400 mt-1 font-mono">{repo.clone_url || repo.full_name || repo.id}</p>
        </div>
        <div className="flex gap-2 items-center">
          {repo.project_id && <Link to={`/projects/${repo.project_id}`} className="btn-sm btn-secondary">프로젝트 →</Link>}
          <button onClick={handleSync} disabled={syncing} className="btn-sm btn-primary">{syncing ? '동기화 중...' : '🔄 동기화'}</button>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-5 gap-3 stat-grid mb-6">
        <StatCard label="브랜치" value={branches.length} accent="blue" />
        <StatCard label="베이스라인" value={baselines.length} accent="green" />
        <StatCard label="활성 세션" value={activeSessions.length} accent="purple" to="/sessions" />
        <StatCard label="보안 발견" value={findings.length} accent="red" to="/security" />
        <StatCard label="민감도" value={repo.sensitivity || 'internal'} accent={repo.sensitivity === 'restricted' || repo.sensitivity === 'confidential' ? 'red' : 'yellow'} />
      </div>

      {/* File browser (C2) */}
      <div className="card mb-4">
        <div className="flex justify-between items-center mb-3">
          <h3 className="text-sm font-semibold">📂 파일 브라우저 · File Browser</h3>
          <span className="text-xs text-gray-400 font-mono">/{treePath}</span>
        </div>
        {notSynced ? (
          <div className="text-center py-8">
            <p className="text-sm text-gray-500 mb-2">아직 동기화되지 않았습니다</p>
            <button onClick={handleSync} className="btn-primary text-sm">🔄 지금 동기화</button>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="border border-gray-100 rounded-lg max-h-80 overflow-y-auto">
              {treePath !== '' && (
                <button onClick={() => loadTree(treePath.split('/').slice(0, -1).join('/'))} className="w-full text-left text-xs text-blue-600 hover:bg-gray-50 px-3 py-2 border-b border-gray-50">⬆ 상위 폴더</button>
              )}
              {tree.length === 0 && <p className="text-xs text-gray-400 px-3 py-2">빈 폴더</p>}
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
        {heat ? (
          <div className="space-y-2">
            <div className="flex items-center gap-3">
              <div className="flex-1">
                <div className="text-xs text-gray-500 mb-1">민감도 점수: {heat.heat_score ?? '-'}</div>
                <div className="h-2 bg-gray-100 rounded overflow-hidden">
                  <div className={`h-full ${(heat.heat_score || 0) >= 70 ? 'bg-red-500' : (heat.heat_score || 0) >= 40 ? 'bg-yellow-500' : 'bg-green-500'}`} style={{ width: `${Math.min(heat.heat_score || 0, 100)}%` }} />
                </div>
              </div>
              <span className={`badge-${(heat.heat_score || 0) >= 70 ? 'red' : (heat.heat_score || 0) >= 40 ? 'yellow' : 'green'}`}>{heat.level || repo.sensitivity}</span>
            </div>
            {heat.findings_count !== undefined && <div className="text-xs text-gray-500">발견: {heat.findings_count}건</div>}
          </div>
        ) : <p className="text-xs text-gray-400">열지도 데이터 없음 — 보안 발견이 기록되면 채워집니다</p>}
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
