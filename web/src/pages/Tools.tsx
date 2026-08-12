import { useState, useEffect } from 'react'
import { api } from '../api'
import { FilterBar, useFilteredData, FilterConfig } from '../components/FilterBar'

const FILTER_CONFIG: FilterConfig = {
  searchFields: ['name', 'name_ko', 'tool_class'],
  searchPlaceholder: '도구명으로 검색...',
  dropdowns: [
    { key: 'category', label: '분류', options: [
      { value: 'read', label: 'Read' }, { value: 'write', label: 'Write' },
      { value: 'execute', label: 'Execute' }, { value: 'network', label: 'Network' },
    ]},
    { key: 'danger_level', label: '위험도', options: [
      { value: 'low', label: '낮음' }, { value: 'medium', label: '중간' },
      { value: 'high', label: '높음' }, { value: 'critical', label: '치명적' },
    ]},
  ],
}

export default function Tools() {
  const [tools, setTools] = useState<any[]>([])
  const [filters, setFilters] = useState({ search: '', dateFrom: '', dateTo: '', dropdowns: {} as Record<string, string> })

  const load = () => api.listTools().then(data => setTools(Array.isArray(data) ? data : []))
  useEffect(() => { load() }, [])

  const filtered = useFilteredData(tools, filters, FILTER_CONFIG)

  const seed = async () => { await api.seedTools(); load() }

  const dangerBadge = (d: string) => {
    const m: Record<string,string> = { low: 'badge-green', medium: 'badge-blue', high: 'badge-yellow', critical: 'badge-red' }
    return m[d] || 'badge-gray'
  }
  const dangerLabel = (d: string) => {
    const m: Record<string,string> = { low: '낮음', medium: '중간', high: '높음', critical: '치명적' }
    return m[d] || d
  }

  // Stats
  const stats = {
    total: tools.length,
    requiringApproval: tools.filter(t => t.requires_approval).length,
    highRisk: tools.filter(t => t.danger_level === 'high' || t.danger_level === 'critical').length,
    byCategory: tools.reduce((acc, t) => { acc[t.category] = (acc[t.category] || 0) + 1; return acc }, {} as Record<string, number>),
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">도구 관리 <span className="text-gray-400 text-lg font-normal">Tools & MCP</span></h1>
        <button onClick={seed} className="btn-secondary text-sm">기본 도구 등록</button>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-3 mb-4">
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold">{stats.total}</div>
          <div className="text-xs text-gray-500">전체 도구</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-yellow-600">{stats.requiringApproval}</div>
          <div className="text-xs text-gray-500">승인 필요</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-red-600">{stats.highRisk}</div>
          <div className="text-xs text-gray-500">고위험 도구</div>
        </div>
        <div className="card py-3 text-center">
          <div className="text-2xl font-bold text-blue-600">{Object.keys(stats.byCategory).length}</div>
          <div className="text-xs text-gray-500">분류 수</div>
        </div>
      </div>

      <FilterBar config={FILTER_CONFIG} onChange={setFilters} />

      <div className="card">
        {filtered.length === 0 ? (
          <p className="text-gray-400 text-center py-8">등록된 도구가 없습니다</p>
        ) : (
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3">도구명</th>
                <th className="pb-3">한글명</th>
                <th className="pb-3">분류</th>
                <th className="pb-3">위험도</th>
                <th className="pb-3">승인</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(t => (
                <tr key={t.id} className="border-b border-gray-100 last:border-0 hover:bg-blue-50/30">
                  <td className="py-3 font-mono text-sm">{t.name}</td>
                  <td className="py-3 text-sm">{t.name_ko || '-'}</td>
                  <td className="py-3"><span className="badge-gray">{t.category}</span></td>
                  <td className="py-3"><span className={dangerBadge(t.danger_level)}>{dangerLabel(t.danger_level)}</span></td>
                  <td className="py-3">{t.requires_approval ? <span className="badge-yellow">✓</span> : <span className="text-gray-300">-</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
