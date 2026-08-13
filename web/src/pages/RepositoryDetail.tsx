import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }

export default function RepositoryDetail() {
  const { id } = useParams<{ id: string }>()
  const [repo, setRepo] = useState<any>(null)
  const [sessions, setSessions] = useState<any[]>([])
  const [tab, setTab] = useState<'overview' | 'sessions'>('overview')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    Promise.all([
      fetch(`/api/repositories/${id}`, { headers: authHeaders() }).then(r => r.json()).catch(() => null),
      api.listSessions().then((d: any[]) => Array.isArray(d) ? d.filter((s: any) => s.repository_id === id) : []),
    ]).then(([r, s]) => { setRepo(r); setSessions(s) }).finally(() => setLoading(false))
  }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!repo) return <div className="text-gray-400 p-8 text-center">저장소를 찾을 수 없습니다</div>

  return (
    <div>
      <Link to="/repositories" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 저장소 목록</Link>
      <div className="card mb-6">
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-2xl font-bold">{repo.name}</h1>
            {repo.full_name && <p className="text-sm text-gray-400">{repo.full_name}</p>}
            {repo.clone_url && <p className="text-xs text-gray-400 font-mono mt-1">{repo.clone_url}</p>}
          </div>
          <div className="flex gap-2">
            <span className="badge-gray">{repo.scm_provider || 'git'}</span>
            <span className="badge-gray">{repo.sensitivity || 'internal'}</span>
          </div>
        </div>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'overview', label: '개요', en: 'Overview' },
          { id: 'sessions', label: '세션', en: 'Sessions' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.id === 'sessions' && sessions.length > 0 && `(${sessions.length})`}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="card grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">이름:</span> {repo.name}</div>
          <div><span className="text-gray-500">Full Name:</span> {repo.full_name || '-'}</div>
          <div><span className="text-gray-500">SCM:</span> {repo.scm_type || 'git'} / {repo.scm_provider || '-'}</div>
          <div><span className="text-gray-500">Clone URL:</span> <span className="font-mono text-xs">{repo.clone_url || '-'}</span></div>
          <div><span className="text-gray-500">기본 브랜치:</span> <span className="font-mono">{repo.default_branch || 'main'}</span></div>
          <div><span className="text-gray-500">민감도:</span> {repo.sensitivity || 'internal'}</div>
          <div><span className="text-gray-500">상태:</span> {repo.status || 'active'}</div>
          <div><span className="text-gray-500">생성일:</span> {repo.created_at?.slice(0, 10)}</div>
        </div>
      )}

      {tab === 'sessions' && (
        <div className="card">
          {sessions.length === 0 ? (
            <p className="text-gray-400 text-center py-8">이 저장소의 세션이 없습니다</p>
          ) : (
            <table className="w-full">
              <thead><tr className="border-b text-left text-xs text-gray-500 uppercase"><th className="pb-3">제목</th><th className="pb-3">상태</th><th className="pb-3">브랜치</th><th className="pb-3">시작일</th></tr></thead>
              <tbody>
                {sessions.map(s => (
                  <tr key={s.id} className="border-b border-gray-100">
                    <td className="py-3 text-sm"><Link to="/sessions" className="text-blue-600 hover:underline">{s.title || '제목 없음'}</Link></td>
                    <td className="py-3"><span className="badge-gray">{s.status}</span></td>
                    <td className="py-3 text-xs font-mono text-gray-400">{s.branch || '-'}</td>
                    <td className="py-3 text-xs text-gray-400">{s.opened_at?.slice(0, 10)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}
