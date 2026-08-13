import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import ConfirmDialog from '../components/ConfirmDialog'
import { showToast } from '../components/Toast'

export default function Sandboxes() {
  const [sandboxes, setSandboxes] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [destroyTarget, setDestroyTarget] = useState<string | null>(null)
  const [form, setForm] = useState({ runtime_mode: 'docker', image: 'patty/sandbox-base:latest', session_id: '', cpu_limit: '4', memory_limit_mb: '8192', network_policy: 'restricted' })

  const load = () => {
    fetch('/api/sandboxes', { headers: authHeaders() }).then(r => r.json()).then(data => setSandboxes(Array.isArray(data) ? data : [])).catch(() => setSandboxes([]))
    fetch('/api/sessions', { headers: authHeaders() }).then(r => r.json()).then(data => setSessions(Array.isArray(data) ? data : [])).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const create = async () => {
    try {
      await fetch('/api/sandboxes', {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify(form)
      })
      setShowForm(false)
      load()
    } catch {}
  }

  const destroy = async (id: string) => {
    if (!confirm('이 샌드박스를 파기하시겠습니까? 모든 데이터가 삭제됩니다.')) return
    try { await fetch(`/api/sandboxes/${id}/destroy`, { method: 'POST', headers: authHeaders() }); load() } catch {}
  }

  const snapshot = async (id: string) => {
    try {
      const res = await fetch(`/api/sandboxes/${id}/snapshot`, { method: 'POST', headers: authHeaders() })
      if (res.ok) showToast('포렌식 스냅샷 생성됨 · Forensic snapshot captured')
    } catch {}
  }

  const statusBadge = (s: string) => { const m: Record<string,string> = { running:'badge-green', stopped:'badge-gray', isolated:'badge-red', error:'badge-red' }; return m[s] || 'badge-gray' }
  const statusLabel = (s: string) => { const m: Record<string,string> = { running:'실행 중', stopped:'중지됨', isolated:'격리됨', error:'오류' }; return m[s] || s }

  const modeInfo: Record<string, { icon: string; desc: string; isolation: string }> = {
    docker: { icon: '🐳', desc: 'Docker 컨테이너', isolation: '네임스페이스 격리' },
    firecracker: { icon: '🔥', desc: 'Firecracker microVM', isolation: '하드웨어 수준 격리 (KVM)' },
    gvisor: { icon: '🛡', desc: 'gVisor 샌드박스', isolation: '사용자 공간 커널' },
    kata: { icon: '🏗', desc: 'Kata Containers', isolation: '경량 VM + 컨테이너' },
    none: { icon: '💻', desc: '로컬 실행 (관리형)', isolation: '격리 없음 (정책 기반 통제만)' },
  }

  const policyInfo: Record<string, string> = {
    restricted: '제한 (인바운드/아웃바운드 차단)',
    'egress-only': '송신만 (외부 접속 허용, 수신 차단)',
    full: '전체 (모든 네트워크 허용)',
    airgap: '에어갭 (완전 차단, 로컬만)',
  }

  const getSessionTitle = (sid: string) => sessions.find(s => s.session_id === sid || s.id === sid)

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-2xl font-bold">샌드박스 <span className="text-gray-400 text-lg font-normal">Sandbox & Runtime</span></h1>
          <p className="text-xs text-gray-400 mt-1">격리된 실행 환경 관리 · PRD §31 · AI 도구 실행을 안전한 환경에서 통제</p>
        </div>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary text-sm">+ 샌드박스 생성</button>
      </div>

      {/* Explanation card */}
      <div className="card mb-6">
        <h3 className="text-sm font-semibold mb-2">샌드박스란? · What is a Sandbox?</h3>
        <p className="text-xs text-gray-500 mb-3">
          샌드박스는 AI 하네스가 코드를 실행하는 격리된 환경입니다. 엔터프라이즈/정부 환경에서는 모든 도구 실행이
          중앙에서 관리되는 샌드박스 내에서 이루어져야 합니다 (PRD §31.2).
        </p>
        <div className="grid grid-cols-5 gap-3">
          {Object.entries(modeInfo).map(([mode, info]) => (
            <div key={mode} className="bg-gray-50 rounded p-2 text-center">
              <div className="text-xl mb-1">{info.icon}</div>
              <div className="text-xs font-medium capitalize">{mode}</div>
              <div className="text-[10px] text-gray-400">{info.desc}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Create form */}
      {showForm && (
        <div className="card mb-6">
          <h3 className="text-sm font-semibold mb-4">샌드박스 생성 · Create Sandbox</h3>
          <div className="grid grid-cols-3 gap-4">
            <div><label className="label">런타임 모드 · Runtime Mode</label>
              <select className="input" value={form.runtime_mode} onChange={e => setForm({ ...form, runtime_mode: e.target.value })}>
                {Object.entries(modeInfo).map(([mode, info]) => <option key={mode} value={mode}>{info.icon} {mode} — {info.desc}</option>)}
              </select>
              <p className="text-xs text-blue-600 mt-1">🔒 {modeInfo[form.runtime_mode]?.isolation}</p>
            </div>
            <div><label className="label">베이스 이미지 · Base Image</label>
              <input className="input font-mono text-xs" value={form.image} onChange={e => setForm({ ...form, image: e.target.value })} placeholder="patty/sandbox-base:latest" />
            </div>
            <div><label className="label">세션 연결 · Link to Session</label>
              <select className="input" value={form.session_id} onChange={e => setForm({ ...form, session_id: e.target.value })}>
                <option value="">연결 안함</option>
                {sessions.filter(s => s.status === 'active').map(s => <option key={s.id} value={s.session_id}>{s.title || s.session_id?.slice(0, 20)}</option>)}
              </select>
            </div>
            <div><label className="label">CPU 코어</label><input className="input" type="number" value={form.cpu_limit} onChange={e => setForm({ ...form, cpu_limit: e.target.value })} /></div>
            <div><label className="label">메모리 (MB)</label><input className="input" type="number" value={form.memory_limit_mb} onChange={e => setForm({ ...form, memory_limit_mb: e.target.value })} /></div>
            <div><label className="label">네트워크 정책</label>
              <select className="input" value={form.network_policy} onChange={e => setForm({ ...form, network_policy: e.target.value })}>
                <option value="restricted">🔒 제한 (Restricted)</option>
                <option value="egress-only">📤 송신만 (Egress Only)</option>
                <option value="full">🌐 전체 (Full)</option>
                <option value="airgap">✈️ 에어갭 (Air-gap)</option>
              </select>
              <p className="text-xs text-gray-400 mt-1">{policyInfo[form.network_policy]}</p>
            </div>
          </div>
          <div className="flex gap-2 mt-4">
            <button onClick={create} className="btn-primary text-sm">생성</button>
            <button onClick={() => setShowForm(false)} className="btn-secondary text-sm">취소</button>
          </div>
        </div>
      )}

      {/* Sandbox grid */}
      <div className="card">
        {sandboxes.length === 0 ? (
          <div className="text-center py-12">
            <div className="text-4xl mb-2">📦</div>
            <p className="text-gray-400">등록된 샌드박스가 없습니다</p>
            <p className="text-xs text-gray-400 mt-1">샌드박스를 생성하여 AI 도구 실행 환경을 관리하세요</p>
          </div>
        ) : (
          <div className="grid grid-cols-3 gap-4">
            {sandboxes.map(s => (
              <div key={s.id} className="border border-gray-200 rounded-lg p-4 hover:shadow-sm transition-shadow">
                <div className="flex items-center justify-between mb-2">
                  <span className="font-mono text-xs text-gray-500">{s.id?.slice(0, 12)}</span>
                  <span className={statusBadge(s.status)}>{statusLabel(s.status)}</span>
                </div>
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-lg">{modeInfo[s.runtime_mode]?.icon || '📦'}</span>
                  <div>
                    <div className="text-sm font-medium">{s.runtime_mode || s.image || 'Sandbox'}</div>
                    <div className="text-xs text-gray-400">{modeInfo[s.runtime_mode]?.desc}</div>
                  </div>
                </div>
                <div className="text-xs text-gray-500 space-y-0.5">
                  {s.image && <div>📦 {s.image}</div>}
                  <div>⚙️ CPU: {s.cpu_limit || '-'} · 💾 {s.memory_limit_mb || '-'}MB</div>
                  <div>🌐 <span className="font-medium">{policyInfo[s.network_policy]?.split(' ')[0] || s.network_policy}</span></div>
                  {s.session_id && <div>🔗 세션: {getSessionTitle(s.session_id)?.title || s.session_id?.slice(0, 16)}</div>}
                  {s.created_at && <div>🕐 생성: {s.created_at?.slice(0, 19)}</div>}
                </div>
                <div className="flex gap-1 mt-3">
                  <button onClick={() => snapshot(s.id)} className="btn-sm btn-secondary">📸 스냅샷</button>
                  <button onClick={() => setDestroyTarget(s.id)} className="btn-sm btn-danger">파기</button>
                  {s.session_id && <Link to="/sessions" className="btn-sm btn-secondary ml-auto">세션 →</Link>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={!!destroyTarget}
        title="샌드박스 파기 · Destroy Sandbox"
        message="이 샌드박스를 파기하시겠습니까? 모든 데이터가 삭제됩니다."
        confirmLabel="파기 실행"
        danger
        onConfirm={async () => { if (destroyTarget) { try { await fetch(`/api/sandboxes/${destroyTarget}/destroy`, { method: 'POST', headers: authHeaders() }); load() } catch {} } setDestroyTarget(null) }}
        onCancel={() => setDestroyTarget(null)}
      />
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }
