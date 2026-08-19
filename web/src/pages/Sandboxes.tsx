import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { MODE_KO, NETWORK_KO, SANDBOX_STATUS_META, sandboxActions, sandboxStatusMeta } from '../sandboxLifecycle'

// Sandboxes page (web/15 plan): governed isolated runtime control.
// Provisioning is REAL per-mode (docker/microvm/local/remote) with an
// honest status: "running" only when the runtime accepted the
// container; otherwise "defined" (definition persisted, not running).
// Row actions come from the shared lifecycle state machine
// (sandboxLifecycle.ts, PAT-1513) so only state-valid actions appear.

export default function Sandboxes() {
  const { favorites, sortPinnedFirst } = useFavorites('sandboxes')
  const [sandboxes, setSandboxes] = useState<any[]>([])
  const [allowlist, setAllowlist] = useState<{ images: string[]; enforced: boolean }>({ images: [], enforced: false })
  const [formOpen, setFormOpen] = useState(false)
  const [allowlistOpen, setAllowlistOpen] = useState(false)
  const [allowlistText, setAllowlistText] = useState('')
  const [form, setForm] = useState({
    mode: 'container', base_image: 'patty/sandbox-base:latest', cpu_limit: '1', memory_limit_mb: 1024,
    network_policy: 'none', session_id: '',
  })
  const [filterMode, setFilterMode] = useState('')
  const [filterStatus, setFilterStatus] = useState('')
  const [destroyTarget, setDestroyTarget] = useState<any>(null)

  const load = () => {
    api.listSandboxes().then((d: any[]) => setSandboxes(Array.isArray(d) ? d : [])).catch(() => {})
    api.getSandboxImageAllowlist().then(setAllowlist).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const create = async () => {
    if (!form.base_image.trim()) {
      showToast('이미지가 필요합니다', 'error')
      return
    }
    try {
      const sb = await api.createSandbox({
        mode: form.mode, base_image: form.base_image, cpu_limit: form.cpu_limit,
        memory_limit_mb: form.memory_limit_mb, network_policy: form.network_policy,
        session_id: form.session_id || undefined,
      })
      showToast(sb.status === 'running' ? '샌드박스 실행 중' : `정의 저장됨 (${sb.status}) — 런타임 연결 시 실행됩니다`, sb.status === 'running' ? 'success' : 'info')
      setFormOpen(false)
      load()
    } catch (e: any) { showToast(e?.message || '생성 실패', 'error') }
  }

  const destroy = async (sb: any) => {
    try {
      await api.destroySandbox(sb.id)
      showToast('샌드박스 파괴 완료', 'success')
      setDestroyTarget(null)
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const snapshot = async (sb: any) => {
    try {
      const res = await api.snapshotSandbox(sb.id)
      showToast(`포렌식 스냅샷 기록: ${res.snapshot_id || '생성됨'}`, 'success')
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const retry = async (sb: any) => {
    try {
      const res = await api.retrySandbox(sb.id)
      showToast(res.status === 'running' ? '프로비저닝 재시도 — 실행 중' : `재시도 완료 — 상태: ${sandboxStatusMeta(res.status).ko}`, 'info')
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const saveAllowlist = async () => {
    const images = allowlistText.split('\n').map(s => s.trim()).filter(Boolean)
    try {
      await api.setSandboxImageAllowlist(images)
      showToast(images.length ? `이미지 허용 목록 저장 (${images.length}개) — 이제 목록 외 이미지는 거부됩니다` : '허용 목록 해제 — 모든 이미지 허용', 'success')
      setAllowlistOpen(false)
      load()
    } catch (e: any) { showToast(e?.message || '실패', 'error') }
  }

  const filtered = sandboxes.filter(s =>
    (!filterMode || s.mode === filterMode) && (!filterStatus || s.status === filterStatus))
  const sorted = sortPinnedFirst(filtered, s => s.id)

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">샌드박스 · Sandboxes</h2>
          <p className="text-[11px] text-gray-400">
            격리 실행 런타임 (§31). 상태는 정직합니다 — 런타임이 실제로 수락한 경우에만 "실행 중"으로 표시됩니다.
            {allowlist.enforced && <span className="text-amber-600 ml-2">이미지 허용 목록 강제 중 ({allowlist.images.length})</span>}
          </p>
        </div>
        <div className="flex gap-2 shrink-0 flex-wrap">
          <button className="btn-sm btn-secondary" onClick={() => { setAllowlistText(allowlist.images.join('\n')); setAllowlistOpen(true) }}>이미지 허용 목록</button>
          <button className="btn-sm btn-primary" onClick={() => setFormOpen(true)}>+ 새 샌드박스</button>
        </div>
      </div>

      <div className="flex gap-2 flex-wrap">
        <select className="input text-xs w-32" value={filterMode} onChange={e => setFilterMode(e.target.value)}>
          <option value="">전체 모드</option>
          {Object.entries(MODE_KO).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <select className="input text-xs w-36" value={filterStatus} onChange={e => setFilterStatus(e.target.value)}>
          <option value="">전체 상태</option>
          {Object.entries(SANDBOX_STATUS_META).map(([k, v]) => <option key={k} value={k}>{v.ko}</option>)}
        </select>
      </div>

      <div className="space-y-2">
        {sorted.length === 0 && <EmptyState icon="📦" title="샌드박스가 없습니다"
          message="민감한 작업을 격리 런타임에서 실행하세요." action={{ label: '+ 새 샌드박스', onClick: () => setFormOpen(true) }} />}
        {sorted.map((s: any) => (
          <div key={s.id} className="card p-3 flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <FavoriteStar entity="sandboxes" id={s.id} />
              <div className="min-w-0">
                <Link to={`/sandboxes/${s.id}`} className="text-xs font-semibold font-mono truncate text-blue-600 hover:underline block">{s.id?.slice(0, 14)}</Link>
                <div className="text-[10px] text-gray-400">
                  {MODE_KO[s.mode] || s.mode} · {s.base_image} · CPU {s.cpu_limit || '—'} · {s.memory_limit_mb || 0}MB · 네트워크 {NETWORK_KO[s.network_policy] || s.network_policy}
                </div>
                {s.session_id && (
                  <Link to={`/sessions/${s.session_id}`} className="text-[10px] text-blue-600 hover:underline">세션 {s.session_id.slice(0, 12)}</Link>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <span className={`text-[10px] px-2 py-0.5 rounded-full border ${sandboxStatusMeta(s.status).badge}`}>
                {sandboxStatusMeta(s.status).ko}
              </span>
              {/* Only state-valid actions render; the detail page explains
                  why the rest are unavailable (PAT-1513). */}
              {sandboxActions(s).filter(a => a.enabled).map(a => (
                <button key={a.id}
                  className={a.danger ? 'btn-xs-danger' : 'btn-xs-secondary'}
                  onClick={() => a.id === 'destroy' ? setDestroyTarget(s) : a.id === 'snapshot' ? snapshot(s) : retry(s)}>
                  {a.ko}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>

      <Modal open={formOpen} title="새 샌드박스" onClose={() => setFormOpen(false)}
        footer={<ModalFooter onCancel={() => setFormOpen(false)} onConfirm={create} confirmLabel="생성" />}>
        <div className="space-y-2">
          <div>
            <label className="text-[10px] text-gray-500">런타임 모드</label>
            <select className="input text-xs w-full" value={form.mode} onChange={e => setForm({ ...form, mode: e.target.value })}>
              <option value="container">컨테이너 (Docker/containerd)</option>
              <option value="microvm">마이크로VM (Firecracker)</option>
              <option value="local">로컬 프로세스 (격리 없음 — 문서화된 한계)</option>
              <option value="remote">원격 호스트</option>
            </select>
          </div>
          <div>
            <label className="text-[10px] text-gray-500">이미지 {allowlist.enforced ? '(허용 목록에서 선택)' : ''}</label>
            {allowlist.enforced ? (
              <select className="input text-xs w-full" value={form.base_image} onChange={e => setForm({ ...form, base_image: e.target.value })}>
                {allowlist.images.map(img => <option key={img} value={img}>{img}</option>)}
              </select>
            ) : (
              <input className="input text-xs w-full" value={form.base_image} onChange={e => setForm({ ...form, base_image: e.target.value })} />
            )}
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">CPU</label>
              <input className="input text-xs w-full" value={form.cpu_limit} onChange={e => setForm({ ...form, cpu_limit: e.target.value })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-500">메모리 (MB)</label>
              <input className="input text-xs w-full" type="number" value={form.memory_limit_mb} onChange={e => setForm({ ...form, memory_limit_mb: Number(e.target.value) })} />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-500">네트워크 정책</label>
              <select className="input text-xs w-full" value={form.network_policy} onChange={e => setForm({ ...form, network_policy: e.target.value })}>
                <option value="none">차단 (--network none)</option>
                <option value="restricted">제한</option>
                <option value="host">호스트</option>
              </select>
            </div>
            <div>
              <label className="text-[10px] text-gray-500">세션 바인딩 (선택)</label>
              <input className="input text-xs w-full" placeholder="세션 ID" value={form.session_id} onChange={e => setForm({ ...form, session_id: e.target.value })} />
            </div>
          </div>
        </div>
      </Modal>

      <Modal open={!!destroyTarget} title="샌드박스 파괴"
        onClose={() => setDestroyTarget(null)}
        footer={<ModalFooter onCancel={() => setDestroyTarget(null)} onConfirm={() => destroy(destroyTarget)} confirmLabel="파괴" danger />}>
        <p className="text-[11px] text-gray-500">런타임을 종료하고 정의를 제거합니다 (스냅샷이 있다면 보존). 되돌릴 수 없습니다.</p>
      </Modal>

      <Modal open={allowlistOpen} title="이미지 허용 목록 (§31.1)"
        onClose={() => setAllowlistOpen(false)}
        footer={<ModalFooter onCancel={() => setAllowlistOpen(false)} onConfirm={saveAllowlist} confirmLabel="저장" />}>
        <div className="space-y-2">
          <p className="text-[11px] text-gray-500">줄당 하나의 이미지 ref. 목록이 비어 있지 않으면 목록 외 이미지는 생성 시 거부됩니다 (fail-closed).</p>
          <textarea className="input text-xs w-full font-mono" rows={6} value={allowlistText} onChange={e => setAllowlistText(e.target.value)} />
        </div>
      </Modal>
    </div>
  )
}
