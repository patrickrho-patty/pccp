import { useState, useEffect } from 'react'
import { api } from '../api'
import { Link } from 'react-router-dom'

export default function Sessions() {
  const [sessions, setSessions] = useState<any[]>([])

  useEffect(() => { api.listSessions().then(data => setSessions(Array.isArray(data) ? data : data || [])) }, [])

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { active: 'badge-green', pending: 'badge-yellow', closed: 'badge-gray', terminated: 'badge-red' }
    return map[s] || 'badge-gray'
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">세션 <span className="text-gray-400 text-lg font-normal">Sessions</span></h1>
      <div className="card">
        {sessions.length === 0 ? (
          <p className="text-gray-400 text-center py-8">세션이 없습니다</p>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">제목</th>
                <th className="pb-3">세션 ID</th>
                <th className="pb-3">모델</th>
                <th className="pb-3">상태</th>
                <th className="pb-3">프로바이던스</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={s.id} className="border-b border-gray-100 last:border-0">
                  <td className="py-3">
                    <div className="font-medium">{s.title || '제목 없음'}</div>
                    <div className="text-xs text-gray-400">{s.task_purpose}</div>
                  </td>
                  <td className="py-3 font-mono text-xs">{s.session_id?.slice(0, 25)}</td>
                  <td className="py-3 text-sm">{s.model_class}</td>
                  <td className="py-3"><span className={statusBadge(s.status)}>{s.status}</span></td>
                  <td className="py-3">
                    <Link to={`/sessions/${s.id}/provenance`} className="text-patty-600 text-sm hover:underline">보기 →</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
