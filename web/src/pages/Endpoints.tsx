import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Endpoints() {
  const [endpoints, setEndpoints] = useState<any[]>([])

  useEffect(() => { api.listEndpoints().then(data => setEndpoints(Array.isArray(data) ? data : data || [])) }, [])

  const handleLease = async (id: string) => {
    await api.issueEndpointLease(id)
    api.listEndpoints().then(data => setEndpoints(Array.isArray(data) ? data : data || []))
  }

  const statusBadge = (s: string) => {
    const map: Record<string, string> = { active: 'badge-green', enrolled: 'badge-blue', pending: 'badge-yellow', revoked: 'badge-red', quarantined: 'badge-red' }
    return map[s] || 'badge-gray'
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">추론 엔드포인트 <span className="text-gray-400 text-lg font-normal">Inference Endpoints</span></h1>
      <div className="card">
        {endpoints.length === 0 ? (
          <div className="text-center py-8">
            <p className="text-gray-400 mb-4">등록된 엔드포인트가 없습니다</p>
            <p className="text-sm text-gray-400">PIA를 실행하고 등록하세요: <code className="bg-gray-100 px-2 py-1 rounded">./bin/pccp-pia --enroll --org &lt;org_id&gt; --model &lt;model_pkg&gt;</code></p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">엔드포인트 ID</th>
                <th className="pb-3">PIA Peer</th>
                <th className="pb-3">엔진</th>
                <th className="pb-3">보증</th>
                <th className="pb-3">상태</th>
                <th className="pb-3"></th>
              </tr>
            </thead>
            <tbody>
              {endpoints.map((e) => (
                <tr key={e.id} className="border-b border-gray-100 last:border-0">
                  <td className="py-3 font-mono text-xs">{e.endpoint_id?.slice(0, 25)}</td>
                  <td className="py-3 font-mono text-xs">{e.pia_peer_id}</td>
                  <td className="py-3 text-sm">{e.serving_engine} {e.serving_engine_version}</td>
                  <td className="py-3"><span className="badge-blue">{e.assurance_level}</span></td>
                  <td className="py-3"><span className={statusBadge(e.status)}>{e.status}</span></td>
                  <td className="py-3">
                    <button onClick={() => handleLease(e.id)} className="text-patty-600 text-sm hover:underline">리스 발급</button>
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
