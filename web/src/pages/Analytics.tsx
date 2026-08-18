import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { formatUsageAmount, formatUsageQuantity, UsageReport, UsageReportData } from '../components/UsageReport'

const RANGES = [
  { value: '7d', label: '7일' },
  { value: '30d', label: '30일' },
  { value: '90d', label: '90일' },
  { value: '365d', label: '1년' },
]
export default function Analytics() {
  const [range, setRange] = useState('30d')
  const [data, setData] = useState<UsageReportData | null>(null)
  const [loadError, setLoadError] = useState(false)
  const [users, setUsers] = useState<any[]>([])
  const [models, setModels] = useState<any[]>([])

  useEffect(() => {
	const controller = new AbortController()
    setLoadError(false)
	api.usageExtended(range, '', controller.signal).then(setData).catch((error) => {
	  if (error?.name !== 'AbortError') { setData(null); setLoadError(true) }
	})
	return () => controller.abort()
  }, [range])
  useEffect(() => {
    api.listUsers().then((d: any[]) => setUsers(Array.isArray(d) ? d : [])).catch(() => {})
    api.catalogModels().then((d: any[]) => setModels(Array.isArray(d) ? d : [])).catch(() => {})
  }, [])

	const usersByID = useMemo(() => new Map(users.map(user => [user.id, user])), [users])
	const modelsByID = useMemo(() => {
	  const result = new Map<string, any>()
	  for (const model of models) {
	    if (model.id) result.set(model.id, model)
	    if (model.package_id) result.set(model.package_id, model)
	  }
	  return result
	}, [models])
  const userName = (id: string) => usersByID.get(id)?.name_ko || usersByID.get(id)?.name || id?.slice(0, 8)
  const modelName = (id: string) => modelsByID.get(id)?.name || id?.slice(0, 8)
  const periodLabel = RANGES.find(item => item.value === range)?.label || range
  const hasLedger = Boolean(data?.record_count)
  const delayedMeters = (data?.meters || []).filter(meter => meter.state === 'delayed').length

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
          <p className="text-[11px] text-gray-400">릴레이 계량 원장 기준 사용량입니다. 합계를 선택하면 같은 기간의 원장 상세로 이동합니다.</p>
        </div>
        <div className="flex gap-2 items-center shrink-0">
          <select className="input text-xs" value={range} onChange={e => setRange(e.target.value)}>
            {RANGES.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}
          </select>
          <button className="btn-sm btn-secondary" onClick={exportCSV2}>CSV 내보내기</button>
        </div>
      </div>

      {loadError && <div className="rounded border border-red-200 bg-red-50 p-3 text-xs text-red-700">사용량 원장을 불러오지 못했습니다. 0으로 표시하지 않았습니다.</div>}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <a href="#usage-ledger"><StatCard label="총 토큰" value={hasLedger ? data!.total_tokens.toLocaleString() : '미수집'} accent="blue" sub={`${periodLabel} · 원장 ${data?.record_count ?? '—'}건`} /></a>
        <a href="#usage-ledger"><StatCard label="입력 토큰" value={hasLedger ? data!.input_tokens.toLocaleString() : '미수집'} accent="green" sub={periodLabel} /></a>
        <a href="#usage-ledger"><StatCard label="출력 토큰" value={hasLedger ? data!.output_tokens.toLocaleString() : '미수집'} accent="purple" sub={periodLabel} /></a>
        <a href="#usage-ledger"><StatCard label={`비용 (${data?.display_currency || '통화 미확인'})`} value={data?.display_total?.state === 'recorded' || data?.display_total?.state === 'zero' ? formatUsageAmount(data.display_total.amount_micros, data.display_total.currency) : data?.display_total?.state === 'error' ? '집계 오류' : '미수집'} accent="orange" sub={!hasLedger ? '원장 기록 없음' : data?.display_total?.state === 'unavailable' ? '단가 또는 환율 미설정' : data?.display_total?.state === 'error' ? '원장 확인 필요' : `${periodLabel} · 대사 ${data?.reconciled ? '완료' : '필요'}${delayedMeters ? ` · 지연 ${delayedMeters}` : ''}`} /></a>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">모델별 토큰</h3>
          {Object.keys(data?.by_model || {}).length === 0 && <p className="text-[11px] text-gray-400">기록 없음</p>}
          {Object.entries(data?.by_model || {}).sort((a: any, b: any) => b[1].quantity - a[1].quantity).map(([model, usage]: any) => (
            <Link key={model} to={`/models/${model}`} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">
              <span className="text-gray-700">{modelName(model)}</span>
              <span className="text-gray-500">{formatUsageQuantity(usage)}</span>
            </Link>
          ))}
        </div>
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">사용자별 토큰</h3>
          {Object.keys(data?.by_user || {}).length === 0 && <p className="text-[11px] text-gray-400">기록 없음</p>}
          {Object.entries(data?.by_user || {}).sort((a: any, b: any) => b[1].quantity - a[1].quantity).map(([user, usage]: any) => (
            <Link key={user} to={`/users/${user}?tab=usage`} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">
              <span className="text-gray-700">{userName(user)}</span>
              <span className="text-gray-500">{formatUsageQuantity(usage)}</span>
            </Link>
          ))}
        </div>
      </div>

      <div className="card p-4">
        <h3 className="text-xs font-bold mb-1">단위별 사용량</h3>
        <p className="text-[10px] text-gray-400 mb-2">토큰·시간·바이트·건수는 서로 합산하지 않습니다.</p>
        {data?.by_unit ? (
          <div className="grid grid-cols-2 md:grid-cols-5 gap-2">
            {Object.entries(data.by_unit).map(([unit, usage]: any) => (
              <div key={unit} className="border border-gray-100 rounded p-2">
                <div className="text-[10px] uppercase text-gray-400">{unit}</div>
                <div className="text-sm font-semibold">{formatUsageQuantity(usage)}</div>
                <div className="text-[10px] text-gray-400 mt-1">{usage.state === 'unavailable' ? '계량 기록 없음' : usage.state === 'delayed' ? '계량 지연' : '원장 반영'}</div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-[11px] text-gray-400">사용량 정보를 불러오지 못했습니다.</p>
        )}
      </div>

      <UsageReport report={data} loadMore={(cursor) => api.usageExtended(range, cursor)} />
    </div>
  )
}
