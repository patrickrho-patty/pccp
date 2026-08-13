import { useState, useEffect, Fragment } from 'react'
import { api } from '../api'
import { Link } from 'react-router-dom'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'
import EmptyState from '../components/EmptyState'
import { formatRelative } from '../utils/format'
import { exportCSV } from '../utils/csv'
import { showToast } from '../components/Toast'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['title', 'task_purpose', 'session_id', 'harness_id'],
  searchPlaceholder: '제목, 목적, 세션 ID로 검색...',
  dropdowns: [
    {
      key: 'status',
      label: '상태',
      options: [
        { value: 'active', label: '🟢 활성' },
        { value: 'paused', label: '🟡 일시정지' },
        { value: 'closed', label: '⚪ 종료' },
        { value: 'terminated', label: '🔴 강제종료' },
        { value: 'completed', label: '✅ 완료' },
      ],
    },
    {
      key: 'model_class',
      label: '모델',
      options: [
        { value: 'pmp_qwen3_moe_v1', label: 'Qwen3 MoE' },
        { value: 'patty-code-standard', label: 'Patty Code Standard' },
        { value: 'patty-code-pro', label: 'Patty Code Pro' },
      ],
    },
  ],
}

export default function Sessions() {
  const [sessions, setSessions] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [repos, setRepos] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [viewMode, setViewMode] = useState<'table' | 'cards'>('table')
  const [timeline, setTimeline] = useState<any>(null)
  const [exchanges, setExchanges] = useState<any[]>([])
  const [inspectorSession, setInspectorSession] = useState<any>(null)
  const [inspectorExchanges, setInspectorExchanges] = useState<any[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [provenance, setProvenance] = useState<any>(null)
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })
  const [page, setPage] = useState(1)
  const pageSize = 25
  const [selectedSessions, setSelectedSessions] = useState<Set<string>>(new Set())
  const [form, setForm] = useState({ user_id: '', project_id: '', repository_id: '', branch: '', title: '', task_purpose: '', model_class: 'patty-code-standard' })

  const load = () => {
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : []))
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
    api.listProjects().then(data => setProjects(Array.isArray(data) ? data : []))
    api.listRepositories().then(data => setRepos(Array.isArray(data) ? data : []))
    api.listHarnesses().then(data => setHarnesses(Array.isArray(data) ? data : []))
  }
  useEffect(() => {
    load()
    // Auto-refresh for live updates
    const interval = setInterval(load, 10000)
    return () => clearInterval(interval)
  }, [])

  const filtered = useFilteredData(sessions, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const orgId = sessions[0]?.organization_id || users[0]?.organization_id || ''
    const harnessId = harnesses[0]?.harness_id || 'hrn_dev'
    try {
      await api.openSession({ ...form, organization_id: orgId, harness_id: harnessId })
      setShowForm(false)
      setForm({ user_id: '', project_id: '', repository_id: '', branch: '', title: '', task_purpose: '', model_class: 'patty-code-standard' })
      load()
    } catch (err: any) { showToast('세션 생성 실패: ' + err.message) }
  }

  const handleClose = async (id: string) => { if (confirm('종료하시겠습니까?')) { try { await api.closeSession(id); load() } catch {} } }
  const handlePause = async (id: string) => { try { await api.pauseSession(id); load() } catch {} }
  const handleResume = async (id: string) => { try { await api.resumeSession(id); load() } catch {} }

  const [usageData, setUsageData] = useState<Record<string, any>>({})
  const toggleExpand = async (session: any) => {
    if (expandedId === session.id) { setExpandedId(null); return }
    setExpandedId(session.id)
    try { const chain = await api.getProvenance(session.id); setProvenance(chain) } catch { setProvenance(null) }
    // Fetch usage data
    try {
      const res = await fetch(`/api/sessions/${session.id}/usage`, { headers: authHeaders() })
      if (res.ok) { const usage = await res.json(); setUsageData(prev => ({ ...prev, [session.id]: usage })) }
    } catch {}
    // Fetch timeline
    try {
      const res = await fetch(`/api/sessions/${session.id}/timeline`, { headers: authHeaders() })
      if (res.ok) { const tl = await res.json(); setTimeline(tl) }
    } catch {}
    // Fetch conversation history
    try {
      const res = await fetch(`/api/sessions/${session.id}/exchanges`, { headers: authHeaders() })
      if (res.ok) { const ex = await res.json(); setExchanges(Array.isArray(ex) ? ex : []) }
    } catch {}
  }

  const getUserName = (userId: string) => {
    const u = users.find(u => u.id === userId)
    return u?.name_ko || u?.name || userId?.slice(0, 8)
  }

  const formatDuration = (openedAt: string, closedAt?: string) => {
    if (!openedAt) return '-'
    const start = new Date(openedAt).getTime()
    const end = closedAt && closedAt !== '0001-01-01T00:00:00Z' ? new Date(closedAt).getTime() : Date.now()
    const diff = end - start
    const mins = Math.floor(diff / 60000)
    if (mins < 60) return `${mins}분`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}시간 ${mins % 60}분`
    const days = Math.floor(hours / 24)
    return `${days}일 ${hours % 24}시간`
  }

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { active: 'badge-green', pending: 'badge-yellow', closed: 'badge-gray', terminated: 'badge-red', paused: 'badge-yellow' }
    return map[s] || 'badge-gray'
  }
  const statusLabel = (s: string) => {
    const map: Record<string, string> = { active: '활성', pending: '대기', closed: '종료', terminated: '강제종료', paused: '일시정지' }
    return map[s] || s
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">AI 세션 <span className="text-gray-400 text-lg font-normal">AI Sessions</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">{showForm ? '취소' : '+ 세션 시작'}</button>
        <button onClick={() => exportCSV(`sessions_${new Date().toISOString().slice(0,10)}.csv`, ['제목', '개발자', '모델', '브랜치', '상태', '시작일'], sessions.map(s => [s.title, s.user_id, s.model_class, s.branch, s.status, s.opened_at?.slice(0,10)]))} className="btn-sm btn-secondary ml-2">📥 CSV</button>
      </div>

      {showForm && (
        <form onSubmit={handleCreate} className="card mb-6">
          <h2 className="text-sm font-semibold mb-4">새 AI 코딩 세션 · New Session</h2>
          <div className="grid grid-cols-2 gap-4">
            <div><label className="label">개발자 · Developer</label><select className="input" value={form.user_id} onChange={e => setForm({ ...form, user_id: e.target.value })} required><option value="">선택...</option>{users.map(u => <option key={u.id} value={u.id}>{u.name_ko || u.name} ({u.email})</option>)}</select></div>
            <div><label className="label">프로젝트 · Project</label><select className="input" value={form.project_id} onChange={e => setForm({ ...form, project_id: e.target.value })} required><option value="">선택...</option>{projects.map(p => <option key={p.id} value={p.id}>{p.name_ko || p.name}</option>)}</select></div>
            <div><label className="label">저장소 · Repository</label><select className="input" value={form.repository_id} onChange={e => setForm({ ...form, repository_id: e.target.value })}><option value="">선택 안함</option>{repos.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}</select></div>
            <div><label className="label">브랜치 · Branch</label><input className="input" value={form.branch} onChange={e => setForm({ ...form, branch: e.target.value })} placeholder="feature/refund" /></div>
            <div><label className="label">세션 제목 · Title</label><input className="input" value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} placeholder="환불 로직 구현" required /></div>
            <div><label className="label">모델 · Model</label><select className="input" value={form.model_class} onChange={e => setForm({ ...form, model_class: e.target.value })}><option value="patty-code-standard">Patty Code Standard</option><option value="patty-code-pro">Patty Code Pro</option></select></div>
            <div className="col-span-2"><label className="label">작업 목적 · Task Purpose</label><input className="input" value={form.task_purpose} onChange={e => setForm({ ...form, task_purpose: e.target.value })} placeholder="payment refund processing" /></div>
          </div>
          <button type="submit" className="btn-primary mt-4">세션 시작 · Start Session</button>
        </form>
      )}

      {selectedSessions.size > 0 && (
        <div className="flex items-center gap-3 mb-4 p-3 bg-blue-50 rounded-lg">
          <span className="text-sm font-medium text-blue-700">{selectedSessions.size}개 세션 선택됨</span>
          <button onClick={async () => { for (const id of selectedSessions) { try { await api.closeSession(id) } catch {} } setSelectedSessions(new Set()); load() }} className="btn-sm btn-secondary">일괄 종료</button>
          <button onClick={async () => { for (const id of selectedSessions) { try { await api.pauseSession(id) } catch {} } setSelectedSessions(new Set()); load() }} className="btn-sm btn-secondary">일괄 일시정지</button>
          <button onClick={() => setSelectedSessions(new Set())} className="btn-sm btn-secondary">취소</button>
        </div>
      )}
      {/* View mode toggle */}
      <div className="flex items-center gap-2 mb-4">
        <button onClick={() => setViewMode('table')} className={`btn-sm ${viewMode === 'table' ? 'btn-primary' : 'btn-secondary'}`}>표</button>
        <button onClick={() => setViewMode('cards')} className={`btn-sm ${viewMode === 'cards' ? 'btn-primary' : 'btn-secondary'}`}>실시간 카드</button>
        <span className="text-xs text-gray-400 ml-auto">🟢 {sessions.filter(s => s.status === 'active').length}개 활성 · {sessions.length}개 전체</span>
      </div>

      {/* Card view (live sessions) */}
      {viewMode === 'cards' && (
        <div className="grid grid-cols-3 gap-4 mb-6">
          {sessions.filter(s => s.status === 'active' || s.status === 'paused').map(s => (
            <div key={s.id} className="card cursor-pointer hover:shadow-md transition-shadow" onClick={() => toggleExpand(s)}>
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className={`w-2 h-2 rounded-full ${s.status === 'active' ? 'bg-green-500 animate-pulse' : 'bg-yellow-500'}`} />
                  <span className="text-sm font-medium">{s.title || '제목 없음'}</span>
                </div>
              </div>
              <div className="text-xs text-gray-500 mb-2">
                <Link to={`/users/${s.user_id}`} className="text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>{getUserName(s.user_id)}</Link>
                <span className="text-gray-400 ml-1">· {s.model_class}</span>
              </div>
              <div className="bg-gray-900 rounded p-3 font-mono text-[11px] text-gray-300 min-h-[50px]">
                <div className={s.status === 'active' ? 'text-green-400' : 'text-yellow-400'}>▸ {statusLabel(s.status)}</div>
                <div className="text-gray-500 mt-1">지속: {formatDuration(s.opened_at, s.closed_at)}</div>
                <div className="text-gray-500">브랜치: {s.branch || '-'}</div>
              </div>
            </div>
          ))}
          {sessions.filter(s => s.status === 'active' || s.status === 'paused').length === 0 && (
            <div className="col-span-3 card"><EmptyState icon="📡" title="활성 세션이 없습니다" message="세션을 시작하면 실시간으로 표시됩니다" /></div>
          )}
        </div>
      )}

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        {paged.length === 0 ? (
          <div className="py-4"><EmptyState icon={filters.search ? '🔍' : '◐'} title={filters.search ? '검색 결과가 없습니다' : '세션이 없습니다'} message={filters.search ? '다른 검색어로 시도해보세요' : '세션 시작 버튼으로 새 세션을 열어보세요'} /></div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3 w-8"><input type="checkbox" onChange={(e) => { if (e.target.checked) setSelectedSessions(new Set(paged.map(s => s.id))); else setSelectedSessions(new Set()) }} /></th>
                <th className="pb-3">제목 · Title</th>
                <th className="pb-3">개발자</th>
                <th className="pb-3">모델</th>
                <th className="pb-3">브랜치</th>
                <th className="pb-3">상태</th>
                <th className="pb-3 text-right">작업</th>
              </tr>
            </thead>
            <tbody>
              {paged.map(s => (
                <Fragment key={s.id}>
                  <tr className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer" onClick={() => toggleExpand(s)}>
                    <td className="py-3" onClick={e => e.stopPropagation()}><input type="checkbox" checked={selectedSessions.has(s.id)} onChange={() => { const next = new Set(selectedSessions); if (next.has(s.id)) next.delete(s.id); else next.add(s.id); setSelectedSessions(next) }} /></td>
                    <td className="py-3"><div className="font-medium text-sm">{s.title || '제목 없음'}</div><div className="text-xs text-gray-400">{s.task_purpose}</div></td>
                    <td className="py-3 text-sm"><Link to={`/users/${s.user_id}`} className="text-blue-600 hover:underline">{getUserName(s.user_id)}</Link></td>
                    <td className="py-3 text-sm">{s.model_class}</td>
                    <td className="py-3 text-sm font-mono">{s.branch || '-'}</td>
                    <td className="py-3 text-xs text-gray-500">{formatDuration(s.opened_at, s.closed_at)}</td>
                    <td className="py-3"><span className={statusBadge(s.status)}>{statusLabel(s.status)}</span></td>
                    <td className="py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex gap-2 justify-end">
                        {s.status === 'active' && (<><button onClick={() => handlePause(s.id)} className="text-yellow-600 text-xs hover:underline">일시정지</button><button onClick={() => handleClose(s.id)} className="text-red-600 text-xs hover:underline">종료</button></>)}
                        {s.status === 'paused' && <button onClick={() => handleResume(s.id)} className="text-green-600 text-xs hover:underline">재개</button>}
                        <button onClick={async () => { setInspectorSession(s); try { const res = await fetch(`/api/sessions/${s.id}/exchanges`, { headers: authHeaders() }); if (res.ok) { setInspectorExchanges(await res.json()) } } catch {} }} className="text-blue-600 text-xs hover:underline">상세 검사</button>
                        <Link to={`/sessions/${s.id}/provenance`} className="text-blue-600 text-xs hover:underline">프로바이던스</Link>
                      </div>
                    </td>
                  </tr>
                  {expandedId === s.id && (
                    <tr className="bg-gray-50"><td colSpan={7} className="p-4">
                      <div className="grid grid-cols-3 gap-4 text-sm">
                        <div><span className="text-gray-500">세션 ID:</span> <span className="font-mono text-xs">{s.session_id?.slice(0, 30)}</span></div>
                        <div><span className="text-gray-500">하네스:</span> <Link to={`/harnesses/${s.harness_id}`} className="font-mono text-xs text-blue-600 hover:underline">{s.harness_id}</Link></div>
                        <div><span className="text-gray-500">개발자:</span> <Link to={`/users/${s.user_id}`} className="text-blue-600 hover:underline">{getUserName(s.user_id)}</Link></div>
                        <div><span className="text-gray-500">시작:</span> <span className="text-xs">{formatRelative(s.opened_at)}</span></div>
                      </div>
                      {usageData[s.id] && (
                        <div className="mt-2 pt-2 border-t border-gray-200">
                          <div className="text-xs font-semibold text-gray-600 mb-1">토큰 사용량 · Token Usage</div>
                          <div className="flex gap-4 text-xs">
                            <span>입력: <strong className="text-blue-600">{(usageData[s.id].input_tokens || 0).toLocaleString()}</strong></span>
                            <span>출력: <strong className="text-green-600">{(usageData[s.id].output_tokens || 0).toLocaleString()}</strong></span>
                            <span>총: <strong>{(usageData[s.id].total_tokens || 0).toLocaleString()}</strong></span>
                            <span>추론 수: <strong>{usageData[s.id].total_records || 0}</strong></span>
                          </div>
                        </div>
                      )}
                      {provenance && provenance.actions?.length > 0 && (
                        <div className="mt-3 pt-3 border-t border-gray-200"><div className="text-xs font-semibold text-gray-600 mb-2">액션 이력 ({provenance.actions.length})</div>
                        <div className="space-y-1">{provenance.actions.slice(0, 5).map((a: any, i: number) => (<div key={i} className="flex items-center gap-2 text-xs"><span className="text-blue-500">●</span><span className="font-mono">{a.action_type}</span><span className="text-gray-400">{a.occurred_at?.slice(0, 19)}</span><span className="badge-green">{a.verdict_result}</span></div>))}</div></div>
                      )}
                    </td></tr>
                  )}
</Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Pagination total={filtered.length} page={page} pageSize={pageSize} onPageChange={setPage} />

      {/* Session Inspector Modal */}
      {inspectorSession && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50" onClick={() => setInspectorSession(null)}>
          <div className="bg-white rounded-xl shadow-xl max-w-4xl w-full mx-4 max-h-[85vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
            <div className="p-5 border-b border-gray-100 flex items-center justify-between sticky top-0 bg-white z-10">
              <div>
                <h3 className="font-semibold">세션 검사기 · Session Inspector</h3>
                <p className="text-xs text-gray-500">{inspectorSession.title} · {inspectorSession.session_id?.slice(0, 20)}</p>
              </div>
              <button onClick={() => setInspectorSession(null)} className="text-gray-400 hover:text-gray-600">✕</button>
            </div>
            <div className="p-5 space-y-4">
              {/* Summary */}
              <div className="grid grid-cols-4 gap-3">
                <div className="bg-gray-50 rounded p-3">
                  <div className="text-xs text-gray-500">개발자</div>
                  <Link to={`/users/${inspectorSession.user_id}`} className="text-sm font-medium text-blue-600 hover:underline">{getUserName(inspectorSession.user_id)}</Link>
                </div>
                <div className="bg-gray-50 rounded p-3">
                  <div className="text-xs text-gray-500">모델</div>
                  <div className="text-sm font-medium">{inspectorSession.model_class || '-'}</div>
                </div>
                <div className="bg-gray-50 rounded p-3">
                  <div className="text-xs text-gray-500">상태</div>
                  <span className={statusBadge(inspectorSession.status)}>{statusLabel(inspectorSession.status)}</span>
                </div>
                <div className="bg-gray-50 rounded p-3">
                  <div className="text-xs text-gray-500">지속 시간</div>
                  <div className="text-sm font-medium">{formatDuration(inspectorSession.opened_at, inspectorSession.closed_at)}</div>
                </div>
              </div>

              {/* Timeline */}
              {timeline && (
                <>
                  {timeline.actions?.length > 0 && (
                    <div>
                      <h4 className="text-xs font-semibold text-gray-600 mb-2">액션 타임라인 · Actions ({timeline.actions.length})</h4>
                      <div className="space-y-1 max-h-48 overflow-y-auto">
                        {timeline.actions.slice(0, 20).map((a: any, i: number) => (
                          <div key={i} className="flex items-center gap-3 text-xs py-1 px-2 bg-gray-50 rounded">
                            <span className="font-mono text-gray-400 w-16">{a.occurred_at?.slice(11, 19)}</span>
                            <span className="font-medium w-32 truncate">{a.action_type || a.action || '-'}</span>
                            <span className="text-gray-500 truncate flex-1">{a.tool_name || a.description || '-'}</span>
                            <span className={`px-1.5 py-0.5 rounded text-[10px] ${a.outcome === 'success' ? 'bg-green-100 text-green-700' : a.outcome === 'error' ? 'bg-red-100 text-red-700' : 'bg-gray-100 text-gray-500'}`}>{a.outcome || a.policy_decision || '-'}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                  {timeline.change_sets?.length > 0 && (
                    <div>
                      <h4 className="text-xs font-semibold text-gray-600 mb-2">코드 변경 · Change Sets ({timeline.change_sets.length})</h4>
                      <div className="space-y-1">
                        {timeline.change_sets.slice(0, 10).map((cs: any, i: number) => (
                          <div key={i} className="flex items-center gap-2 text-xs p-2 bg-gray-50 rounded">
                            <span className={`px-1.5 py-0.5 rounded ${cs.ai_generated ? 'bg-green-100 text-green-700' : 'bg-blue-100 text-blue-700'}`}>{cs.ai_generated ? 'AI' : 'Human'}</span>
                            <span className="font-mono">{cs.commit_sha?.slice(0, 12) || '-'}</span>
                            <span className="text-gray-500 truncate">{cs.message || cs.title || '-'}</span>
                            <span className="text-gray-400 ml-auto">+{cs.additions || 0} -{cs.deletions || 0}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                  {timeline.findings?.length > 0 && (
                    <div>
                      <h4 className="text-xs font-semibold text-gray-600 mb-2">보안 발견 · Findings ({timeline.findings.length})</h4>
                      <div className="space-y-1">
                        {timeline.findings.map((f: any, i: number) => (
                          <div key={i} className="flex items-center gap-2 text-xs p-2 bg-red-50 rounded">
                            <span className={`px-1.5 py-0.5 rounded ${f.severity === 'critical' ? 'bg-red-200 text-red-800' : 'bg-yellow-200 text-yellow-800'}`}>{f.severity}</span>
                            <span className="font-medium">{f.finding_type}</span>
                            <Link to="/security" className="text-blue-600 hover:underline">{f.title_ko || f.title || '-'}</Link>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </>
              )}

              {/* Conversation History */}
              {inspectorExchanges && inspectorExchanges.length > 0 && (
                <div>
                  <h4 className="text-xs font-semibold text-gray-600 mb-2">대화 기록 · Conversation History ({inspectorExchanges.length})</h4>
                  <div className="space-y-3 max-h-96 overflow-y-auto">
                    {inspectorExchanges.map((ex, i) => (
                      <div key={i} className="border border-gray-100 rounded-lg p-3">
                        <div className="flex items-center gap-2 text-[10px] text-gray-400 mb-2">
                          <span>Exchange #{i + 1}</span>
                          {ex.model_package_id && <span>· {ex.model_package_id}</span>}
                          {ex.input_tokens > 0 && <span>· 입력: {ex.input_tokens} 토큰</span>}
                          {ex.output_tokens > 0 && <span>· 출력: {ex.output_tokens} 토큰</span>}
                          {ex.latency_ms > 0 && <span>· {ex.latency_ms}ms</span>}
                          <span className={`ml-auto px-1.5 py-0.5 rounded ${ex.verdict_result === 'allow' ? 'bg-green-100 text-green-700' : ex.verdict_result === 'deny' ? 'bg-red-100 text-red-700' : 'bg-gray-100 text-gray-500'}`}>{ex.verdict_result || ex.status}</span>
                        </div>
                        {ex.prompt_text && (
                          <div className="mb-2">
                            <div className="text-[10px] font-medium text-blue-600 mb-0.5">👤 프롬프트</div>
                            <pre className="text-xs bg-blue-50 rounded p-2 whitespace-pre-wrap font-mono overflow-x-auto max-h-32 overflow-y-auto">{ex.prompt_text}</pre>
                          </div>
                        )}
                        {ex.response_text && (
                          <div>
                            <div className="text-[10px] font-medium text-green-600 mb-0.5">🤖 응답</div>
                            <pre className="text-xs bg-green-50 rounded p-2 whitespace-pre-wrap font-mono overflow-x-auto max-h-32 overflow-y-auto">{ex.response_text}</pre>
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }