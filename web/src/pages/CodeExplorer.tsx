import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'

export default function CodeExplorer() {
  const [repos, setRepos] = useState<any[]>([])
  const [selectedRepo, setSelectedRepo] = useState<string | null>(null)
  const [changesets, setChangesets] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [selectedChange, setSelectedChange] = useState<any>(null)

  const authHeaders = () => { const t = localStorage.getItem('pccp_token'); return t ? { Authorization: `Bearer ${t}` } : {} }

  useEffect(() => {
    Promise.all([
      fetch('/api/repositories', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/api/sessions', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
      fetch('/api/users', { headers: authHeaders() }).then(r => r.json()).catch(() => []),
    ]).then(([r, s, u]) => {
      setRepos(Array.isArray(r) ? r : [])
      setSessions(Array.isArray(s) ? s : [])
      setUsers(Array.isArray(u) ? u : [])
    })
  }, [])

  const loadChangesets = (repoId: string) => {
    setSelectedRepo(repoId)
    // Get sessions for this repo, then get their changesets
    const repo = repos.find(r => r.id === repoId)
    if (!repo) return
    const repoSessions = sessions.filter(s => s.repository_id === repoId || s.project_id === repo.project_id)

    // Try to fetch changesets from timeline endpoint
    Promise.all(repoSessions.map(s => 
      fetch(`/api/sessions/${s.id}/timeline`, { headers: authHeaders() })
        .then(r => r.json()).catch(() => null)
    )).then(results => {
      const allChanges: any[] = []
      results.forEach((tl, idx) => {
        if (tl?.change_sets) {
          tl.change_sets.forEach((cs: any) => {
            allChanges.push({ ...cs, session: repoSessions[idx], repo })
          })
        }
      })
      setChangesets(allChanges)
    })
  }

  const getUserName = (uid: string) => users.find(u => u.id === uid)
  const getSessionTitle = (sid: string) => sessions.find(s => s.id === sid || s.session_id === sid)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">코드 프로바이던스 탐색기 <span className="text-gray-400 text-lg font-normal">Code Provenance Explorer</span></h1>
      <p className="text-xs text-gray-400 mb-6">AI/인간 코드 기여 추적 · 세션별 변경 이력 · PRD §19 Line-Level Provenance</p>

      {/* Explanation */}
      <div className="card mb-6">
        <div className="flex items-start gap-3">
          <span className="text-2xl">🔬</span>
          <div>
            <h3 className="text-sm font-semibold">프로바이던스란?</h3>
            <p className="text-xs text-gray-500 mt-1">
              모든 코드 변경을 AI 생성인지 인간 수정인지 추적합니다. 각 변경은 세션, 모델, 사용자에 연결되어
              감사 및 컴플라이언스 증거로 사용됩니다.
            </p>
            <div className="flex gap-4 mt-2 text-xs">
              <span className="flex items-center gap-1"><span className="w-3 h-3 bg-green-200 rounded border-l-2 border-green-500" /> AI 생성</span>
              <span className="flex items-center gap-1"><span className="w-3 h-3 bg-blue-200 rounded border-l-2 border-blue-500" /> 인간 수정</span>
              <span className="flex items-center gap-1"><span className="w-3 h-3 bg-yellow-200 rounded border-l-2 border-yellow-500" /> AI 후 인간 수정</span>
            </div>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-12 gap-4">
        {/* Repository list */}
        <div className="col-span-3">
          <h3 className="text-sm font-semibold mb-2">저장소 · Repositories</h3>
          <div className="space-y-1">
            {repos.map(r => (
              <div key={r.id} onClick={() => loadChangesets(r.id)}
                className={`p-2 rounded cursor-pointer text-sm ${selectedRepo === r.id ? 'bg-blue-50 border-l-2 border-blue-400' : 'hover:bg-gray-50'}`}>
                <div className="font-medium">{r.name}</div>
                <div className="text-xs text-gray-400">{r.scm_provider} · {r.default_branch}</div>
              </div>
            ))}
            {repos.length === 0 && <p className="text-xs text-gray-400 py-4">저장소가 없습니다</p>}
          </div>
        </div>

        {/* Changeset list */}
        <div className="col-span-4">
          <h3 className="text-sm font-semibold mb-2">변경 이력 · Changes</h3>
          {!selectedRepo ? (
            <p className="text-xs text-gray-400 py-4">저장소를 선택하세요</p>
          ) : changesets.length === 0 ? (
            <div className="text-center py-8">
              <p className="text-xs text-gray-400">이 저장소에 변경 이력이 없습니다</p>
              <p className="text-xs text-gray-400 mt-1">AI 세션이 코드를 생성하면 여기에 표시됩니다</p>
            </div>
          ) : (
            <div className="space-y-1 max-h-96 overflow-y-auto">
              {changesets.map((cs, i) => {
                const session = cs.session
                const user = session ? getUserName(session.user_id) : null
                return (
                  <div key={i} onClick={() => setSelectedChange(cs)}
                    className={`p-2 rounded cursor-pointer text-xs border-l-2 ${selectedChange === cs ? 'bg-blue-50' : ''} ${
                      cs.ai_generated ? 'border-l-green-500' : 'border-l-blue-500'
                    }`}>
                    <div className="flex items-center gap-2 mb-1">
                      <span className={`px-1.5 py-0.5 rounded text-[10px] ${cs.ai_generated ? 'bg-green-100 text-green-700' : 'bg-blue-100 text-blue-700'}`}>
                        {cs.ai_generated ? '🤖 AI' : '👤 Human'}
                      </span>
                      {cs.commit_sha && <span className="font-mono text-gray-400">{cs.commit_sha.slice(0, 8)}</span>}
                    </div>
                    <div className="text-gray-700 truncate">{cs.message || cs.title || '제목 없음'}</div>
                    <div className="text-gray-400 mt-0.5">
                      {user && <Link to="/users" className="text-blue-600 hover:underline">{user.name_ko || user.name}</Link>}
                      {session && <span> · <Link to="/sessions" className="text-blue-600 hover:underline">{session.title}</Link></span>}
                    </div>
                    {(cs.additions > 0 || cs.deletions > 0) && (
                      <div className="mt-0.5">
                        <span className="text-green-600">+{cs.additions || 0}</span>
                        <span className="text-red-600 ml-1">-{cs.deletions || 0}</span>
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </div>

        {/* Detail panel */}
        <div className="col-span-5">
          <h3 className="text-sm font-semibold mb-2">상세 · Detail</h3>
          {!selectedChange ? (
            <p className="text-xs text-gray-400 py-4">변경을 선택하세요</p>
          ) : (
            <div className="card space-y-3">
              {/* Attribution */}
              <div className="flex items-center gap-2">
                <span className={`px-2 py-1 rounded text-xs ${selectedChange.ai_generated ? 'bg-green-100 text-green-700' : 'bg-blue-100 text-blue-700'}`}>
                  {selectedChange.ai_generated ? '🤖 AI 생성' : '👤 인간 작성'}
                </span>
                {selectedChange.commit_sha && <span className="font-mono text-xs text-gray-400">{selectedChange.commit_sha.slice(0, 16)}</span>}
              </div>

              {/* Message */}
              <div>
                <div className="text-xs font-medium text-gray-500">커밋 메시지</div>
                <div className="text-sm mt-1">{selectedChange.message || selectedChange.title || '-'}</div>
              </div>

              {/* Session info */}
              {selectedChange.session && (
                <div className="bg-gray-50 rounded p-3">
                  <div className="text-xs font-medium text-gray-500 mb-1">연결된 세션</div>
                  <Link to="/sessions" className="text-sm text-blue-600 hover:underline">{selectedChange.session.title}</Link>
                  <div className="text-xs text-gray-400 mt-1">
                    모델: {selectedChange.session.model_class || '-'} · 상태: {selectedChange.session.status}
                  </div>
                  {selectedChange.session.user_id && (() => {
                    const u = getUserName(selectedChange.session.user_id)
                    return u ? <div className="text-xs mt-1">개발자: <Link to="/users" className="text-blue-600 hover:underline">{u.name_ko || u.name}</Link></div> : null
                  })()}
                </div>
              )}

              {/* Diff stats */}
              {(selectedChange.additions > 0 || selectedChange.deletions > 0) && (
                <div>
                  <div className="text-xs font-medium text-gray-500">변경 통계</div>
                  <div className="text-sm mt-1">
                    <span className="text-green-600">+{selectedChange.additions || 0} 추가</span>
                    <span className="text-red-600 ml-2">-{selectedChange.deletions || 0} 삭제</span>
                  </div>
                </div>
              )}

              {/* Patch */}
              {selectedChange.patch && (
                <div>
                  <div className="text-xs font-medium text-gray-500 mb-1">패치</div>
                  <pre className="text-xs bg-gray-900 text-gray-300 rounded p-3 overflow-x-auto max-h-48 overflow-y-auto">{selectedChange.patch}</pre>
                </div>
              )}

              {/* Links */}
              <div className="flex gap-2 pt-2 border-t border-gray-100">
                <Link to="/sessions" className="btn-sm btn-secondary">세션 →</Link>
                <Link to="/audit" className="btn-sm btn-secondary">감사 →</Link>
                <Link to="/security" className="btn-sm btn-secondary">보안 →</Link>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
