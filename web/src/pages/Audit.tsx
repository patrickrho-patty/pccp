import { useState, useEffect, Fragment } from 'react'
import { Link } from 'react-router-dom'
import { FilterBar, useFilteredData, Pagination, FilterConfig } from '../components/FilterBar'
import EmptyState from '../components/EmptyState'
import { formatRelative } from '../utils/format'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['action', 'event_type', 'resource_type', 'resource_id', 'details', 'actor_id'],
  searchPlaceholder: '이벤트, 행위자, 리소스, 상세내용으로 검색 · Search events...',
  dateField: 'occurred_at',
  dropdowns: [
    {
      key: 'event_type',
      label: '유형',
      options: [
        { value: 'harness', label: '하네스 · Harness' },
        { value: 'user', label: '사용자 · User' },
        { value: 'session', label: '세션 · Session' },
        { value: 'model', label: '모델 · Model' },
        { value: 'policy', label: '정책 · Policy' },
        { value: 'security', label: '보안 · Security' },
        { value: 'compliance', label: '컴플라이언스 · Compliance' },
        { value: 'cp.', label: '시스템 · System' },
      ],
    },
    {
      key: 'result',
      label: '결과',
      options: [
        { value: 'success', label: '성공 · Success' },
        { value: 'denied', label: '거부 · Denied' },
        { value: 'failure', label: '실패 · Failed' },
      ],
    },
    {
      key: 'actor_type',
      label: '행위자',
      options: [
        { value: 'admin', label: '관리자 · Admin' },
        { value: 'system', label: '시스템 · System' },
        { value: 'user', label: '사용자 · User' },
      ],
    },
  ],
}

export default function Audit() {
  const [events, setEvents] = useState<any[]>([])
  const [filters, setFilters] = useState({
    search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string>,
  })
  const [page, setPage] = useState(1)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const pageSize = 50

  const [chainReport, setChainReport] = useState<{verified: boolean; events: number; reason?: string; first_break_id?: string} | null>(null)
  const [verifying, setVerifying] = useState(false)

  const verifyChain = () => {
    setVerifying(true)
    fetch('/api/audit/verify', { headers: authHeaders() })
      .then(r => r.json())
      .then(setChainReport)
      .catch(() => setChainReport({ verified: false, events: 0, reason: '확인 실패' }))
      .finally(() => setVerifying(false))
  }

  useEffect(() => {
    fetch('/api/audit', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => setEvents(Array.isArray(data) ? data : []))
      .catch(() => setEvents([]))
  }, [])

  const filtered = useFilteredData(events, filters, FILTER_CONFIG)
  const paged = filtered.slice((page - 1) * pageSize, page * pageSize)

  const resultBadge = (r: string) => r === 'success' ? 'badge-green' : r === 'denied' || r === 'failure' ? 'badge-red' : 'badge-yellow'

  // Stats from filtered set
  const stats = {
    total: events.length,
    success: events.filter(e => e.result === 'success').length,
    denied: events.filter(e => e.result === 'denied' || e.result === 'failure').length,
    filtered: filtered.length,
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">감사 로그 <span className="text-gray-400 text-lg font-normal">Audit Trail</span></h1>

      {/* Chain verification (tamper evidence) */}
      <div className="card p-4 mb-4 flex items-center justify-between">
        <div>
          <div className="text-sm font-medium">해시 체인 검증 · Hash-Chain Verification</div>
          <div className="text-xs text-gray-500 mt-0.5">
            {chainReport === null
              ? '감사 이벤트의 무결성을 해시 체인으로 검증합니다'
              : chainReport.verified
                ? `체인 무결 · ${chainReport.events}개 이벤트 검증 완료 — 위변조 없음`
                : `체인 손상 감지 — ${chainReport.reason ?? '알 수 없는 이유'}${chainReport.first_break_id ? ` (이벤트 ${chainReport.first_break_id.slice(0, 8)}…)` : ''}`}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {chainReport && (
            <span className={chainReport.verified ? 'badge-green' : 'badge-red'}>
              {chainReport.verified ? '무결' : '손상'}
            </span>
          )}
          <button onClick={verifyChain} disabled={verifying}
            className="btn btn-secondary text-xs">{verifying ? '검증 중…' : '체인 검증'}</button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-3 mb-4">
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold cursor-pointer hover:text-blue-600" onClick={() => setFilters({ ...filters, dropdowns: {} })}>{stats.total}</div>
          <div className="text-xs text-gray-500">전체 이벤트 · Total</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-green-600 cursor-pointer hover:text-green-700" onClick={() => setFilters({ ...filters, dropdowns: { result: 'success' } })}>{stats.success}</div>
          <div className="text-xs text-gray-500">성공 · Success</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-red-600 cursor-pointer hover:text-red-700" onClick={() => setFilters({ ...filters, dropdowns: { result: 'denied' } })}>{stats.denied}</div>
          <div className="text-xs text-gray-500">거부/실패 · Denied</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-blue-600">{stats.filtered}</div>
          <div className="text-xs text-gray-500">필터 결과 · Filtered</div>
        </div>
      </div>

      {/* Filters */}
      {/* Quick time presets */}
      <div className="flex gap-1 mb-3">
        {[
          { label: '오늘', labelEn: 'Today', days: 0, fromToday: true },
          { label: '어제', labelEn: 'Yesterday', days: 1, fromToday: false },
          { label: '최근 7일', labelEn: '7 days', days: 7, fromToday: true },
          { label: '최근 30일', labelEn: '30 days', days: 30, fromToday: true },
        ].map(preset => (
          <button
            key={preset.label}
            onClick={() => {
              const now = new Date()
              const from = new Date()
              if (preset.days === 0 && preset.fromToday) {
                from.setHours(0, 0, 0, 0)
              } else if (preset.days === 1 && !preset.fromToday) {
                from.setDate(now.getDate() - 1)
                from.setHours(0, 0, 0, 0)
                const to = new Date()
                to.setHours(0, 0, 0, 0)
                setFilters({ ...filters, dateFrom: from.toISOString().slice(0, 10), dateTo: to.toISOString().slice(0, 10) })
                return
              } else {
                from.setDate(now.getDate() - preset.days)
              }
              setFilters({ ...filters, dateFrom: from.toISOString().slice(0, 10), dateTo: now.toISOString().slice(0, 10) })
            }}
            className="btn-sm btn-secondary"
          >
            {preset.label}
          </button>
        ))}
        <button
          onClick={() => setFilters({ ...filters, dateFrom: '', dateTo: '' })}
          className="btn-sm btn-secondary"
        >
          전체
        </button>
      </div>

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      {/* Table */}
      <div className="card">
        {paged.length === 0 ? (
          <div className="text-center py-8">
            <EmptyState icon="☰" title="표시할 감사 이벤트가 없습니다" message="관리자 활동이 기록되면 표시됩니다" />
          </div>
        ) : (
          <table className="w-full overflow-x-auto block">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-2">시간 · Time</th>
                <th className="pb-2">이벤트 · Event</th>
                <th className="pb-2">행위자 · Actor</th>
                <th className="pb-2">리소스 · Resource</th>
                <th className="pb-2">상세 · Details</th>
                <th className="pb-2">결과 · Result</th>
              </tr>
            </thead>
            <tbody>
              {paged.map(e => (
                <Fragment key={e.id}>
                <tr className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30 cursor-pointer" onClick={() => setExpandedId(expandedId === e.id ? null : e.id)}>
                  <td className="py-2 text-xs text-gray-400 whitespace-nowrap" title={e.occurred_at?.slice(0, 19)}>{formatRelative(e.occurred_at)}</td>
                  <td className="py-2 text-sm font-medium font-mono">{e.event_type}</td>
                  <td className="py-2 text-sm">{e.actor_type}{e.actor_id ? ` (${e.actor_id.slice(0, 8)})` : ''}</td>
                  <td className="py-2 text-sm">{e.resource_type}{e.resource_id ? ': ' : ''}{e.resource_id && (
                    e.resource_type === 'user' ? <Link to={`/users/${e.resource_id}`} className="text-blue-600 hover:underline" onClick={ev => ev.stopPropagation()}>{e.resource_id.slice(0, 12)}</Link> :
                    e.resource_type === 'harness' ? <Link to={`/harnesses/${e.resource_id}`} className="text-blue-600 hover:underline" onClick={ev => ev.stopPropagation()}>{e.resource_id.slice(0, 12)}</Link> :
                    e.resource_type === 'project' ? <Link to={`/projects/${e.resource_id}`} className="text-blue-600 hover:underline" onClick={ev => ev.stopPropagation()}>{e.resource_id.slice(0, 12)}</Link> :
                    e.resource_id.slice(0, 12)
                  )}</td>
                  <td className="py-2 text-xs text-gray-500 max-w-xs truncate">{e.details?.slice(0, 80)}</td>
                  <td className="py-2"><span className={resultBadge(e.result)}>{e.result}</span></td>
                </tr>
                {expandedId === e.id && (
                  <tr className="bg-gray-50"><td colSpan={6} className="p-4">
                    <pre className="text-xs font-mono whitespace-pre-wrap break-all text-gray-600">{e.details || '(no details)'}</pre>
                  </td></tr>
                )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <Pagination
        total={filtered.length}
        page={page}
        pageSize={pageSize}
        onPageChange={setPage}
      />
    </div>
  )
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
