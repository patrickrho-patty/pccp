import { useState, useEffect } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api } from '../api'

function authHeaders(): Record<string, string> { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }

export default function UserDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [user, setUser] = useState<any>(null)
  const [sessions, setSessions] = useState<any[]>([])
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [tab, setTab] = useState<'overview' | 'sessions' | 'harnesses' | 'audit'>('overview')
  const [auditEvents, setAuditEvents] = useState<any[]>([])
  const [enrollmentCode, setEnrollmentCode] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    Promise.all([
      api.getUser(id),
      api.listSessions().then((d: any[]) => d.filter((s: any) => s.user_id === id)),
      api.listHarnesses().then((d: any[]) => {
        const userSessions = sessions
        return d.filter((h: any) => h.allowed_users?.includes(id))
      }),
    ]).then(([u, sess, hrn]) => {
      setUser(u)
      setSessions(sess)
      setHarnesses(hrn)
    }).catch(() => {}).finally(() => setLoading(false))
    api.getUserAudit(id || '').then(d => setAuditEvents(Array.isArray(d) ? d : [])).catch(() => {})
  }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!user) return <div className="text-gray-400 p-8 text-center">사용자를 찾을 수 없습니다</div>

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { active: 'badge-green', suspended: 'badge-yellow', offboarded: 'badge-gray' }
    return map[s] || 'badge-gray'
  }
  const statusLabel = (s: string) => ({ active: '활성', suspended: '정지', offboarded: '퇴사' } as any)[s] || s

  return (
    <div>
      <Link to="/users" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 사용자 목록</Link>

      <div className="card mb-6">
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-2xl font-bold">{user.name_ko || user.name}</h1>
            <p className="text-sm text-gray-400">{user.name} · {user.email}</p>
          </div>
          <span className={statusBadge(user.status)}>{statusLabel(user.status)}</span>
        </div>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'overview', label: '개요', en: 'Overview' },
          { id: 'sessions', label: '세션', en: 'Sessions' },
          { id: 'harnesses', label: '하네스', en: 'Harnesses' },
          { id: 'audit', label: '감사', en: 'Audit' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.id === 'sessions' && sessions.length > 0 && `(${sessions.length})`}
            {t.id === 'harnesses' && harnesses.length > 0 && `(${harnesses.length})`}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="card grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">이메일:</span> {user.email}</div>
          <div><span className="text-gray-500">한글 이름:</span> {user.name_ko || '-'}</div>
          <div><span className="text-gray-500">직함:</span> {user.title || '-'}</div>
          <div><span className="text-gray-500">인증 방식:</span> {user.auth_method}</div>
          <div><span className="text-gray-500">부서:</span> {user.business_unit_id || '-'}</div>
          <div><span className="text-gray-500">상태:</span> <span className={statusBadge(user.status)}>{statusLabel(user.status)}</span></div>
          <div><span className="text-gray-500">로케일:</span> {user.locale || 'ko-KR'}</div>
          <div><span className="text-gray-500">타임존:</span> {user.timezone || 'Asia/Seoul'}</div>
          <div><span className="text-gray-500">사번:</span> {user.employee_id || '-'}</div>
          <div><span className="text-gray-500">등록일:</span> {user.created_at?.slice(0, 10)}</div>
          {user.last_login_at && <div><span className="text-gray-500">마지막 로그인:</span> {user.last_login_at?.slice(0, 19)}</div>}
          <div className="col-span-2 pt-3 border-t border-gray-100">
            <button onClick={async () => {
              try {
                await fetch('/api/communications/conversations', {
                  method: 'POST',
                  headers: { ...authHeaders(), 'Content-Type': 'application/json' },
                  body: JSON.stringify({ type: 'direct', title: user.name_ko || user.name, participant_ids: [id] })
                })
                navigate('/communications')
              } catch {}
            }} className="btn-sm btn-primary">💬 메시지 보내기</button>
            <button onClick={async () => {
              try {
                const res = await fetch(`/api/users/${id}/enrollment-code`, { method: 'POST', headers: authHeaders() })
                const data = await res.json()
                setEnrollmentCode(data.code)
              } catch {}
            }} className="btn-sm btn-secondary ml-2">🔑 초대 코드</button>
            {enrollmentCode && <span className="text-xs font-mono text-blue-600 ml-2 select-all">{enrollmentCode}</span>}
          </div>
        </div>
      )}

      {tab === 'sessions' && (
        <div className="card">
          {sessions.length === 0 ? (
            <p className="text-gray-400 text-center py-8">세션 이력이 없습니다</p>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead><tr className="border-b text-left text-xs text-gray-500 uppercase">
                <th className="pb-3">제목</th><th className="pb-3">모델</th><th className="pb-3">상태</th><th className="pb-3">시작일</th>
              </tr></thead>
              <tbody>
                {sessions.map(s => (
                  <tr key={s.id} className="border-b border-gray-100">
                    <td className="py-3 text-sm"><Link to="/sessions" className="text-blue-600 hover:underline">{s.title || '제목 없음'}</Link></td>
                    <td className="py-3 text-sm">{s.model_class}</td>
                    <td className="py-3"><span className="badge-gray">{s.status}</span></td>
                    <td className="py-3 text-xs text-gray-400">{s.opened_at?.slice(0, 10)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {tab === 'harnesses' && (
        <div className="card">
          {harnesses.length === 0 ? (
            <p className="text-gray-400 text-center py-8">연결된 하네스가 없습니다</p>
          ) : (
            harnesses.map(h => (
              <div key={h.id} className="flex items-center justify-between py-3 border-b border-gray-100 last:border-0">
                <div>
                  <Link to="/harnesses" className="text-sm font-mono text-blue-600 hover:underline">{h.harness_id?.slice(0, 30)}</Link>
                  <span className="ml-2 badge-gray">{h.status}</span>
                </div>
                <span className="text-xs text-gray-400">v{h.binary_version}</span>
              </div>
            ))
          )}
        </div>
      )}

      {tab === 'audit' && (
        <div className="card">
          {auditEvents.length === 0 ? (
            <p className="text-gray-400 text-center py-8">감사 이력이 없습니다</p>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead><tr className="border-b text-left text-xs text-gray-500 uppercase"><th className="pb-3">시간</th><th className="pb-3">이벤트</th><th className="pb-3">결과</th></tr></thead>
              <tbody>
                {auditEvents.map((e, i) => (
                  <tr key={i} className="border-b border-gray-100">
                    <td className="py-3 text-xs text-gray-400">{e.occurred_at?.slice(0, 19)}</td>
                    <td className="py-3 text-sm">{e.action || e.event_type}</td>
                    <td className="py-3"><span className="badge-gray">{e.result || '-'}</span></td>
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
