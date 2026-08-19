import { useState, useEffect } from 'react'
import { useParams, Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { Modal, ModalFooter } from '../components/Modal'
import { showToast } from '../components/Toast'
import { formatRelative } from '../utils/format'
import { useConfirm } from '../components/useConfirm'
import { deriveHarnessHealth, statusLabelKo, riskLabelKo, healthMeta } from '../harnessHealth'

// HarnessDetail (harnesses C1/C5) — deep-linkable harness view with
// device posture, PPC credential metadata (issuer/validity/revocation),
// allowed users, sessions, attestation history, and audit.
export default function HarnessDetail() {
  const { id } = useParams<{ id: string }>()
  const confirm = useConfirm()
  const [detail, setDetail] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = (searchParams.get('tab') as 'overview' | 'sessions' | 'security' | 'audit') || 'overview'
  const setTab = (t: string) => setSearchParams(t === 'overview' ? {} : { tab: t })
  const [revokeOpen, setRevokeOpen] = useState(false)
  const [revokeReason, setRevokeReason] = useState('')

  const load = () => {
    if (!id) return
    api.getHarnessDetail(id)
      .then(setDetail)
      .catch(() => setDetail(null))
      .finally(() => setLoading(false))
  }
  useEffect(() => { load() }, [id])

  if (loading) return <div className="p-8 space-y-3 animate-pulse"><div className="h-4 bg-gray-100 rounded w-1/2" /><div className="h-4 bg-gray-100 rounded w-2/3" /></div>
  if (!detail?.harness) return <div className="text-gray-400 p-8 text-center">하네스를 찾을 수 없습니다</div>

  const h = detail.harness
  const cred = detail.credential
  const device = detail.device
  const sessions = detail.sessions || []
  const allowedUsers = detail.allowed_users || []
  const auditEvents = detail.audit_events || []
  const attEvents = detail.attestation_events || []

  // PAT-1492: one explainable health state derived from canonical dimensions.
  // The header, stat cards, and dimension list all consume the same result so
  // a green indicator can never co-occur with an expired signal (unless it is
  // explicitly a different dimension). Raw risk/lifecycle enums are replaced
  // by governed Korean labels with evidence.
  const health = deriveHarnessHealth({
    status: h.status, risk_state: h.risk_state,
    last_heartbeat: h.last_heartbeat, last_attestation: h.last_attestation,
    binary_version: h.binary_version, stale: detail.stale,
    version_blocked: detail.version_blocked,
  })
  const overallMeta = healthMeta(health.overall)
  const activeSessions = sessions.filter((s: any) => s.status === 'active')

  const handleRevoke = async () => {
    if (!revokeReason.trim()) { showToast('사유를 입력하세요', 'error'); return }
    try {
      const res: any = await api.revokeHarness(h.id, revokeReason)
      showToast(res?.relay_propagated === false ? '폐기됨 (릴레이 채널 미연결)' : '폐기됨 · PPC가 폐기 목록에 등록됨', 'success')
      setRevokeOpen(false); setRevokeReason(''); load()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  return (
    <div>
      <Link to="/harnesses" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 하네스 목록</Link>

      <div className="card mb-6 flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold font-mono">{h.harness_id}</h1>
          <p className="text-sm text-gray-400">v{h.binary_version} · {h.build_channel || 'stable'} · {h.enrollment_mode || 'sso'}</p>
        </div>
        <div className="flex gap-2 items-center flex-wrap">
          <span className={`text-[11px] px-2 py-0.5 rounded-full border ${overallMeta.color}`}>{overallMeta.icon} 전체 {overallMeta.label}</span>
          <span className={`text-[11px] px-2 py-0.5 rounded-full border ${healthMeta(health.dimensions.find(d => d.key === 'lifecycle')!.state).color}`}>
            {statusLabelKo(h.status)}
          </span>
          <span className={`text-[11px] px-2 py-0.5 rounded-full border ${healthMeta(health.dimensions.find(d => d.key === 'risk')!.state).color}`}>
            위험 {riskLabelKo(h.risk_state)}
          </span>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 stat-grid mb-6">
        <StatCard label="활성 세션" value={activeSessions.length} accent="blue" to="/sessions" query={`?harness_id=${encodeURIComponent(h.harness_id)}`} />
        <StatCard label="허용 사용자" value={allowedUsers.length} accent="green" />
        <StatCard label="전체 상태" value={`${overallMeta.icon} ${overallMeta.label}`} accent={health.overall === 'critical' ? 'red' : health.overall === 'warning' ? 'orange' : health.overall === 'attention' ? 'yellow' : 'green'} sub={health.summary} />
        <StatCard label="하트비트" value={health.dimensions.find(d => d.key === 'heartbeat')!.icon} accent={health.dimensions.find(d => d.key === 'heartbeat')!.state === 'warning' ? 'orange' : health.dimensions.find(d => d.key === 'heartbeat')!.state === 'attention' ? 'yellow' : 'green'} sub={h.last_heartbeat?.slice(0, 16).replace('T', ' ')} />
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200" role="tablist">
        {[
          { id: 'overview', label: '개요', en: 'Overview' },
          { id: 'sessions', label: '세션', en: 'Sessions', count: sessions.length },
          { id: 'security', label: '보안', en: 'Security' },
          { id: 'audit', label: '감사', en: 'Audit', count: auditEvents.length },
        ].map(t => (
          <button key={t.id} role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && t.count > 0 && `(${t.count})`}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <>
          <div className="card mb-4">
            <h3 className="text-sm font-semibold mb-3">하네스 정보 · Harness</h3>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div><span className="text-gray-500">하네스 ID:</span> <span className="font-mono text-xs">{h.harness_id}</span></div>
              <div><span className="text-gray-500">바이너리 버전:</span> v{h.binary_version}{detail.version_blocked && <span className="text-red-600 ml-1">(최소 {detail.forced_version?.min_version} 미만 — 차단)</span>}</div>
              <div><span className="text-gray-500">빌드 해시:</span> <span className="font-mono text-xs">{h.binary_hash?.slice(0, 16) || '-'}</span></div>
              <div><span className="text-gray-500">릴리스 채널:</span> {h.build_channel || 'stable'}</div>
              <div><span className="text-gray-500">정책 프로필:</span> {h.policy_profile || '-'}</div>
              <div><span className="text-gray-500">라이선스:</span> {h.license_state || '-'}</div>
              <div><span className="text-gray-500">등록일:</span> {formatRelative(h.enrolled_at)}</div>
              <div><span className="text-gray-500">하트비트:</span> {h.last_heartbeat ? formatRelative(h.last_heartbeat) : '-'}</div>
              <div><span className="text-gray-500">마지막 증명:</span> {h.last_attestation ? formatRelative(h.last_attestation) : '-'}</div>
              <div><span className="text-gray-500">공개키:</span> <span className="font-mono text-xs break-all">{h.public_key?.slice(0, 32)}…</span></div>
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
            <div className="card">
              <h3 className="text-sm font-semibold mb-3">기기 · Device</h3>
              {device ? (
                <div className="space-y-1 text-sm">
                  <div><span className="text-gray-500">호스트:</span> {device.hostname || '-'}</div>
                  <div><span className="text-gray-500">OS:</span> {device.os || '-'} {device.os_version || ''} ({device.arch || '-'})</div>
                  <div><span className="text-gray-500">MDM:</span> {device.mdm_enrolled ? '등록됨' : '미등록'}</div>
                  {device.mdm_posture && <div className="text-xs"><span className="text-gray-500">MDM 상태:</span> <span className="font-mono text-[10px] break-all">{device.mdm_posture.slice(0, 200)}</span></div>}
                  <div><span className="text-gray-500">네트워크 존:</span> {device.network_zone || '-'}</div>
                  <div><span className="text-gray-500">IP:</span> <span className="font-mono text-xs">{device.ip_address || '-'}</span></div>
                  <div><span className="text-gray-500">첫 관측:</span> {formatRelative(device.first_seen)} · 마지막: {formatRelative(device.last_seen)}</div>
                </div>
              ) : <p className="text-xs text-gray-400">기기 정보 없음</p>}
            </div>

            <div className="card">
              <h3 className="text-sm font-semibold mb-3">자격증명 · PPC</h3>
              {cred ? (
                <div className="space-y-1 text-sm">
                  <div><span className="text-gray-500">직렬:</span> <span className="font-mono text-xs">{cred.serial}</span></div>
                  <div><span className="text-gray-500">발급자:</span> {cred.issuer}</div>
                  <div><span className="text-gray-500">대상:</span> <span className="font-mono text-xs">{cred.subject_peer_id}</span></div>
                  <div><span className="text-gray-500">유효 기간:</span> {cred.not_before?.slice(0, 10)} ~ {cred.not_after?.slice(0, 10)}</div>
                  <div><span className="text-gray-500">상태:</span>{' '}
                    {cred.revoked
                      ? <span className="badge-red">폐기됨 ({cred.revoked_reason || 'no reason'})</span>
                      : cred.valid ? <span className="badge-green">유효</span> : <span className="badge-yellow">만료</span>}
                  </div>
                  <div><span className="text-gray-500">빌드 채널:</span> {cred.build_channel || '-'}</div>
                  <div><span className="text-gray-500">폐기 권한:</span> {cred.revocation_authority}</div>
                </div>
              ) : <p className="text-xs text-gray-400">자격증명 정보 없음</p>}
            </div>
          </div>

          <div className="card mb-4">
            <h3 className="text-sm font-semibold mb-3">허용 사용자 · Allowed Users ({allowedUsers.length})</h3>
            {allowedUsers.length === 0 ? <p className="text-xs text-gray-400">허용된 사용자 없음</p> : (
              <div className="space-y-1">
                {allowedUsers.map((u: any) => (
                  <div key={u.id} className="flex items-center gap-3 text-sm p-2 bg-gray-50 rounded">
                    <Link to={`/users/${u.id}`} className="text-blue-600 hover:underline font-medium">{u.name_ko || u.name}</Link>
                    <span className="text-xs text-gray-400">{u.email}</span>
                    <span className="text-xs text-gray-400 ml-auto">{u.title_ko || u.title || ''}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}

      {tab === 'sessions' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">세션 · Sessions ({sessions.length})</h3>
          {sessions.length === 0 ? <p className="text-xs text-gray-400">세션 없음</p> : (
            <div className="space-y-2">
              {sessions.map((s: any) => (
                <div key={s.id} className="flex items-center gap-3 text-sm p-2 bg-gray-50 rounded flex-wrap">
                  <Link to={`/sessions/${s.session_id || s.id}`} className="text-blue-600 hover:underline font-medium">{s.title || '제목 없음'}</Link>
                  <span className={`text-xs ${s.status === 'active' ? 'text-green-600' : s.status === 'terminated' ? 'text-red-600' : 'text-gray-400'}`}>{s.status}</span>
                  {s.model_class && <span className="text-xs text-gray-400">· {s.model_class}</span>}
                  <span className="text-xs text-gray-400 ml-auto">{formatRelative(s.opened_at)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'security' && (
        <div className="space-y-4">
          <div className="card">
            <h3 className="text-sm font-semibold mb-3">보안 상태 · Security</h3>
            <div className="space-y-1 text-sm">
              <div><span className="text-gray-500">위험 상태:</span> {h.risk_state}</div>
              <div><span className="text-gray-500">폐기 사유:</span> {h.revocation_reason || '-'}</div>
              {detail.forced_version && (
                <div><span className="text-gray-500">강제 버전:</span> {detail.forced_version.min_version} (링 {detail.forced_version.release_ring}) — {detail.forced_version.reason || ''}</div>
              )}
            </div>
            <div className="flex gap-2 mt-4">
              {(h.status === 'active' || h.status === 'enrolled') && (
                <button className="btn-sm btn-secondary" onClick={async () => {
                  if (!await confirm({ title: '격리', message: '이 하네스를 격리하시겠습니까? 모든 활성 세션이 종료됩니다.', danger: true })) return
                  await api.quarantineHarness(h.id); showToast('격리됨', 'info'); load()
                }}>격리 · Quarantine</button>
              )}
              {(h.status === 'active' || h.status === 'enrolled') && (
                <button className="btn-sm btn-danger" onClick={() => { setRevokeOpen(true); setRevokeReason('') }}>폐기 · Revoke</button>
              )}
              {h.status === 'quarantined' && (
                <button className="btn-sm btn-primary" onClick={async () => { await api.reactivateHarness(h.id); showToast('재활성화됨', 'success'); load() }}>재활성화 · Reactivate</button>
              )}
            </div>
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-3">증명 이력 · Attestation ({attEvents.length})</h3>
            {attEvents.length === 0 ? <p className="text-xs text-gray-400">증명 이벤트 없음 — 하네스 하트비트가 증명을 보고하면 여기에 쌓입니다</p> : (
              <div className="space-y-1">
                {attEvents.map((a: any) => (
                  <div key={a.id} className="text-xs flex justify-between gap-3">
                    <span className="text-gray-600">{a.action} — {a.details?.slice(0, 80)}</span>
                    <span className="text-gray-400 flex-shrink-0">{formatRelative(a.occurred_at)}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {tab === 'audit' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">감사 이벤트 · Audit ({auditEvents.length})</h3>
          {auditEvents.length === 0 ? <p className="text-xs text-gray-400">감사 이벤트 없음</p> : (
            <div className="space-y-1">
              {auditEvents.map((a: any) => (
                <div key={a.id} className="text-xs flex justify-between gap-3 py-1 border-b border-gray-50">
                  <div>
                    <span className="font-medium text-gray-700">{a.action}</span>
                    <span className="text-gray-400 ml-2">{a.details?.slice(0, 100)}</span>
                  </div>
                  <span className="text-gray-400 flex-shrink-0">{formatRelative(a.occurred_at)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      <Modal open={revokeOpen} title="하네스 폐기 · Revoke" subtitle={h.harness_id} onClose={() => setRevokeOpen(false)} size="sm"
        footer={<ModalFooter onCancel={() => setRevokeOpen(false)} onConfirm={handleRevoke} confirmLabel="폐기 실행" danger disabled={!revokeReason.trim()} />}>
        <div>
          <p className="text-sm text-gray-600 mb-3">PPC가 CA 폐기 목록에 등록되고 모든 활성 세션이 종료됩니다.</p>
          <label className="label">사유 · Reason (필수)</label>
          <textarea className="input" rows={3} value={revokeReason} onChange={e => setRevokeReason(e.target.value)} placeholder="예: 직원 퇴사, 기기 분실" />
        </div>
      </Modal>
    </div>
  )
}
