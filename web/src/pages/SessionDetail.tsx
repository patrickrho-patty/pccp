import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { formatRelative } from '../utils/format'

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }

// SessionDetail (00 A7 /{entity}/:id) — deep-linkable session view with
// status, usage, provenance entry, and lifecycle actions.
export default function SessionDetail() {
  const { id } = useParams<{ id: string }>()
  const [session, setSession] = useState<any>(null)
  const [usage, setUsage] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    api.listSessions().then((d: any[]) => {
      const sess = (Array.isArray(d) ? d : []).find((s: any) => s.id === id || s.session_id === id)
      setSession(sess || null)
      setLoading(false)
    }).catch(() => setLoading(false))
    fetch(`/api/sessions/${id}/usage`, { headers: authHeaders() })
      .then(r => r.json()).then(setUsage).catch(() => {})
  }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!session) return (
    <div>
      <Link to="/sessions" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 세션 목록</Link>
      <p className="text-gray-400 p-8 text-center">세션을 찾을 수 없습니다</p>
    </div>
  )

  const statusBadge = (s: string) =>
    s === 'active' ? 'badge-green' : s === 'idle' ? 'badge-yellow' : s === 'terminated' ? 'badge-red' : 'badge-gray'

  return (
    <div>
      <Link to="/sessions" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 세션 목록</Link>
      <div className="card mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold">{session.title || '제목 없음'}</h1>
          <p className="text-xs text-gray-400 mt-1 font-mono">{session.session_id}</p>
        </div>
        <div className="flex gap-2 items-center">
          <span className={statusBadge(session.status)}>{session.status}</span>
          <span className="badge-gray">{session.protection_profile || 'P0'}</span>
        </div>
      </div>

      <div className="grid grid-cols-4 gap-3 stat-grid mb-6">
        <StatCard label="입력 토큰" value={usage?.input_tokens ?? '-'} accent="blue" />
        <StatCard label="출력 토큰" value={usage?.output_tokens ?? '-'} accent="green" />
        <StatCard label="총 토큰" value={usage?.total_tokens ?? '-'} accent="purple" />
        <StatCard label="모델 클래스" value={session.model_class || '-'} accent="gray" />
      </div>

      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">세션 정보 · Session</h3>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">하네스:</span> <Link to={`/harnesses/${session.harness_id}`} className="text-blue-600 hover:underline font-mono text-xs">{session.harness_id}</Link></div>
          <div><span className="text-gray-500">사용자:</span> <Link to={`/users/${session.user_id}`} className="text-blue-600 hover:underline">{session.user_id}</Link></div>
          <div><span className="text-gray-500">프로젝트:</span> {session.project_id ? <Link to={`/projects/${session.project_id}`} className="text-blue-600 hover:underline">{session.project_id}</Link> : '-'}</div>
          <div><span className="text-gray-500">저장소:</span> {session.repository_id ? <Link to={`/repositories/${session.repository_id}`} className="text-blue-600 hover:underline">{session.repository_id}</Link> : '-'}</div>
          <div><span className="text-gray-500">브랜치:</span> <span className="font-mono text-xs">{session.branch || '-'}</span></div>
          <div><span className="text-gray-500">정책 에포크:</span> <span className="font-mono text-xs">{session.policy_epoch_id?.slice(0, 20) || '-'}</span></div>
          <div><span className="text-gray-500">열림:</span> {formatRelative(session.opened_at)}</div>
          <div><span className="text-gray-500">마지막 활동:</span> {formatRelative(session.last_activity_at || session.opened_at)}</div>
        </div>
      </div>

      <div className="flex gap-2">
        <Link to={`/sessions/${session.session_id || session.id}/provenance`} className="btn-sm btn-secondary">🔗 프로바이던스 체인 →</Link>
        {session.status === 'active' && (
          <button className="btn-sm btn-secondary" onClick={async () => { await api.pauseSession(session.session_id || session.id); window.location.reload() }}>⏸ 일시정지</button>
        )}
        {(session.status === 'active' || session.status === 'paused') && (
          <button className="btn-sm btn-danger" onClick={async () => { await api.closeSession(session.session_id || session.id); window.location.reload() }}>종료 · Close</button>
        )}
      </div>
    </div>
  )
}
