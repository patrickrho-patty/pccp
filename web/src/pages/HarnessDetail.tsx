import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }

export default function HarnessDetail() {
  const { id } = useParams<{ id: string }>()
  const [harness, setHarness] = useState<any>(null)
  const [sessions, setSessions] = useState<any[]>([])
  const [tab, setTab] = useState<'overview' | 'sessions' | 'security' | 'audit'>('overview')
  const [auditEvents, setAuditEvents] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    Promise.all([
      fetch(`/api/harnesses/${id}`, { headers: authHeaders() }).then(r => r.json()).catch(() => null),
      api.listSessions().then((d: any[]) => Array.isArray(d) ? d.filter((s: any) => s.harness_id === id || s.harness_id === harness?.harness_id) : []),
    ]).then(([h, sess]) => {
      setHarness(h)
      // re-filter sessions by harness_id if we got the harness
      if (h?.harness_id) {
        api.listSessions().then((d: any[]) => setSessions(d.filter((s: any) => s.harness_id === h.harness_id))).catch(() => {})
      }
    }).finally(() => setLoading(false))
    api.getHarnessAudit(id || '').then(d => setAuditEvents(Array.isArray(d) ? d : [])).catch(() => {})
  }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!harness) return <div className="text-gray-400 p-8 text-center">하네스를 찾을 수 없습니다</div>

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { enrolled: 'badge-green', active: 'badge-green', quarantined: 'badge-red', revoked: 'badge-gray', pending: 'badge-yellow' }
    return map[s] || 'badge-gray'
  }
  const statusLabel = (s: string) => ({ enrolled: '등록됨', active: '활성', pending: '대기', quarantined: '격리됨', revoked: '폐기됨' } as any)[s] || s

  // Parse allowed users
  let allowedUsers: string[] = []
  try { allowedUsers = JSON.parse(harness.allowed_users || '[]') } catch {}

  return (
    <div>
      <Link to="/harnesses" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 하네스 목록</Link>

      <div className="card mb-6">
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-2xl font-bold font-mono">{harness.harness_id?.slice(0, 30)}</h1>
            <p className="text-sm text-gray-400">v{harness.binary_version} · {harness.build_channel || 'stable'}</p>
          </div>
          <div className="flex gap-2">
            <span className={statusBadge(harness.status)}>{statusLabel(harness.status)}</span>
            <span className={harness.risk_state === 'high' ? 'badge-red' : harness.risk_state === 'elevated' ? 'badge-yellow' : 'badge-green'}>{harness.risk_state}</span>
          </div>
        </div>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'overview', label: '개요', en: 'Overview' },
          { id: 'sessions', label: '세션', en: 'Sessions' },
          { id: 'security', label: '보안', en: 'Security' },
          { id: 'audit', label: '감사', en: 'Audit' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.id === 'sessions' && sessions.length > 0 && `(${sessions.length})`}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="card grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">하네스 ID:</span> <span className="font-mono text-xs">{harness.harness_id}</span></div>
          <div><span className="text-gray-500">바이너리 버전:</span> v{harness.binary_version}</div>
          <div><span className="text-gray-500">빌드 해시:</span> <span className="font-mono text-xs">{harness.binary_hash?.slice(0, 16) || '-'}</span></div>
          <div><span className="text-gray-500">릴리스 채널:</span> {harness.build_channel || 'stable'}</div>
          <div><span className="text-gray-500">정책 프로필:</span> {harness.policy_profile || '-'}</div>
          <div><span className="text-gray-500">라이선스:</span> {harness.license_state || '-'}</div>
          <div><span className="text-gray-500">등록 모드:</span> {harness.enrollment_mode || 'sso'}</div>
          <div><span className="text-gray-500">등록일:</span> {harness.enrolled_at?.slice(0, 10)}</div>
          <div><span className="text-gray-500">마지막 하트비트:</span> {harness.last_heartbeat?.slice(0, 19) || '-'}</div>
          <div><span className="text-gray-500">마지막 증명:</span> {harness.last_attestation?.slice(0, 19) || '-'}</div>
          <div className="col-span-2"><span className="text-gray-500">허용된 사용자:</span> {allowedUsers.length > 0 ? allowedUsers.map(uid => <Link key={uid} to={`/users/${uid}`} className="text-blue-600 hover:underline mr-2">{uid.slice(0, 8)}</Link>) : '-'}</div>
        </div>
      )}

      {tab === 'sessions' && (
        <div className="card">
          {sessions.length === 0 ? (
            <p className="text-gray-400 text-center py-8">세션 이력이 없습니다</p>
          ) : (
            <table className="w-full">
              <thead><tr className="border-b text-left text-xs text-gray-500 uppercase">
                <th className="pb-3">제목</th><th className="pb-3">상태</th><th className="pb-3">시작일</th>
              </tr></thead>
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

      {tab === 'security' && (
        <div className="card space-y-3 text-sm">
          <div><span className="text-gray-500">위험 상태:</span> <span className={harness.risk_state === 'high' ? 'badge-red' : 'badge-green'}>{harness.risk_state}</span></div>
          <div><span className="text-gray-500">상태:</span> <span className={statusBadge(harness.status)}>{statusLabel(harness.status)}</span></div>
          {harness.revocation_reason && <div><span className="text-gray-500">폐기 사유:</span> {harness.revocation_reason}</div>}
          <div><span className="text-gray-500">공개키:</span> <span className="font-mono text-xs break-all">{harness.public_key?.slice(0, 40)}...</span></div>
          <div className="flex gap-2 pt-4">
            <Link to="/fleet" className="text-sm text-blue-600 hover:underline">플릿에서 관리 →</Link>
            <Link to="/audit" className="text-sm text-blue-600 hover:underline">감사 로그 →</Link>
          </div>
        </div>
      )}

      {tab === 'audit' && (
        <div className="card">
          {auditEvents.length === 0 ? (
            <p className="text-gray-400 text-center py-8">감사 이력이 없습니다</p>
          ) : (
            <table className="w-full">
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
