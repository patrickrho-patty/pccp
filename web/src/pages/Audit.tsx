import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Audit() {
  const [events, setEvents] = useState<any[]>([])

  useEffect(() => { api.listAudit().then(data => setEvents(Array.isArray(data) ? data : [])) }, [])

  const resultBadge = (r: string) => r === 'success' ? 'badge-green' : r === 'denied' ? 'badge-red' : 'badge-yellow'

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">감사 로그 <span className="text-gray-400 text-lg font-normal">Audit Log</span></h1>
      <div className="card">
        {events.length === 0 ? (
          <p className="text-gray-400 text-center py-8">감사 이벤트가 없습니다</p>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">시간</th>
                <th className="pb-3">액션</th>
                <th className="pb-3">행위자</th>
                <th className="pb-3">리소스</th>
                <th className="pb-3">결과</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id} className="border-b border-gray-100 last:border-0">
                  <td className="py-3 text-xs text-gray-400 font-mono">{e.occurred_at}</td>
                  <td className="py-3 text-sm font-medium">{e.action}</td>
                  <td className="py-3 text-sm">{e.actor_type}</td>
                  <td className="py-3 text-sm">{e.resource_type} {e.resource_id?.slice(0, 12)}</td>
                  <td className="py-3"><span className={resultBadge(e.result)}>{e.result}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
