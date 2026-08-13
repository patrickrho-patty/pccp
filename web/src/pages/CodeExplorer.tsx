import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'

export default function CodeExplorer() {
  const [repos, setRepos] = useState<any[]>([])
  const [selectedRepo, setSelectedRepo] = useState<string | null>(null)
  const [changeSets, setChangeSets] = useState<any[]>([])
  const [spans, setSpans] = useState<any[]>([])
  const [stats, setStats] = useState<any>(null)
  const [sessions, setSessions] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [selectedChange, setSelectedChange] = useState<any>(null)
  const [loading, setLoading] = useState(false)
  const [tab, setTab] = useState<'changes' | 'spans' | 'files'>('changes')

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

  const loadRepoData = async (repoId: string) => {
    setSelectedRepo(repoId)
    setSelectedChange(null)
    setLoading(true)

    try {
      const [provRes, statsRes] = await Promise.all([
        fetch(`/api/provenance/repos/${repoId}`, { headers: authHeaders() }).then(r => r.json()),
        fetch(`/api/provenance/repos/${repoId}/stats`, { headers: authHeaders() }).then(r => r.json()),
      ])

      setChangeSets(provRes?.change_sets || [])
      setSpans(provRes?.spans || [])
      setStats(statsRes || null)
    } catch {
      setChangeSets([])
      setSpans([])
      setStats(null)
    }
    setLoading(false)
  }

  const getUserName = (uid: string) => {
    const u = users.find(u => u.id === uid)
    return u ? (u.name_ko || u.name) : '-'
  }
  const getSessionTitle = (sid: string) => sessions.find(s => s.session_id === sid || s.id === sid)

  const attrConfig: Record<string, { label: string; color: string; border: string; badge: string }> = {
    AI_GENERATED: { label: '🤖 AI 생성', color: 'text-green-600', border: 'border-l-green-500', badge: 'bg-green-100 text-green-700' },
    AI_THEN_HUMAN_EDITED: { label: '✏️ AI 후 수정', color: 'text-yellow-600', border: 'border-l-yellow-500', badge: 'bg-yellow-100 text-yellow-700' },
    HUMAN_THEN_AI_ASSISTED: { label: '🤝 인간+AI', color: 'text-blue-600', border: 'border-l-blue-500', badge: 'bg-blue-100 text-blue-700' },
    HUMAN_WRITTEN: { label: '👤 인간 작성', color: 'text-gray-600', border: 'border-l-gray-500', badge: 'bg-gray-100 text-gray-700' },
  }
  const getAttr = (state: string) => attrConfig[state] || attrConfig.AI_GENERATED

  // Group spans by file
  const fileGroups: Record<string, any[]> = {}
  spans.forEach(sp => {
    const f = sp.file_path || 'unknown'
    if (!fileGroups[f]) fileGroups[f] = []
    fileGroups[f].push(sp)
  })

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">코드 프로바이던스 탐색기 <span className="text-gray-400 text-lg font-normal">Code Provenance Explorer</span></h1>
      <p className="text-xs text-gray-400 mb-6">AI/인간 코드 기여 추적 · 변경셋 · 라인 수준 출처 · PRD §19</p>

      {/* Explanation */}
      <div className="card mb-6">
        <div className="flex items-start gap-3">
          <span className="text-2xl">🔬</span>
          <div className="flex-1">
            <h3 className="text-sm font-semibold">프로바이던스란? · What is Provenance?</h3>
            <p className="text-xs text-gray-500 mt-1">
              모든 코드 변경의 출처를 추적합니다: AI가 생성했는지, 인간이 작성했는지, AI 생성 후 인간이 수정했는지.
              각 변경은 세션, 모델, 사용자에 연결되어 감사 및 컴플라이언스 증거로 사용됩니다.
            </p>
            <div className="flex gap-4 mt-2 text-xs flex-wrap">
              {Object.entries(attrConfig).map(([key, cfg]) => (
                <span key={key} className="flex items-center gap-1">
                  <span className={`w-3 h-3 rounded border-l-2 ${cfg.border}`} />
                  {cfg.label}
                </span>
              ))}
            </div>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-12 gap-4">
        {/* Repository list */}
        <div className="col-span-3">
          <h3 className="text-sm font-semibold mb-2">저장소 · Repositories</h3>
          <div className="space-y-1 max-h-[600px] overflow-y-auto">
            {repos.map(r => {
              const repoIcon = { github: '🐙', gitlab: '🦊', bitbucket: '🪣', gitea: '🍵', git: '📦' }[r.scm_provider] || '📦'
              return (
                <div key={r.id} onClick={() => loadRepoData(r.id)}
                  className={`p-2 rounded cursor-pointer ${selectedRepo === r.id ? 'bg-blue-50 border-l-2 border-blue-400' : 'hover:bg-gray-50'}`}>
                  <div className="flex items-center gap-2">
                    <span>{repoIcon}</span>
                    <span className="text-sm font-medium">{r.name}</span>
                  </div>
                  <div className="text-xs text-gray-400 ml-5">{r.default_branch || 'main'}</div>
                </div>
              )
            })}
            {repos.length === 0 && <p className="text-xs text-gray-400 py-4">저장소가 없습니다</p>}
          </div>
        </div>

        {/* Main content */}
        <div className="col-span-9">
          {!selectedRepo ? (
            <div className="card text-center py-16">
              <div className="text-4xl mb-2">👈</div>
              <p className="text-gray-400">저장소를 선택하세요 · Select a repository</p>
            </div>
          ) : loading ? (
            <div className="card text-center py-16"><p className="text-gray-400">로딩 중...</p></div>
          ) : (
            <>
              {/* Stats overview */}
              {stats && (
                <div className="grid grid-cols-5 gap-3 mb-4">
                  <div className="card py-3 px-4 text-center">
                    <div className="text-2xl font-bold text-blue-600">{stats.total_changesets || 0}</div>
                    <div className="text-xs text-gray-500">총 변경</div>
                  </div>
                  <div className="card py-3 px-4 text-center">
                    <div className="text-2xl font-bold text-green-600">{stats.ai_generated || 0}</div>
                    <div className="text-xs text-gray-500">AI 생성</div>
                  </div>
                  <div className="card py-3 px-4 text-center">
                    <div className="text-2xl font-bold text-green-600">{stats.ai_percentage || '0%'}</div>
                    <div className="text-xs text-gray-500">AI 비율</div>
                  </div>
                  <div className="card py-3 px-4 text-center">
                    <div className="text-2xl font-bold text-green-600">+{stats.lines_added || 0}</div>
                    <div className="text-xs text-gray-500">추가 라인</div>
                  </div>
                  <div className="card py-3 px-4 text-center">
                    <div className="text-2xl font-bold text-red-600">-{stats.lines_removed || 0}</div>
                    <div className="text-xs text-gray-500">삭제 라인</div>
                  </div>
                </div>
              )}

              {/* Attribution bar */}
              {stats && (stats.total_changesets || 0) > 0 && (
                <div className="card mb-4 py-3 px-4">
                  <div className="flex items-center gap-2 mb-2 text-xs">
                    <span className="font-medium">코드 기여 분포</span>
                  </div>
                  <div className="h-4 bg-gray-100 rounded-full overflow-hidden flex">
                    {stats.ai_generated > 0 && <div className="h-full bg-green-500" style={{ width: `${(stats.ai_generated / stats.total_changesets) * 100}%` }} title="AI Generated" />}
                    {stats.ai_then_human > 0 && <div className="h-full bg-yellow-500" style={{ width: `${(stats.ai_then_human / stats.total_changesets) * 100}%` }} title="AI then Human" />}
                    {stats.human_written > 0 && <div className="h-full bg-blue-500" style={{ width: `${(stats.human_written / stats.total_changesets) * 100}%` }} title="Human" />}
                  </div>
                  <div className="flex justify-between mt-1 text-[10px] text-gray-400">
                    <span>🤖 AI {stats.ai_generated || 0}</span>
                    <span>✏️ 수정 {stats.ai_then_human || 0}</span>
                    <span>👤 인간 {stats.human_written || 0}</span>
                  </div>
                </div>
              )}

              {/* Tabs */}
              <div className="flex gap-1 mb-4 border-b border-gray-200">
                {[
                  { id: 'changes', label: '변경셋', en: 'Changesets', count: changeSets.length },
                  { id: 'spans', label: '라인 출처', en: 'Spans', count: spans.length },
                  { id: 'files', label: '파일별', en: 'Files', count: Object.keys(fileGroups).length },
                ].map(t => (
                  <button key={t.id} onClick={() => setTab(t.id as any)}
                    className={`px-3 py-2 text-xs font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
                    {t.label} ({t.count})
                  </button>
                ))}
              </div>

              {/* Changesets tab */}
              {tab === 'changes' && (
                <div>
                  {changeSets.length === 0 ? (
                    <div className="card text-center py-12">
                      <p className="text-gray-400 text-sm">이 저장소에 변경 이력이 없습니다</p>
                      <p className="text-xs text-gray-400 mt-1">AI 세션이 코드를 생성하면 변경셋이 자동으로 기록됩니다</p>
                      <Link to="/sessions" className="btn-sm btn-secondary mt-3 inline-block">세션 보기 →</Link>
                    </div>
                  ) : (
                    <div className="space-y-2 max-h-[500px] overflow-y-auto">
                      {changeSets.map((cs, i) => {
                        const attr = getAttr(cs.attribution_state)
                        const sess = getSessionTitle(cs.session_id)
                        return (
                          <div key={cs.id || i} onClick={() => setSelectedChange(selectedChange === cs ? null : cs)}
                            className={`card border-l-4 ${attr.border} cursor-pointer ${selectedChange === cs ? 'ring-2 ring-blue-100' : ''}`}>
                            <div className="flex items-center gap-3">
                              <span className={`px-2 py-1 rounded text-xs ${attr.badge}`}>{attr.label}</span>
                              {cs.branch && <span className="text-xs font-mono text-gray-500">{cs.branch}</span>}
                              <span className="text-sm font-medium flex-1 truncate">{cs.diff_summary?.slice(0, 80) || `변경셋 #${i + 1}`}</span>
                              <span className="text-green-600 text-xs">+{cs.lines_added || 0}</span>
                              <span className="text-red-600 text-xs">-{cs.lines_removed || 0}</span>
                            </div>
                            <div className="flex items-center gap-3 mt-2 text-xs text-gray-400">
                              {sess && <Link to="/sessions" className="text-blue-600 hover:underline">{sess.title}</Link>}
                              {cs.user_id && <span>· <Link to="/users" className="text-blue-600 hover:underline">{getUserName(cs.user_id)}</Link></span>}
                              {cs.model_package_id && <span>· {cs.model_package_id}</span>}
                              {cs.status && <span>· {cs.status}</span>}
                              <span className="ml-auto">{cs.created_at?.slice(0, 16)}</span>
                            </div>
                            {/* Expanded detail */}
                            {selectedChange === cs && (
                              <div className="mt-3 pt-3 border-t border-gray-100 space-y-2">
                                {cs.files_changed && (
                                  <div>
                                    <div className="text-xs font-medium text-gray-500 mb-1">변경 파일</div>
                                    <div className="flex flex-wrap gap-1">
                                      {JSON.parse(cs.files_changed || '[]').map((f: string, idx: number) => (
                                        <code key={idx} className="text-xs bg-gray-100 px-1.5 py-0.5 rounded">{f}</code>
                                      ))}
                                    </div>
                                  </div>
                                )}
                                {cs.diff_summary && (
                                  <div>
                                    <div className="text-xs font-medium text-gray-500 mb-1">Diff 요약</div>
                                    <pre className="text-xs bg-gray-900 text-gray-300 rounded p-2 overflow-x-auto max-h-32 overflow-y-auto">{cs.diff_summary}</pre>
                                  </div>
                                )}
                                <div className="flex gap-2">
                                  <Link to="/sessions" className="btn-sm btn-secondary">세션 →</Link>
                                  <Link to="/audit" className="btn-sm btn-secondary">감사 →</Link>
                                  <Link to="/security" className="btn-sm btn-secondary">보안 →</Link>
                                </div>
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              )}

              {/* Spans tab */}
              {tab === 'spans' && (
                <div>
                  {spans.length === 0 ? (
                    <div className="card text-center py-12"><p className="text-gray-400 text-sm">라인 수준 출처 데이터가 없습니다</p></div>
                  ) : (
                    <table className="w-full">
                      <thead><tr className="border-b text-left text-xs text-gray-500">
                        <th className="pb-2">파일</th><th className="pb-2">라인</th><th className="pb-2">출처</th>
                        <th className="pb-2">신뢰도</th><th className="pb-2">모델</th>
                      </tr></thead>
                      <tbody>
                        {spans.slice(0, 50).map((sp, i) => {
                          const attr = getAttr(sp.attribution_state)
                          return (
                            <tr key={sp.id || i} className="border-b border-gray-100 last:border-0">
                              <td className="py-2 text-xs font-mono">{sp.file_path?.split('/').pop()}</td>
                              <td className="py-2 text-xs text-gray-500">{sp.start_line}-{sp.end_line}</td>
                              <td className="py-2"><span className={`text-xs ${attr.badge} px-1.5 py-0.5 rounded`}>{attr.label}</span></td>
                              <td className="py-2 text-xs">{sp.confidence ? `${(sp.confidence * 100).toFixed(0)}%` : '-'}</td>
                              <td className="py-2 text-xs text-gray-400">{sp.model_package_id || '-'}</td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  )}
                </div>
              )}

              {/* Files tab */}
              {tab === 'files' && (
                <div>
                  {Object.keys(fileGroups).length === 0 ? (
                    <div className="card text-center py-12"><p className="text-gray-400 text-sm">파일 데이터가 없습니다</p></div>
                  ) : (
                    <div className="space-y-2">
                      {Object.entries(fileGroups).map(([filePath, fileSpans]) => {
                        const aiCount = fileSpans.filter(s => s.attribution_state === 'AI_GENERATED').length
                        const humanCount = fileSpans.filter(s => s.attribution_state === 'HUMAN_WRITTEN').length
                        const total = fileSpans.length
                        const aiPct = total > 0 ? Math.round((aiCount / total) * 100) : 0
                        return (
                          <div key={filePath} className="card">
                            <div className="flex items-center justify-between">
                              <code className="text-sm font-mono">{filePath}</code>
                              <span className="badge-gray">{total} spans</span>
                            </div>
                            <div className="mt-2 h-2 bg-gray-100 rounded-full overflow-hidden flex">
                              <div className="h-full bg-green-500" style={{ width: `${aiPct}%` }} />
                              <div className="h-full bg-blue-500" style={{ width: `${100 - aiPct}%` }} />
                            </div>
                            <div className="flex justify-between mt-1 text-[10px] text-gray-400">
                              <span>🤖 AI {aiCount} ({aiPct}%)</span>
                              <span>👤 인간 {humanCount}</span>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
