import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'

const RANGES = [
  { value: '7d', label: '7일' },
  { value: '30d', label: '30일' },
  { value: '90d', label: '90일' },
  { value: '365d', label: '1년' },
]
const METRIC_KO: Record<string, string> = {
  tokens_in: '입력 토큰', tokens_out: '출력 토큰', gpu_seconds: 'GPU 초', storage_bytes: '스토리지 바이트',
}

// PAT-1501: formatUsage renders a usage row with its unit. Cross-unit
// totals are intentionally not implemented — by_unit rows must be
// displayed separately so the UI cannot sum a quantity without its unit.
function formatUsage(usage: { quantity?: number; unit?: string } | undefined): string {
  if (!usage || usage.unit == null) return '—'
  const q = Number(usage.quantity || 0)
  if (usage.unit === 'usd_micro') return `${q.toLocaleString()} µ¢`
  if (usage.unit === 'krw') return `₩ ${q.toLocaleString()}`
  if (usage.unit === 'seconds') return `${q.toLocaleString()} s`
  if (usage.unit === 'bytes') return `${q.toLocaleString()} B`
  if (usage.unit === 'count') return `${q.toLocaleString()} 건`
  return `${q.toLocaleString()} ${usage.unit}`
}

export default function Analytics() {
  const [range, setRange] = useState('30d')
  const [data, setData] = useState<any>(null)
  const [breakdown, setBreakdown] = useState<any>(null)
  const [users, setUsers] = useState<any[]>([])
  const [models, setModels] = useState<any[]>([])

  useEffect(() => {
    api.usageExtended(range).then(setData).catch(() => setData(null))
    api.usageBreakdown(range).then(setBreakdown).catch(() => setBreakdown(null))
  }, [range])
  useEffect(() => {
    api.listUsers().then((d: any[]) => setUsers(Array.isArray(d) ? d : [])).catch(() => {})
    api.catalogModels().then((d: any[]) => setModels(Array.isArray(d) ? d : [])).catch(() => {})
  }, [])

  const totalTokens = (data?.by_metric?.tokens_in || 0) + (data?.by_metric?.tokens_out || 0)
  const userName = (id: string) => users.find(u => u.id === id)?.name_ko || users.find(u => u.id === id)?.name || id?.slice(0, 8)
  const modelName = (id: string) => models.find((m: any) => m.id === id || m.package_id === id)?.name || id?.slice(0, 8)

  const exportCSV2 = async () => {
    const token = localStorage.getItem('pccp_token')
    try {
      const resp = await fetch(`/api/analytics/usage-extended?range=${range}&format=csv`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      if (!resp.ok) throw new Error('export failed')
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `usage-${range}.csv`
      a.click()
      URL.revokeObjectURL(url)
    } catch { /* noop */ }
  }

  return (
    <div className="p-6 space-y-4 page-enter">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <h2 className="text-sm font-bold">분석 · Analytics</h2>
          <p className="text-[11px] text-gray-400">실제 사용량 (릴레이 계량 파이프라인) — 모든 숫자는 클릭하여 원본으로 이동합니다.</p>
        </div>
        <div className="flex gap-2 items-center shrink-0">
          <select className="input text-xs" value={range} onChange={e => setRange(e.target.value)}>
            {RANGES.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}
          </select>
          <button className="btn-sm btn-secondary" onClick={exportCSV2}>CSV 내보내기</button>
        </div>
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard label="총 토큰" value={totalTokens.toLocaleString()} accent="blue" to="/sessions" />
        <StatCard label="입력 토큰" value={(data?.by_metric?.tokens_in || 0).toLocaleString()} accent="green" to="/sessions" />
        <StatCard label="출력 토큰" value={(data?.by_metric?.tokens_out || 0).toLocaleString()} accent="purple" to="/sessions" />
        <StatCard label="누적 비용 (µ¢)" value={(data?.total_cost_micros || 0).toLocaleString()} accent="orange" to="/analytics" />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">모델별 토큰</h3>
          {Object.keys(data?.by_model || {}).length === 0 && <p className="text-[11px] text-gray-400">기록 없음</p>}
          {Object.entries(data?.by_model || {}).sort((a: any, b: any) => b[1] - a[1]).map(([model, qty]: any) => (
            <Link key={model} to={`/model-infra`} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">
              <span className="text-gray-700">{modelName(model)}</span>
              <span className="text-gray-500">{Number(qty).toLocaleString()}</span>
            </Link>
          ))}
        </div>
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">개발자별 토큰</h3>
          {Object.keys(data?.by_user || {}).length === 0 && <p className="text-[11px] text-gray-400">기록 없음</p>}
          {Object.entries(data?.by_user || {}).sort((a: any, b: any) => b[1] - a[1]).map(([user, qty]: any) => (
            <Link key={user} to={`/users/${user}`} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">
              <span className="text-gray-700">{userName(user)}</span>
              <span className="text-gray-500">{Number(qty).toLocaleString()}</span>
            </Link>
          ))}
        </div>
      </div>

      <div className="card p-4">
        <h3 className="text-xs font-bold mb-2">지표별 집계</h3>
        {Object.entries(data?.by_metric || {}).map(([metric, qty]: any) => (
          <div key={metric} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1">
            <span className="text-gray-700">{METRIC_KO[metric] || metric}</span>
            <span className="text-gray-500">{Number(qty).toLocaleString()}</span>
          </div>
        ))}
      </div>

      {/* PAT-1501: by-unit breakdown — one row per unit, no cross-unit sum. */}
      <div className="card p-4">
        <h3 className="text-xs font-bold mb-1">단위별 집계 · By-unit totals</h3>
        <p className="text-[10px] text-gray-400 mb-2">
          서로 다른 단위는 합치지 않습니다. 각 행은 자체 단위만 표시합니다.
        </p>
        {breakdown?.by_unit ? (
          <div className="grid grid-cols-2 md:grid-cols-5 gap-2">
            {Object.entries(breakdown.by_unit).map(([unit, usage]: any) => (
              <div key={unit} className="border border-gray-100 rounded p-2">
                <div className="text-[10px] uppercase text-gray-400">{unit}</div>
                <div className="text-sm font-semibold">{formatUsage(usage)}</div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-[11px] text-gray-400">집계 없음</p>
        )}
        {breakdown?.reconciled === false && (
          <p className="text-[11px] text-red-500 mt-2">⚠ 항목이 원장과 일치하지 않습니다 (unreconciled).</p>
        )}
      </div>
    </div>
  )
}
