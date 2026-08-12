import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Audit() {
  const [events, setEvents] = useState<any[]>([])
  const [filter, setFilter] = useState('')
  const [eventType, setEventType] = useState('')

  useEffect(() => {
    loadEvents()
  }, [])

  const loadEvents = () => {
    fetch('/api/audit', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => setEvents(Array.isArray(data) ? data : data || []))
      .catch(() => setEvents([]))
  }

  const filtered = events.filter(e => {
    if (eventType && !e.event_type?.includes(eventType)) return false
    if (filter) {
      const text = `${e.action} ${e.event_type} ${e.resource_type} ${e.resource_id || ''} ${e.details || ''}`.toLowerCase()
      if (!text.includes(filter.toLowerCase())) return false
    }
    return true
  })

  const resultBadge = (r: string) => r === 'success' ? 'badge-green' : r === 'denied' || r === 'failure' ? 'badge-red' : 'badge-yellow'

  const eventTypes = [...new Set(events.map(e => e.event_type?.split('.')[0]))].filter(Boolean)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">감사 로그 <span className="text-gray-400 text-lg font-normal">Audit Trail</span></h1>

      {/* Filter Bar */}
      <div className="flex gap-3 mb-4">
        <input
          className="input flex-1"
          placeholder="검색 · Search events..."
          value={filter}
          onChange={e => setFilter(e.target.value)}
        />
        <select className="input max-w-xs" value={eventType} onChange={e => setEventType(e.target.value)}>
          <option value="">전체 유형 · All Types</option>
          {eventTypes.map(t => <option key={t} value={t}>{t}</option>)}
        </select>
        <button className="btn-secondary" onClick={() => { setFilter(''); setEventType('') }}>초기화</button>
        <button className="btn-secondary" onClick={() => {
          const csv = ['timestamp,event_type,actor,action,resource,result']
          filtered.forEach(e => csv.push([e.occurred_at, e.event_type, e.actor_type, e.action, e.resource_type, e.result].join(',')))
          const blob = new Blob([csv.join('\n')], { type: 'text/csv' })
          const url = URL.createObjectURL(blob)
          const a = document.createElement('a'); a.href = url; a.download = 'audit_export.csv'; a.click()
        }}>CSV 내보내기</button>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-3 mb-4">
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold">{events.length}</div>
          <div className="text-xs text-gray-500">전체 이벤트 · Total</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-green-600">{events.filter(e => e.result === 'success').length}</div>
          <div className="text-xs text-gray-500">성공 · Success</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-red-600">{events.filter(e => e.result === 'denied' || e.result === 'failure').length}</div>
          <div className="text-xs text-gray-500">거부/실패 · Denied/Failed</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-blue-600">{filtered.length}</div>
          <div className="text-xs text-gray-500">필터 결과 · Filtered</div>
        </div>
      </div>

      {/* Event Table */}
      <div className="card">
        {filtered.length === 0 ? (
          <div className="text-center py-8">
            <p className="text-gray-400">표시할 감사 이벤트가 없습니다.</p>
          </div>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                <th className="pb-2">시간 · Time</th>
                <th className="pb-2">이벤트 · Event</th>
                <th className="pb-2">행위자 · Actor</th>
                <th className="pb-2">리소스 · Resource</th>
                <th className="pb-2">상세 · Details</th>
                <th className="pb-2">결과 · Result</th>
              </tr>
            </thead>
            <tbody>
              {filtered.slice(0, 100).map((e) => (
                <tr key={e.id} className="border-b border-gray-100 last:border-0 hover:bg-gray-50">
                  <td className="py-2 text-xs text-gray-400 font-mono whitespace-nowrap">{e.occurred_at?.slice(0, 19)}</td>
                  <td className="py-2 text-sm font-medium font-mono">{e.event_type}</td>
                  <td className="py-2 text-sm">{e.actor_type}{e.actor_id ? ` (${e.actor_id.slice(0, 8)})` : ''}</td>
                  <td className="py-2 text-sm">{e.resource_type}{e.resource_id ? `: ${e.resource_id.slice(0, 12)}` : ''}</td>
                  <td className="py-2 text-xs text-gray-500 max-w-xs truncate">{e.details?.slice(0, 80)}</td>
                  <td className="py-2"><span className={resultBadge(e.result)}>{e.result}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {filtered.length > 100 && (
        <p className="text-center text-sm text-gray-400 mt-4">최근 100개 이벤트만 표시됩니다 · Showing latest 100 events</p>
      )}
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
