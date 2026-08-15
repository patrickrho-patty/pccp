import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'

function authHeaders(): Record<string, string> { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }

export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>()
  const [project, setProject] = useState<any>(null)
  const [repos, setRepos] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [tab, setTab] = useState<'overview' | 'repos' | 'sessions'>('overview')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    Promise.all([
      fetch(`/api/projects/${id}`, { headers: authHeaders() }).then(r => r.json()).catch(() => null),
      api.listRepositories().then((d: any[]) => Array.isArray(d) ? d.filter((r: any) => r.project_id === id) : []),
      api.listSessions().then((d: any[]) => Array.isArray(d) ? d.filter((s: any) => s.project_id === id) : []),
    ]).then(([p, r, s]) => { setProject(p); setRepos(r); setSessions(s) }).finally(() => setLoading(false))
  }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!project) return <div className="text-gray-400 p-8 text-center">프로젝트를 찾을 수 없습니다</div>

  let allowedModels: string[] = []
  try { allowedModels = JSON.parse(project.allowed_model_classes || '[]') } catch {}

  return (
    <div>
      <Link to="/projects" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 프로젝트 목록</Link>
      <div className="card mb-6">
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-2xl font-bold">{project.name_ko || project.name}</h1>
            <p className="text-sm text-gray-400">{project.name} · {project.slug}</p>
            {project.description && <p className="text-sm text-gray-500 mt-2">{project.description}</p>}
          </div>
          <span className={project.status === 'archived' ? 'badge-gray' : 'badge-green'}>{project.status || 'active'}</span>
        </div>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'overview', label: '개요', en: 'Overview' },
          { id: 'repos', label: '저장소', en: 'Repos' },
          { id: 'sessions', label: '세션', en: 'Sessions' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.id === 'repos' && repos.length > 0 && `(${repos.length})`}
            {t.id === 'sessions' && sessions.length > 0 && `(${sessions.length})`}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="card grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">프로젝트명:</span> {project.name}</div>
          <div><span className="text-gray-500">한글명:</span> {project.name_ko || '-'}</div>
          <div><span className="text-gray-500">슬러그:</span> {project.slug || '-'}</div>
          <div><span className="text-gray-500">상태:</span> {project.status || 'active'}</div>
          <div><span className="text-gray-500">프로젝트 코드:</span> {project.project_code || '-'}</div>
          <div><span className="text-gray-500">계열사:</span> {project.group_affiliate || '-'}</div>
          <div className="col-span-2"><span className="text-gray-500">허용 모델:</span> {allowedModels.length > 0 ? allowedModels.map(m => <span key={m} className="text-[10px] bg-blue-50 text-blue-600 px-1.5 py-0.5 rounded mr-1">{m}</span>) : '-'}</div>
        </div>
      )}

      {tab === 'repos' && (
        <div className="card">
          {repos.length === 0 ? (
            <p className="text-gray-400 text-center py-8">연결된 저장소가 없습니다</p>
          ) : repos.map(r => (
            <div key={r.id} className="flex items-center gap-2 py-3 border-b border-gray-100 last:border-0">
              <Link to="/repositories" className="text-sm font-medium text-blue-600 hover:underline">{r.name}</Link>
              <span className="badge-gray">{r.scm_provider || 'git'}</span>
              {r.default_branch && <span className="text-xs text-gray-400 font-mono">{r.default_branch}</span>}
            </div>
          ))}
        </div>
      )}

      {tab === 'sessions' && (
        <div className="card">
          {sessions.length === 0 ? (
            <p className="text-gray-400 text-center py-8">세션 이력이 없습니다</p>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead><tr className="border-b text-left text-xs text-gray-500 uppercase"><th className="pb-3">제목</th><th className="pb-3">상태</th><th className="pb-3">시작일</th></tr></thead>
              <tbody>
                {sessions.map(s => (
                  <tr key={s.id} className="border-b border-gray-100">
                    <td className="py-3 text-sm"><Link to="/sessions" className="text-blue-600 hover:underline">{s.title || '제목 없음'}</Link></td>
                    <td className="py-3"><span className="badge-gray">{s.status}</span></td>
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
