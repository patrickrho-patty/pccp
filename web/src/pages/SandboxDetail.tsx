import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api'
import { GovernedActionModal } from '../components/GovernedActionModal'
import { showToast } from '../components/Toast'
import { formatRelative, formatShortTime } from '../utils/format'
import { MODE_KO, NETWORK_KO, sandboxActionsFor, sandboxStatusMeta, sandboxRuntimeConnected } from '../sandboxLifecycle'

// SandboxDetail (PAT-1513) — canonical inspectable sandbox view:
// lifecycle, runtime/provider, owning session/user/harness, image digest,
// network/resource policy, timestamps, snapshot history, audit evidence,
// and lifecycle actions. Actions derive from the server-returned
// valid_actions (internal/sandbox/lifecycle.go is authoritative); the
// client mirror in web/src/sandboxLifecycle.ts supplies only labels and
// disabled reasons.

export default function SandboxDetail() {
  const { id } = useParams<{ id: string }>()
  const [detail, setDetail] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [destroyOpen, setDestroyOpen] = useState(false)
  const [destroyReason, setDestroyReason] = useState('')
  const [busy, setBusy] = useState('')

  const load = () => {
    if (!id) return
    api.getSandboxDetail(id)
      .then(setDetail)
      .catch(() => setDetail(null))
      .finally(() => setLoading(false))
  }
  useEffect(() => { load() }, [id])

  if (loading) return <div className="p-8 space-y-3 animate-pulse"><div className="h-4 bg-gray-100 rounded w-1/2" /><div className="h-4 bg-gray-100 rounded w-2/3" /></div>
  if (!detail?.sandbox) return <div className="text-gray-400 p-8 text-center">샌드박스를 찾을 수 없습니다</div>

  const sb = detail.sandbox
  const meta = sandboxStatusMeta(sb.status)
  // Server-admitted actions are authoritative; the mirror supplies labels
  // and disabled reasons only.
  const actions = sandboxActionsFor(detail.valid_actions || [])
  const connected = sandboxRuntimeConnected(sb)
  const session = detail.session
  const user = detail.user
  const snapshots = detail.snapshots || []
  const auditEvents = detail.audit_events || []

  const run = async (action: string, fn: () => Promise<any>, ok: (res: any) => string) => {
    setBusy(action)
    try {
      const res = await fn()
      showToast(ok(res), 'success')
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
    finally { setBusy('') }
  }

  const doSnapshot = () => run('snapshot', () => api.snapshotSandbox(sb.id),
    res => `포렌식 스냅샷 기록: ${res.snapshot_id || '생성됨'}`)
  const doRetry = () => run('retry', () => api.retrySandbox(sb.id),
    res => res.status === 'running' ? '프로비저닝 재시도 — 실행 중' : `재시도 완료 — 상태: ${sandboxStatusMeta(res.status).ko}`)
  const doDestroy = () => run('destroy', async () => {
    const res = await api.destroySandbox(sb.id)
    setDestroyOpen(false)
    return res
  }, () => '샌드박스 파괴 완료 — 파괴 증거가 감사에 기록됨')

  const clickAction = (actionId: string) => {
    if (actionId === 'snapshot') doSnapshot()
    else if (actionId === 'retry') doRetry()
    else if (actionId === 'destroy') { setDestroyReason(''); setDestroyOpen(true) }
  }

  return (
    <div>
      <Link to="/sandboxes" className="text-sm text-blue-600 hover:underline mb-4 inline-block">← 샌드박스 목록</Link>

      <div className="card mb-6 flex items-start justify-between flex-wrap gap-3">
        <div className="min-w-0">
          <h1 className="text-lg font-bold font-mono break-all">{sb.id}</h1>
          <p className="text-sm text-gray-400">{MODE_KO[sb.mode] || sb.mode} · {sb.base_image}</p>
        </div>
        <div className="flex gap-2 items-center flex-wrap">
          {!connected && sb.status !== 'destroyed' && <span className="badge-yellow">⚠ 런타임 미연결</span>}
          <span className={`text-[10px] px-2 py-0.5 rounded-full border ${meta.badge}`}>{meta.ko}</span>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">런타임 · Runtime</h3>
          <div className="space-y-1 text-sm">
            <div><span className="text-gray-500">모드:</span> {MODE_KO[sb.mode] || sb.mode}</div>
            <div><span className="text-gray-500">프로바이더:</span> <span className="font-mono text-xs">{sb.runtime_provider || '-'}</span></div>
            <div><span className="text-gray-500">생성:</span> {formatRelative(detail.created_at)} <span className="text-xs text-gray-400">{formatShortTime(detail.created_at)}</span></div>
            <div><span className="text-gray-500">시작:</span> {detail.started_at ? formatRelative(detail.started_at) : '-'}</div>
            <div><span className="text-gray-500">파괴:</span> {detail.destroyed_at ? formatRelative(detail.destroyed_at) : '-'}</div>
            {detail.destroy_evidence && <div><span className="text-gray-500">파괴 증거:</span> <span className="font-mono text-xs">{detail.destroy_evidence}</span></div>}
          </div>
        </div>

        <div className="card">
          <h3 className="text-sm font-semibold mb-3">소유 · Ownership</h3>
          <div className="space-y-1 text-sm">
            <div><span className="text-gray-500">세션:</span>{' '}
              {sb.session_id
                ? <Link to={`/sessions/${sb.session_id}`} className="text-blue-600 hover:underline font-mono text-xs">{session?.title || sb.session_id}</Link>
                : <span className="text-gray-400">바인딩 없음</span>}
              {session && <span className="text-xs text-gray-400 ml-1">({session.status})</span>}
            </div>
            <div><span className="text-gray-500">사용자:</span>{' '}
              {user
                ? <Link to={`/users/${user.id}`} className="text-blue-600 hover:underline">{user.name_ko || user.name}</Link>
                : <span className="font-mono text-xs">{sb.user_id || '-'}</span>}
            </div>
            <div><span className="text-gray-500">하네스:</span> <span className="font-mono text-xs">{session?.harness_id || '-'}</span></div>
            <div><span className="text-gray-500">리포지토리:</span> <span className="font-mono text-xs">{sb.repository_id || '-'}</span></div>
          </div>
        </div>

        <div className="card">
          <h3 className="text-sm font-semibold mb-3">이미지 · Image</h3>
          <div className="space-y-1 text-sm">
            <div><span className="text-gray-500">베이스 이미지:</span> <span className="font-mono text-xs break-all">{sb.base_image}</span></div>
            <div><span className="text-gray-500">다이제스트:</span> <span className="font-mono text-xs break-all">{sb.image_digest || <span className="text-gray-400">미고정 (태그 참조)</span>}</span></div>
          </div>
        </div>

        <div className="card">
          <h3 className="text-sm font-semibold mb-3">정책 · 리소스 · Policy</h3>
          <div className="space-y-1 text-sm">
            <div><span className="text-gray-500">네트워크:</span> {NETWORK_KO[sb.network_policy] || sb.network_policy || '-'}</div>
            <div><span className="text-gray-500">CPU:</span> {sb.cpu_limit || '—'}</div>
            <div><span className="text-gray-500">메모리:</span> {sb.memory_limit_mb || 0}MB</div>
            {sb.resource_limits && sb.resource_limits !== 'null' && sb.resource_limits !== '{}' && (
              <div><span className="text-gray-500">추가 제한:</span> <span className="font-mono text-xs break-all">{sb.resource_limits}</span></div>
            )}
          </div>
        </div>
      </div>

      <div className="card mb-4">
        <h3 className="text-sm font-semibold mb-3">조치 · Actions</h3>
        <div className="flex gap-2 flex-wrap">
          {actions.filter(a => a.enabled).map(a => (
            <button key={a.id}
              className={a.danger ? 'btn-sm btn-danger' : 'btn-sm btn-secondary'}
              disabled={busy !== ''}
              onClick={() => clickAction(a.id)}>
              {busy === a.id ? '처리 중…' : a.ko}
            </button>
          ))}
          {actions.every(a => !a.enabled) && <p className="text-xs text-gray-400">이 상태에서 가능한 조치가 없습니다</p>}
        </div>
        {actions.some(a => !a.enabled) && (
          <ul className="mt-2 space-y-0.5">
            {actions.filter(a => !a.enabled).map(a => (
              <li key={a.id} className="text-[11px] text-gray-400">{a.ko} 불가 — {a.reason}</li>
            ))}
          </ul>
        )}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">스냅샷 이력 · Snapshots ({snapshots.length})</h3>
          {snapshots.length === 0 ? <p className="text-xs text-gray-400">스냅샷 없음</p> : (
            <div className="space-y-1">
              {snapshots.map((snap: any, i: number) => (
                <div key={snap.snapshot_id || i} className="text-xs flex justify-between gap-3 py-1 border-b border-gray-50">
                  <span className="font-mono text-gray-700">{snap.snapshot_id || '-'}</span>
                  <span className="text-gray-400 flex-shrink-0">{formatRelative(snap.occurred_at)}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="card">
          <h3 className="text-sm font-semibold mb-3">감사 증거 · Audit ({auditEvents.length})</h3>
          {auditEvents.length === 0 ? <p className="text-xs text-gray-400">감사 이벤트 없음</p> : (
            <div className="space-y-1">
              {auditEvents.map((a: any) => (
                <div key={a.id} className="text-xs flex justify-between gap-3 py-1 border-b border-gray-50">
                  <div className="min-w-0">
                    <span className="font-medium text-gray-700">{a.action}</span>
                    <span className="text-gray-400 ml-2 break-all">{a.details?.slice(0, 100)}</span>
                  </div>
                  <span className="text-gray-400 flex-shrink-0">{formatRelative(a.occurred_at)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      <GovernedActionModal
        open={destroyOpen}
        title="샌드박스 파괴"
        subtitle={sb.id}
        warnings={[{ kind: 'high', text: '런타임을 종료하고 파괴 증거(키 폐기 증명)를 감사에 기록합니다. 되돌릴 수 없습니다.' }]}
        preview={<ul className="text-[11px] text-gray-500 list-disc pl-4 space-y-0.5">
          <li>스냅샷 이력과 감사 증거는 보존됩니다</li>
          {sb.session_id && <li>바인딩된 세션 {sb.session_id.slice(0, 12)}… 의 격리 런타임이 해제됩니다</li>}
          {sb.status === 'defined' && <li>런타임 미연결 상태이므로 정의 레코드만 파괴됩니다</li>}
          <li>동일 작업을 반복해도 증거는 한 번만 기록됩니다 (멱등)</li>
        </ul>}
        reason={destroyReason}
        onReasonChange={setDestroyReason}
        reasonPlaceholder="예: 세션 종료 후 리소스 회수 / 침해 조사 완료"
        confirmLabel="파괴 실행"
        onCancel={() => setDestroyOpen(false)}
        onConfirm={doDestroy}
        danger
      />
    </div>
  )
}
