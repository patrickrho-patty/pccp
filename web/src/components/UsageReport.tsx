import { Link } from 'react-router-dom'

export type MeterState = 'recorded' | 'zero' | 'unavailable' | 'delayed' | 'error'

export interface UsageValue {
  quantity: number
  unit: string
  state?: MeterState
  reason?: string
}

export interface UsageMeter {
  metric_type: string
  unit: string
  quantity: number
  rate_micros_per_unit?: string
  amount_micros: number
  currency?: string
  state: MeterState
  reason?: string
  last_updated?: string
}

export interface UsageLedgerRow {
  id: string
  occurred_at: string
  bucket: string
  unit: string
  quantity: number
  rate_micros_per_unit?: string
  amount_micros: number
  currency?: string
  user_id?: string
  harness_id?: string
  session_id?: string
  model_package_id?: string
  endpoint_id?: string
}

export interface UsageReportData {
  range?: string
  window_start?: string
  window_end?: string
  last_updated?: string
  total_tokens: number
  input_tokens: number
  output_tokens: number
  display_currency?: string
  display_total?: {
    amount_micros: number
    currency: string
    state: MeterState
    reason?: string
    rate?: string
    rate_source?: string
    rate_as_of?: string
  }
  cost_by_currency?: Record<string, { amount_micros: number; currency: string; state: MeterState; reason?: string }>
  by_unit?: Record<string, UsageValue>
  by_metric?: Record<string, UsageMeter>
  by_model?: Record<string, UsageValue>
  by_user?: Record<string, UsageValue>
  by_session?: Record<string, UsageValue>
  meters?: UsageMeter[]
  record_count: number
  reconciled: boolean
  reconciliation_errors?: string[]
  drilldown?: UsageLedgerRow[]
}

const METRIC_LABELS: Record<string, string> = {
  tokens_in: '입력 토큰',
  tokens_out: '출력 토큰',
  cache_read: '캐시 읽기',
  cache_write: '캐시 쓰기',
  media_tokens: '멀티미디어 토큰',
  gpu_seconds: 'GPU 사용 시간',
  storage_bytes: '스토리지',
  tool_call: '도구 호출',
  reservation: '예약 용량',
  flat_fee: '정액 비용',
  refund: '환불·조정',
}

const STATE_META: Record<MeterState, { label: string; className: string }> = {
  recorded: { label: '정상', className: 'bg-green-50 text-green-700 border-green-200' },
  zero: { label: '0', className: 'bg-blue-50 text-blue-700 border-blue-200' },
  unavailable: { label: '미수집', className: 'bg-gray-100 text-gray-600 border-gray-200' },
  delayed: { label: '지연', className: 'bg-amber-50 text-amber-700 border-amber-200' },
  error: { label: '오류', className: 'bg-red-50 text-red-700 border-red-200' },
}

export function usageMetricLabel(metric: string): string {
  return METRIC_LABELS[metric] || metric
}

export function formatUsageQuantity(value?: Partial<UsageValue>): string {
  if (!value?.unit || value.state === 'unavailable') return '미수집'
  const quantity = Number(value.quantity || 0)
  if (value.unit === 'seconds') return `${quantity.toLocaleString()}초`
  if (value.unit === 'bytes') return `${quantity.toLocaleString()} B`
  if (value.unit === 'count') return `${quantity.toLocaleString()}건`
  if (value.unit === 'tokens') return `${quantity.toLocaleString()} 토큰`
  return `${quantity.toLocaleString()} ${value.unit}`
}

export function formatUsageAmount(amountMicros?: number, currency?: string): string {
  if (amountMicros == null || !currency) return '—'
  const value = amountMicros / 1_000_000
  const formatted = value.toLocaleString('ko-KR', { minimumFractionDigits: 0, maximumFractionDigits: 6 })
  return currency === 'KRW' ? `₩${formatted}` : `${currency} ${formatted}`
}

function formatDateTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('ko-KR')
}

function StateBadge({ state, reason }: { state: MeterState; reason?: string }) {
  const meta = STATE_META[state] || STATE_META.error
  return <span title={reason} className={`inline-flex px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.className}`}>{meta.label}</span>
}

export function UsageReport({ report, id = 'usage-ledger', title = '사용량 및 비용 원장' }: {
  report: UsageReportData | null
  id?: string
  title?: string
}) {
  if (!report) {
    return <div className="card p-4 text-[11px] text-gray-400">사용량 정보를 불러오지 못했습니다.</div>
  }
  const meters = report.meters || []
  const ledger = report.drilldown || []

  return (
    <div className="card p-4 space-y-4" id={id}>
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h3 className="text-xs font-bold">{title}</h3>
          <p className="text-[10px] text-gray-400 mt-1">
            계량 기간 {formatDateTime(report.window_start)} – {formatDateTime(report.window_end)} · 최신 계량 {formatDateTime(report.last_updated)}
          </p>
        </div>
        <StateBadge state={report.reconciled ? 'recorded' : 'error'} reason={report.reconciliation_errors?.join('\n')} />
      </div>

      {!report.reconciled && (
        <div className="rounded border border-red-200 bg-red-50 p-3 text-[11px] text-red-700">
          <div className="font-semibold">원장 대사가 완료되지 않았습니다. 이 합계를 청구 또는 한도 판단에 사용하지 마십시오.</div>
          {(report.reconciliation_errors || []).map((error) => <div key={error} className="mt-1">• {error}</div>)}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
        <div className="rounded border border-gray-100 p-3">
          <div className="text-[10px] text-gray-400">입력 / 출력 / 합계</div>
          <div className="text-xs font-semibold mt-1">{report.input_tokens?.toLocaleString() || 0} / {report.output_tokens?.toLocaleString() || 0} / {report.total_tokens?.toLocaleString() || 0} 토큰</div>
        </div>
        <div className="rounded border border-gray-100 p-3">
          <div className="text-[10px] text-gray-400">표시 통화 합계</div>
          <div className="text-xs font-semibold mt-1">{formatUsageAmount(report.display_total?.amount_micros, report.display_total?.currency)}</div>
          {report.display_total?.reason && <div className="text-[10px] text-red-600 mt-1">{report.display_total.reason}</div>}
        </div>
        <div className="rounded border border-gray-100 p-3">
          <div className="text-[10px] text-gray-400">원천 통화 및 환산 근거</div>
          <div className="text-[10px] mt-1 space-y-0.5">
            {Object.values(report.cost_by_currency || {}).map(source => <div key={source.currency}>{source.currency}: {formatUsageAmount(source.amount_micros, source.currency)}</div>)}
            {Object.keys(report.cost_by_currency || {}).length === 0 && <div>비용 기록 없음</div>}
            {report.display_total?.rate && <div>환율 {report.display_total.rate} · {report.display_total.rate_source || '출처 미기록'} · {report.display_total.rate_as_of || '기준일 미기록'}</div>}
          </div>
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-[11px]">
          <thead><tr className="text-left text-gray-400 border-b">
            <th className="py-2">계량 항목</th><th>상태</th><th className="text-right">수량</th><th className="text-right">단가</th><th className="text-right">금액</th><th>최종 계량</th>
          </tr></thead>
          <tbody>
            {meters.map((meter, index) => (
              <tr key={`${meter.metric_type}-${meter.unit}-${meter.currency || ''}-${index}`} className="border-b border-gray-50">
                <td className="py-2 font-medium text-gray-700">{usageMetricLabel(meter.metric_type)}</td>
                <td><StateBadge state={meter.state} reason={meter.reason} /></td>
                <td className="text-right font-mono">{formatUsageQuantity(meter)}</td>
                <td className="text-right font-mono text-gray-500">{meter.rate_micros_per_unit && meter.currency ? `${meter.rate_micros_per_unit} µ${meter.currency}/${meter.unit}` : '—'}</td>
                <td className="text-right font-mono">{formatUsageAmount(meter.amount_micros, meter.currency)}</td>
                <td className="text-gray-400">{formatDateTime(meter.last_updated)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <details open>
        <summary className="cursor-pointer text-xs font-bold text-gray-700">원장 상세 {ledger.length.toLocaleString()}건</summary>
        <p className="text-[10px] text-gray-400 mt-1 mb-2">각 행은 집계 전 원본 계량 기록입니다. 수량·단가·금액과 관련 사용자 및 세션을 함께 확인할 수 있습니다.</p>
        <div className="overflow-x-auto max-h-96">
          <table className="w-full text-[10px]">
            <thead className="sticky top-0 bg-white"><tr className="text-left text-gray-400 border-b">
              <th className="py-2">발생 시각</th><th>항목</th><th className="text-right">수량</th><th className="text-right">금액</th><th>관련 항목</th><th>원장 ID</th>
            </tr></thead>
            <tbody>
              {ledger.map((row) => (
                <tr key={row.id} className="border-b border-gray-50">
                  <td className="py-2 whitespace-nowrap">{formatDateTime(row.occurred_at)}</td>
                  <td>{usageMetricLabel(row.bucket)}</td>
                  <td className="text-right font-mono">{formatUsageQuantity({ quantity: row.quantity, unit: row.unit })}</td>
                  <td className="text-right font-mono">{formatUsageAmount(row.amount_micros, row.currency)}</td>
                  <td className="space-x-2 whitespace-nowrap">
                    {row.user_id && <Link className="text-blue-600 hover:underline" to={`/users/${row.user_id}?tab=usage`}>사용자</Link>}
                    {row.session_id && <Link className="text-blue-600 hover:underline" to={`/sessions/${row.session_id}`}>세션</Link>}
                    {row.model_package_id && <span title={row.model_package_id}>모델 {row.model_package_id.slice(0, 8)}</span>}
                  </td>
                  <td className="font-mono text-gray-400" title={row.id}>{row.id.slice(0, 12)}</td>
                </tr>
              ))}
              {ledger.length === 0 && <tr><td colSpan={6} className="py-6 text-center text-gray-400">선택한 기간의 원장 기록이 없습니다.</td></tr>}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  )
}
