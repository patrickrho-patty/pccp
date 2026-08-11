import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Policy() {
  const [epochs, setEpochs] = useState<any[]>([])
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ allowed_models: 'pmp_kocoder_v1', transition_mode: 'immediate' })

  useEffect(() => { api.listEpochs().then(setEpochs) }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    await api.createEpoch({ ...form, allowed_models: form.allowed_models.split(',').map(s => s.trim()) })
    setShowForm(false)
    api.listEpochs().then(setEpochs)
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">정책 <span className="text-gray-400 text-lg font-normal">Policy</span></h1>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          {showForm ? '취소' : '+ 에포크 생성'}
        </button>
      </div>
      {showForm && (
        <form onSubmit={handleCreate} className="card mb-6 space-y-4">
          <div>
            <label className="label">허용 모델 (쉼표 구분)</label>
            <input className="input" value={form.allowed_models} onChange={(e) => setForm({ ...form, allowed_models: e.target.value })} />
          </div>
          <div>
            <label className="label">전환 모드 (Transition Mode)</label>
            <select className="input" value={form.transition_mode} onChange={(e) => setForm({ ...form, transition_mode: e.target.value })}>
              <option value="immediate">Immediate</option>
              <option value="finish_then_renew">Finish then renew</option>
              <option value="allow_until_expiry">Allow until expiry</option>
            </select>
          </div>
          <button type="submit" className="btn-primary">생성</button>
        </form>
      )}
      <div className="card">
        {epochs.length === 0 ? (
          <p className="text-gray-400 text-center py-8">정책 에포크가 없습니다</p>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">에포크</th>
                <th className="pb-3">번호</th>
                <th className="pb-3">허용 모델</th>
                <th className="pb-3">전환 모드</th>
                <th className="pb-3">상태</th>
              </tr>
            </thead>
            <tbody>
              {epochs.map((ep) => (
                <tr key={ep.id} className="border-b border-gray-100 last:border-0">
                  <td className="py-3 font-mono text-xs">{ep.epoch_id?.slice(0, 25)}</td>
                  <td className="py-3">#{ep.epoch_number}</td>
                  <td className="py-3 text-sm">{ep.allowed_models}</td>
                  <td className="py-3"><span className="badge-gray">{ep.transition_mode}</span></td>
                  <td className="py-3"><span className={ep.status === 'active' ? 'badge-green' : 'badge-gray'}>{ep.status}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
