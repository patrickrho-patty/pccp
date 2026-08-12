import { useState, useEffect } from 'react'

export default function Sandboxes() {
  const [sandboxes, setSandboxes] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ runtime_mode: 'docker', image: '', session_id: '', cpu_limit: '4', memory_limit_mb: '8192', network_policy: 'restricted' })

  const load = () => {
    fetch('/api/sandboxes', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => setSandboxes(Array.isArray(data) ? data : []))
      .catch(() => setSandboxes([]))
  }

  useEffect(() => { load() }, [])

  const create = async () => {
    try {
      await fetch('/api/sandboxes', {
        method: 'POST',
        headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify(form)
      })
      setShowForm(false)
      load()
    } catch {}
  }

  const destroy = async (id: string) => {
    if (!confirm('샌드박스를 파기하시겠습니까?')) return
    try {
      await fetch(`/api/sandboxes/${id}/destroy`, { method: 'POST', headers: authHeaders() })
      load()
    } catch {}
  }

  const snapshot = async (id: string) => {
    try {
      const res = await fetch(`/api/sandboxes/${id}/snapshot`, { method: 'POST', headers: authHeaders() })
      if (res.ok) alert('스냅샷 생성됨')
    } catch {}
  }

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { running: 'badge-green', stopped: 'badge-gray', isolated: 'badge-red', error: 'badge-red' }
    return <span className={map[s] || 'badge-gray'}>{s}</span>
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">샌드박스 <span className="text-gray-400 text-lg font-normal">Sandbox & Runtime Control</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary text-sm">+ 새 샌드박스</button>
      </div>

      {showForm && (
        <div className="card mb-6">
          <h3 className="text-sm font-semibold mb-4">샌드박스 생성</h3>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="label">런타임 모드</label>
              <select className="input" value={form.runtime_mode} onChange={e => setForm({ ...form, runtime_mode: e.target.value })}>
                <option value="docker">Docker</option>
                <option value="firecracker">Firecracker (microVM)</option>
                <option value="gvisor">gVisor</option>
                <option value="kata">Kata Containers</option>
                <option value="none">None (Local)</option>
              </select>
            </div>
            <div>
              <label className="label">이미지</label>
              <input className="input" value={form.image} onChange={e => setForm({ ...form, image: e.target.value })} placeholder="ubuntu:22.04" />
            </div>
            <div>
              <label className="label">CPU 제한</label>
              <input className="input" type="number" value={form.cpu_limit} onChange={e => setForm({ ...form, cpu_limit: e.target.value })} />
            </div>
            <div>
              <label className="label">메모리 (MB)</label>
              <input className="input" type="number" value={form.memory_limit_mb} onChange={e => setForm({ ...form, memory_limit_mb: e.target.value })} />
            </div>
            <div>
              <label className="label">네트워크 정책</label>
              <select className="input" value={form.network_policy} onChange={e => setForm({ ...form, network_policy: e.target.value })}>
                <option value="restricted">제한 (Restricted)</option>
                <option value="egress-only">송신만 (Egress Only)</option>
                <option value="full">전체 (Full)</option>
                <option value="airgap">에어갭 (Air-gap)</option>
              </select>
            </div>
            <div>
              <label className="label">세션 ID (선택)</label>
              <input className="input" value={form.session_id} onChange={e => setForm({ ...form, session_id: e.target.value })} placeholder="세션 연결 (선택)" />
            </div>
          </div>
          <div className="flex gap-2 mt-4">
            <button onClick={create} className="btn-primary text-sm">생성</button>
            <button onClick={() => setShowForm(false)} className="btn-secondary text-sm">취소</button>
          </div>
        </div>
      )}

      <div className="card">
        {sandboxes.length === 0 ? (
          <div className="text-center py-12">
            <div className="text-4xl mb-2">📦</div>
            <p className="text-gray-400">등록된 샌드박스가 없습니다</p>
            <p className="text-xs text-gray-400 mt-1">샌드박스를 생성하여 런타임 환경을 관리하세요</p>
          </div>
        ) : (
          <div className="grid grid-cols-3 gap-4">
            {sandboxes.map((s: any) => (
              <div key={s.id} className="border border-gray-200 rounded-lg p-4 hover:shadow-sm transition-shadow">
                <div className="flex items-center justify-between mb-2">
                  <span className="font-mono text-xs text-gray-500">{s.id?.slice(0, 16)}</span>
                  {statusBadge(s.status)}
                </div>
                <div className="text-sm font-medium mb-1">{s.runtime_mode || s.image || 'Sandbox'}</div>
                <div className="text-xs text-gray-500 space-y-0.5">
                  {s.image && <div>이미지: {s.image}</div>}
                  <div>CPU: {s.cpu_limit || '-'} · 메모리: {s.memory_limit_mb || '-'}MB</div>
                  <div>네트워크: <span className="font-medium">{s.network_policy || '-'}</span></div>
                  {s.session_id && <div>세션: {s.session_id?.slice(0, 16)}</div>}
                  {s.created_at && <div>생성: {s.created_at?.slice(0, 19)}</div>}
                </div>
                <div className="flex gap-1 mt-3">
                  <button onClick={() => snapshot(s.id)} className="btn-sm btn-secondary">스냅샷</button>
                  <button onClick={() => destroy(s.id)} className="btn-sm btn-danger">파기</button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
