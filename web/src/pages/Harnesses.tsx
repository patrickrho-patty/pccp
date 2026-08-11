import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Harnesses() {
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ organization_id: '', user_id: '', harness_id: '', public_key_hex: '', binary_version: '1.0.0' })

  useEffect(() => { api.listHarnesses().then(setHarnesses) }, [])

  const handleEnroll = async (e: React.FormEvent) => {
    e.preventDefault()
    await api.enrollHarness(form)
    setShowForm(false)
    api.listHarnesses().then(setHarnesses)
  }

  const handleRevoke = async (id: string) => {
    if (!confirm('이 하네스를 폐기하시겠습니까?')) return
    await api.revokeHarness(id, 'manual revoke')
    api.listHarnesses().then(setHarnesses)
  }

  const statusBadge = (status: string) => {
    const map: Record<string, string> = {
      enrolled: 'badge-green', active: 'badge-green',
      pending: 'badge-yellow', quarantined: 'badge-yellow',
      revoked: 'badge-red',
    }
    return map[status] || 'badge-gray'
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">하네스 <span className="text-gray-400 text-lg font-normal">Harnesses</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          {showForm ? '취소' : '+ 하네스 등록'}
        </button>
      </div>

      {showForm && (
        <form onSubmit={handleEnroll} className="card mb-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">조직 ID (Organization ID)</label>
              <input className="input" value={form.organization_id} onChange={(e) => setForm({ ...form, organization_id: e.target.value })} required />
            </div>
            <div>
              <label className="label">사용자 ID (User ID)</label>
              <input className="input" value={form.user_id} onChange={(e) => setForm({ ...form, user_id: e.target.value })} required />
            </div>
            <div>
              <label className="label">하네스 ID (Harness Peer ID)</label>
              <input className="input" value={form.harness_id} onChange={(e) => setForm({ ...form, harness_id: e.target.value })} placeholder="hrn_xxx" required />
            </div>
            <div>
              <label className="label">공개키 (Ed25519 Hex)</label>
              <input className="input font-mono text-xs" value={form.public_key_hex} onChange={(e) => setForm({ ...form, public_key_hex: e.target.value })} placeholder="a1b2c3..." required />
            </div>
          </div>
          <button type="submit" className="btn-primary">등록</button>
        </form>
      )}

      <div className="card">
        {harnesses.length === 0 ? (
          <p className="text-gray-400 text-center py-8">등록된 하네스가 없습니다</p>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">하네스 ID</th>
                <th className="pb-3">버전</th>
                <th className="pb-3">등록 모드</th>
                <th className="pb-3">상태</th>
                <th className="pb-3"></th>
              </tr>
            </thead>
            <tbody>
              {harnesses.map((h) => (
                <tr key={h.id} className="border-b border-gray-100 last:border-0">
                  <td className="py-3 font-mono text-xs">{h.harness_id}</td>
                  <td className="py-3 text-sm">{h.binary_version}</td>
                  <td className="py-3"><span className="badge-gray">{h.enrollment_mode}</span></td>
                  <td className="py-3"><span className={statusBadge(h.status)}>{h.status}</span></td>
                  <td className="py-3">
                    {h.status !== 'revoked' && (
                      <button onClick={() => handleRevoke(h.id)} className="text-red-600 text-sm hover:underline">폐기</button>
                    )}
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
