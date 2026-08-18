import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { compareInteger, formatUsageStateInteger, formatUsageAmount, formatUsageQuantity, UsageReport, UsageReportData } from '../components/UsageReport'
import { useAuth } from '../hooks/useAuth'
import { usageDimensionHref } from '../usageView'

const RANGES = [
  { value: '7d', label: '7일' },
  { value: '30d', label: '30일' },
  { value: '90d', label: '90일' },
  { value: '365d', label: '1년' },
]
export default function Analytics() {
  const { profile } = useAuth()
  const [range, setRange] = useState('30d')
  const [data, setData] = useState<UsageReportData | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [exportError, setExportError] = useState(false)

  useEffect(() => {
    let active = true
    const controller = new AbortController()
    setLoading(true)
    setLoadError(false)
    setData(null)
    api.usageExtended(range, '', controller.signal).then(result => {
      if (active) setData(result)
    }).catch((error) => {
      if (active && error?.name !== 'AbortError') { setData(null); setLoadError(true) }
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false; controller.abort() }
  }, [range])
  const periodLabel = RANGES.find(item => item.value === range)?.label || range
  const hasLedger = Boolean(data?.record_count)
  const delayedMeters = (data?.meters || []).filter(meter => meter.state === 'delayed').length

  const exportCSV2 = async () => {
    if (!data) return
    setExportError(false)
    try {
      const ticket = await api.usageExportTicket(range, data?.window_start, data?.window_end, data?.snapshot_at)
      const a = document.createElement('a')
      a.href = ticket.download_url
      a.download = `usage-${range}.csv`
      a.click()
    } catch { setExportError(true) }
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
          <button className="btn-sm btn-secondary" disabled={loading || !data || loadError} onClick={exportCSV2}>CSV 내보내기</button>
        </div>
      </div>

      {loadError && <div className="rounded border border-red-200 bg-red-50 p-3 text-xs text-red-700">사용량 원장을 불러오지 못했습니다. 0으로 표시하지 않았습니다.</div>}
      {exportError && <div className="rounded border border-red-200 bg-red-50 p-3 text-xs text-red-700">CSV 내보내기를 시작하지 못했습니다. 권한과 연결 상태를 확인해 주십시오.</div>}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <a href="#usage-ledger"><StatCard label="총 토큰" value={loading ? '불러오는 중' : formatUsageStateInteger(data?.total_tokens, data?.total_tokens_state)} accent="blue" sub={`${periodLabel} · 원장 ${data?.record_count ?? '—'}건`} /></a>
        <a href="#usage-ledger"><StatCard label="입력 토큰" value={loading ? '불러오는 중' : formatUsageStateInteger(data?.input_tokens, data?.input_tokens_state)} accent="green" sub={periodLabel} /></a>
        <a href="#usage-ledger"><StatCard label="출력 토큰" value={loading ? '불러오는 중' : formatUsageStateInteger(data?.output_tokens, data?.output_tokens_state)} accent="purple" sub={periodLabel} /></a>
        <a href="#usage-ledger"><StatCard label={`비용 (${data?.display_currency || '통화 미확인'})`} value={loading ? '불러오는 중' : data?.display_total?.state === 'recorded' || data?.display_total?.state === 'zero' ? formatUsageAmount(data.display_total.amount_micros, data.display_total.currency) : data?.display_total?.state === 'error' ? '집계 오류' : '미수집'} accent="orange" sub={loading ? periodLabel : !hasLedger ? '원장 기록 없음' : data?.display_total?.state === 'unavailable' ? '단가 또는 환율 미설정' : data?.display_total?.state === 'error' ? '원장 확인 필요' : `${periodLabel} · 대사 ${data?.reconciled ? '완료' : '필요'}${delayedMeters ? ` · 지연 ${delayedMeters}` : ''}`} /></a>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">모델별 토큰</h3>
          {Object.keys(data?.by_model || {}).length === 0 && <p className="text-[11px] text-gray-400">기록 없음</p>}
          {Object.entries(data?.by_model || {}).sort((a: any, b: any) => compareInteger(b[1].quantity, a[1].quantity)).map(([model, usage]: any) => {
            const content = <><span className="text-gray-700">{usage.label || model.slice(0, 8)}</span><span className="text-gray-500">{formatUsageQuantity(usage)}</span></>
            const href = usageDimensionHref(profile, 'model', model, Boolean(usage.label))
            return href ? <Link key={model} to={href} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">{content}</Link> : <div key={model} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 px-1">{content}</div>
          })}
        </div>
        <div className="card p-4">
          <h3 className="text-xs font-bold mb-2">사용자별 토큰</h3>
          {Object.keys(data?.by_user || {}).length === 0 && <p className="text-[11px] text-gray-400">기록 없음</p>}
          {Object.entries(data?.by_user || {}).sort((a: any, b: any) => compareInteger(b[1].quantity, a[1].quantity)).map(([user, usage]: any) => {
            const content = <><span className="text-gray-700">{usage.label || user.slice(0, 8)}</span><span className="text-gray-500">{formatUsageQuantity(usage)}</span></>
            const href = usageDimensionHref(profile, 'user', user, Boolean(usage.label))
            return href ? <Link key={user} to={href} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 hover:bg-gray-50 px-1 rounded">{content}</Link> : <div key={user} className="flex items-center justify-between text-[11px] border-b border-gray-50 py-1 px-1">{content}</div>
          })}
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

      {loading
        ? <div className="card p-4 text-[11px] text-gray-400">사용량 정보를 불러오는 중입니다.</div>
        : <UsageReport report={data} loadMore={(cursor, signal) => api.usageExtended(range, cursor, signal)} />}
    </div>
  )
}
