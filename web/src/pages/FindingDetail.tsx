import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { formatRelative } from '../utils/format'

// FindingDetail (00 A7 /{entity}/:id) — deep-linkable security finding
// with correlated session/user/harness, status updates, and suppress
// (accept-risk) workflow.
export default function FindingDetail() {
  const { id } = useParams<{ id: string }>()
  const [detail, setDetail] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [suppress, setSuppress] = useState(false)
  const [suppressReason, setSuppressReason] = useState('')
  const [suppressDays, setSuppressDays] = useState(30)

  const load = () => {
    if (!id) return
    api.securityFindingDetail(id).then(setDetail).catch(() => setDetail(null)).finally(() => setLoading(false))
  }
  useEffect(() => { load() }, [id])

  if (loading) return <div className="text-gray-400 p-8 text-center">로딩 중...</div>
  if (!detail?.finding) return (
    <div>
      <Link to="/security" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 보안 운영 센터</Link>
      <p className="text-gray-400 p-8 text-center">발견을 찾을 수 없습니다</p>
    </div>
  )

  const f = detail.finding
  const sevBadge = (s: string) => s === 'critical' || s === 'high' ? 'badge-red' : s === 'medium' ? 'badge-yellow' : 'badge-blue'
  const statusBadge = (s: string) => s === 'open' ? 'badge-red' : s === 'investigating' ? 'badge-yellow' : s === 'suppressed' ? 'badge-gray' : 'badge-green'

  return (
    <div>
      <Link to="/security" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 보안 운영 센터</Link>
      <div className="card mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold">{f.title_ko || f.title}</h1>
          <p className="text-xs text-gray-400 mt-1 font-mono">{f.finding_type}</p>
        </div>
        <div className="flex gap-2 items-center shrink-0">
          <span className={sevBadge(f.severity)}>{f.severity}</span>
          <span className={statusBadge(f.status)}>{f.status}</span>
        </div>
      </div>

      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">상세 · Detail</h3>
        <p className="text-sm text-gray-600 mb-3">{f.description || f.description_ko || '-'}</p>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div><span className="text-gray-500">발생 시각:</span> {formatRelative(f.occurred_at)}</div>
          <div><span className="text-gray-500">규칙:</span> <span className="font-mono text-xs">{f.rule_id || '-'}</span></div>
          {f.suppressed && (
            <>
              <div><span className="text-gray-500">억제 사유:</span> {f.suppress_reason || '-'}</div>
              <div><span className="text-gray-500">억제 만료:</span> {formatRelative(f.suppress_expiry)}</div>
            </>
          )}
        </div>
      </div>

      {detail.session && (
        <div className="card mb-4">
          <h3 className="text-sm font-semibold mb-3">연결 세션 · Correlated Session</h3>
          <div className="space-y-1 text-sm">
            <div>세션: <Link to={`/sessions/${detail.session.session_id || detail.session.id}`} className="text-blue-600 hover:underline">{detail.session.title || detail.session.session_id}</Link></div>
            {detail.user && <div>사용자: <Link to={`/users/${detail.user.id}`} className="text-blue-600 hover:underline">{detail.user.name_ko || detail.user.name}</Link></div>}
            {detail.harness && <div>하네스: <Link to={`/harnesses/${detail.harness.id}`} className="text-blue-600 hover:underline font-mono text-xs">{detail.harness.harness_id}</Link></div>}
          </div>
        </div>
      )}

      {detail.audit_events?.length > 0 && (
        <div className="card mb-4">
          <h3 className="text-sm font-semibold mb-3">감사 이벤트 · Audit</h3>
          <div className="space-y-1">
            {detail.audit_events.map((a: any) => (
              <div key={a.id} className="text-xs flex justify-between gap-3">
                <span className="text-gray-600">{a.action}</span>
                <span className="text-gray-400 flex-shrink-0">{formatRelative(a.occurred_at)}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex gap-2 flex-wrap">
        {f.status === 'open' && (
          <button className="btn-sm btn-primary" onClick={async () => { await api.updateSecurityFinding(f.id, { status: 'investigating' }); showToast('조사 중으로 변경', 'info'); load() }}>조사 시작</button>
        )}
        {f.status !== 'resolved' && f.status !== 'false_positive' && (
          <>
            <button className="btn-sm btn-secondary" onClick={async () => { await api.updateSecurityFinding(f.id, { status: 'resolved' }); showToast('해결됨', 'success'); load() }}>해결됨</button>
            <button className="btn-sm btn-secondary" onClick={async () => { await api.updateSecurityFinding(f.id, { status: 'false_positive' }); showToast('오탐 처리됨', 'info'); load() }}>오탐 처리</button>
          </>
        )}
        {!f.suppressed && (
          <button className="btn-sm btn-secondary" onClick={() => setSuppress(true)}>억제 · Suppress</button>
        )}
        {f.suppressed && (
          <button className="btn-sm btn-secondary" onClick={async () => { await api.reopenFinding(f.id); showToast('억제 해제됨', 'info'); load() }}>억제 해제 · Reopen</button>
        )}
      </div>

      {suppress && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4 animate-fadeIn" onClick={() => setSuppress(false)}>
          <div className="bg-white rounded-xl shadow-xl max-w-md w-full p-5 animate-scaleIn" onClick={e => e.stopPropagation()}>
            <h3 className="text-sm font-semibold mb-1">억제 · Suppress Finding (Accept Risk)</h3>
            <p className="text-xs text-gray-400 mb-4">오탐 또는 수용된 위험은 사유와 만료일을 기록합니다.</p>
            <label className="label">사유 · Reason</label>
            <input className="input mb-3" value={suppressReason} onChange={e => setSuppressReason(e.target.value)} placeholder="예: 내부 테스트 데이터" />
            <label className="label">기간 · Days</label>
            <select className="input mb-4" value={suppressDays} onChange={e => setSuppressDays(Number(e.target.value))}>
              <option value={7}>7일</option>
              <option value={30}>30일</option>
              <option value={90}>90일</option>
              <option value={365}>1년</option>
            </select>
            <div className="flex gap-2 justify-end">
              <button onClick={() => setSuppress(false)} className="btn-sm btn-secondary">취소</button>
              <button
                onClick={async () => {
                  if (!suppressReason.trim()) { showToast('사유를 입력하세요', 'error'); return }
                  await api.suppressFinding(f.id, { reason: suppressReason, days: suppressDays })
                  showToast('억제됨 · 만료 시 자동 재오픈', 'success')
                  setSuppress(false)
                  load()
                }}
                className="btn-sm btn-primary"
              >
                억제 실행
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
