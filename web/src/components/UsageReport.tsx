import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { previousUsageLedgerPage, usageReasonLabel, usageReconciliationRows } from '../usageView'

export type IntegerValue = string | number

export type MeterState = 'recorded' | 'zero' | 'unavailable' | 'delayed' | 'error'

export interface UsageValue {
  label?: string
  quantity: IntegerValue
  unit: string
  state?: MeterState
  reason?: string
  reason_code?: string
}

export interface UsageMeter {
  metric_type: string
  unit: string
  quantity: IntegerValue
  rate_micros_per_unit?: string
  amount_micros: IntegerValue
  currency?: string
  state: MeterState
  reason?: string
  reason_code?: string
  cost_state?: MeterState
  cost_reason?: string
  cost_reason_code?: string
  last_updated?: string
}

export interface UsageLedgerRow {
  id: string
  occurred_at: string
  bucket: string
  unit: string
  quantity: IntegerValue
  rate_micros_per_unit?: string
  amount_micros: IntegerValue
  currency?: string
  user_id?: string
  user_label?: string
  user_resolved?: boolean
  harness_id?: string
  harness_label?: string
  harness_resolved?: boolean
  session_id?: string
  session_label?: string
  session_resolved?: boolean
  model_package_id?: string
  model_label?: string
  model_resolved?: boolean
  endpoint_id?: string
  endpoint_label?: string
  endpoint_resolved?: boolean
  pricing_state?: string
  meter_state: MeterState
  reason_code?: string
  included_in_totals: boolean
  applied_rate_micros_per_1k?: string
  applied_price_version?: string
  applied_price_source?: string
  project_id?: string
  project_label?: string
  project_resolved?: boolean
  adjustment?: boolean
}

export interface UsageConversion {
  source_currency: string
  target_currency: string
  source_amount_micros: IntegerValue
  rate?: string
  converted_amount_micros: IntegerValue
  rate_source?: string
  rate_as_of?: string
  rate_version?: string
  state: MeterState
  reason?: string
  reason_code?: string
}

export interface UsageReportData {
  range?: string
  window_start?: string
  window_end?: string
  last_updated?: string
  snapshot_at?: string
  total_tokens: IntegerValue
  input_tokens: IntegerValue
  output_tokens: IntegerValue
  total_tokens_state?: MeterState
  input_tokens_state?: MeterState
  output_tokens_state?: MeterState
  display_currency?: string
  display_total?: {
    amount_micros: IntegerValue
    currency: string
    state: MeterState
    reason?: string
    reason_code?: string
    rate?: string
    rate_source?: string
    rate_as_of?: string
  }
  cost_by_currency?: Record<string, { amount_micros: IntegerValue; currency: string; state: MeterState; reason?: string; reason_code?: string }>
  conversions?: UsageConversion[]
  by_unit?: Record<string, UsageValue>
  by_model?: Record<string, UsageValue>
  by_user?: Record<string, UsageValue>
  by_session?: Record<string, UsageValue>
  meters?: UsageMeter[]
  record_count: number
  reconciled: boolean
  reconciliation_errors?: string[]
  reconciliation_issues?: Array<{ code: string; message: string }>
  drilldown?: UsageLedgerRow[]
  ledger_has_more?: boolean
  ledger_next_cursor?: string
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
  if (value.state === 'error') return '집계 오류'
  const quantity = formatInteger(value.quantity)
  let formatted = `${quantity} ${value.unit}`
  if (value.unit === 'seconds') formatted = `${quantity}초`
  if (value.unit === 'bytes') formatted = `${quantity} B`
  if (value.unit === 'count') formatted = `${quantity}건`
  if (value.unit === 'tokens') formatted = `${quantity} 토큰`
  return value.state === 'delayed' ? `${formatted} · 지연` : formatted
}

function parseInteger(value?: IntegerValue): bigint | null {
  if (value == null || value === '') return null
  try { return BigInt(value) } catch { return null }
}

export function formatInteger(value?: IntegerValue): string {
  const parsed = parseInteger(value)
  return parsed == null ? '—' : parsed.toLocaleString('ko-KR')
}

export function formatUsageStateInteger(value: IntegerValue | undefined, state?: MeterState): string {
  if (state === 'error') return '집계 오류'
  if (state === 'unavailable' || !state) return '미수집'
  const formatted = formatInteger(value)
  return state === 'delayed' ? `${formatted} · 지연` : formatted
}

export function compareInteger(a?: IntegerValue, b?: IntegerValue): number {
  const left = parseInteger(a) || 0n
  const right = parseInteger(b) || 0n
  return left === right ? 0 : left > right ? 1 : -1
}

export function formatUsageAmount(amountMicros?: IntegerValue, currency?: string): string {
  if (amountMicros == null || !currency) return '—'
  const micros = parseInteger(amountMicros)
  if (micros == null) return '—'
  const negative = micros < 0n
  const absolute = negative ? -micros : micros
  const whole = absolute / 1_000_000n
  const fraction = (absolute % 1_000_000n).toString().padStart(6, '0').replace(/0+$/, '')
  const formatted = `${negative ? '-' : ''}${whole.toLocaleString('ko-KR')}${fraction ? `.${fraction}` : ''}`
  return currency === 'KRW' ? `₩${formatted}` : `${currency} ${formatted}`
}

function formatUsageRate(rateMicros?: string, currency?: string, unit?: string): string {
  if (!rateMicros || !currency || !unit) return '—'
  const unitLabel: Record<string, string> = { tokens: '토큰', seconds: '초', bytes: '바이트', count: '건' }
  return `${rateMicros} 마이크로 ${currency}/${unitLabel[unit] || unit}`
}

function formatDateTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('ko-KR')
}

function StateBadge({ state, reason, reasonCode }: { state: MeterState; reason?: string; reasonCode?: string }) {
  const meta = STATE_META[state] || STATE_META.error
  return <span title={usageReasonLabel(reason, reasonCode)} className={`inline-flex px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.className}`}>{meta.label}</span>
}

export function UsageReport({ report, id = 'usage-ledger', title = '사용량 및 비용 원장', loadMore }: {
  report: UsageReportData | null
  id?: string
  title?: string
  loadMore?: (cursor: string, signal?: AbortSignal) => Promise<UsageReportData>
}) {
  const { profile } = useAuth()
  const [ledger, setLedger] = useState<UsageLedgerRow[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [cursorHistory, setCursorHistory] = useState<string[]>([])
  const [pageNumber, setPageNumber] = useState(1)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadMoreError, setLoadMoreError] = useState(false)
  const generation = useRef(0)
  const activeRequest = useRef<AbortController | null>(null)
  const initialPage = useRef<{ ledger: UsageLedgerRow[]; nextCursor: string }>({ ledger: [], nextCursor: '' })

  useEffect(() => {
    const firstLedger = report?.drilldown || []
    const firstNextCursor = report?.ledger_next_cursor || ''
    initialPage.current = { ledger: firstLedger, nextCursor: firstNextCursor }
    setLedger(firstLedger)
    setNextCursor(firstNextCursor)
    setCursorHistory([])
    setPageNumber(1)
    setLoadMoreError(false)
    generation.current += 1
    activeRequest.current?.abort()
    return () => {
      generation.current += 1
      activeRequest.current?.abort()
      activeRequest.current = null
    }
  }, [report])

  if (!report) {
    return <div className="card p-4 text-[11px] text-gray-400">사용량 정보를 불러오지 못했습니다.</div>
  }
  const meters = report.meters || []
  const hasLedger = report.record_count > 0
  const delayedMeters = meters.filter(meter => meter.state === 'delayed').length
  const hasError = meters.some(meter => meter.state === 'error' || meter.cost_state === 'error') || report.display_total?.state === 'error'
  const hasUnavailable = meters.some(meter => meter.reason !== 'no meter event in selected window' && (meter.state === 'unavailable' || meter.cost_state === 'unavailable')) || report.display_total?.state === 'unavailable'
  const overallState: MeterState = !hasLedger ? 'unavailable' : hasError ? 'error' : hasUnavailable ? 'unavailable' : delayedMeters > 0 ? 'delayed' : 'recorded'
  const displayTotalAvailable = report.display_total?.state === 'recorded' || report.display_total?.state === 'zero'
  const reconciliationRows = usageReconciliationRows(report.reconciliation_issues, report.reconciliation_errors)

  const loadPage = async (cursor: string, direction: 'next' | 'previous') => {
    if (!loadMore || loadingMore) return
    setLoadingMore(true)
    setLoadMoreError(false)
    activeRequest.current?.abort()
    const controller = new AbortController()
    activeRequest.current = controller
    const requestGeneration = ++generation.current
    try {
      const next = await loadMore(cursor, controller.signal)
      if (requestGeneration !== generation.current) return
      setLedger(next.drilldown || [])
      setNextCursor(next.ledger_next_cursor || '')
      if (direction === 'next') {
        setCursorHistory(current => [...current, cursor])
        setPageNumber(current => current + 1)
      } else {
        setCursorHistory(current => current.slice(0, -1))
        setPageNumber(current => Math.max(1, current - 1))
      }
    } catch {
      if (!controller.signal.aborted) setLoadMoreError(true)
    } finally {
      if (requestGeneration === generation.current) {
        activeRequest.current = null
        setLoadingMore(false)
      }
    }
  }

  const loadPreviousPage = () => {
    const previous = previousUsageLedgerPage(cursorHistory)
    if (previous.restoreInitial) {
      activeRequest.current?.abort()
      activeRequest.current = null
      generation.current += 1
      setLedger(initialPage.current.ledger)
      setNextCursor(initialPage.current.nextCursor)
      setCursorHistory([])
      setPageNumber(1)
      setLoadingMore(false)
      setLoadMoreError(false)
      return
    }
    if (previous.cursor) void loadPage(previous.cursor, 'previous')
  }

  return (
    <div className="card p-4 space-y-4" id={id}>
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h3 className="text-xs font-bold">{title}</h3>
          <p className="text-[10px] text-gray-400 mt-1">
            계량 기간 {formatDateTime(report.window_start)} – {formatDateTime(report.window_end)} · 최신 계량 {formatDateTime(report.last_updated)}
          </p>
        </div>
        <StateBadge state={overallState} reason={!hasLedger ? '선택한 기간의 원장 기록이 없습니다.' : delayedMeters > 0 ? `${delayedMeters}개 계량 항목의 수집이 지연되었습니다.` : reconciliationRows.map(row => row.label).join('\n')} />
      </div>

      {!report.reconciled && (
        <div className="rounded border border-red-200 bg-red-50 p-3 text-[11px] text-red-700">
          <div className="font-semibold">원장 대사가 완료되지 않았습니다. 이 합계를 청구 또는 한도 판단에 사용하지 마십시오.</div>
          {reconciliationRows.map(row => <div key={row.key} title={row.diagnostic} className="mt-1">• {row.label}</div>)}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
        <div className="rounded border border-gray-100 p-3">
          <div className="text-[10px] text-gray-400">입력 / 출력 / 합계</div>
          <div className="text-xs font-semibold mt-1">{formatUsageStateInteger(report.input_tokens, report.input_tokens_state)} / {formatUsageStateInteger(report.output_tokens, report.output_tokens_state)} / {formatUsageStateInteger(report.total_tokens, report.total_tokens_state)}</div>
        </div>
        <div className="rounded border border-gray-100 p-3">
          <div className="text-[10px] text-gray-400">표시 통화 합계</div>
          <div className="text-xs font-semibold mt-1">{displayTotalAvailable ? formatUsageAmount(report.display_total?.amount_micros, report.display_total?.currency) : report.display_total?.state === 'error' ? '집계 오류' : '미수집'}</div>
          {report.display_total?.reason && <div className="text-[10px] text-red-600 mt-1">{usageReasonLabel(report.display_total.reason, report.display_total.reason_code)}</div>}
        </div>
        <div className="rounded border border-gray-100 p-3">
          <div className="text-[10px] text-gray-400">원천 통화 및 환산 근거</div>
          <div className="text-[10px] mt-1 space-y-0.5">
			{Object.values(report.cost_by_currency || {}).map(source => <div key={source.currency} className={source.state === 'recorded' || source.state === 'zero' ? '' : 'text-red-600'}>{source.currency}: {source.state === 'recorded' || source.state === 'zero' ? formatUsageAmount(source.amount_micros, source.currency) : usageReasonLabel(source.reason, source.reason_code) || '미확정'}</div>)}
            {Object.keys(report.cost_by_currency || {}).length === 0 && <div>비용 기록 없음</div>}
            {(report.conversions || []).map((conversion, index) => (
              <div key={`${conversion.source_currency}-${conversion.target_currency}-${conversion.rate_version || conversion.reason_code || index}`} className={conversion.state === 'recorded' ? 'text-gray-500' : 'text-red-600'}>
                {conversion.source_currency} → {conversion.target_currency}:{' '}
                {conversion.state === 'recorded'
                  ? `${formatUsageAmount(conversion.converted_amount_micros, conversion.target_currency)} · 환율 ${conversion.rate} · ${conversion.rate_source || '출처 미기록'} · ${conversion.rate_as_of || '기준일 미기록'} · ${conversion.rate_version || '버전 미기록'}`
                  : usageReasonLabel(conversion.reason, conversion.reason_code) || '환산 불가'}
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-[11px]">
          <thead><tr className="text-left text-gray-400 border-b">
            <th className="py-2">계량 항목</th><th>계량</th><th>비용</th><th className="text-right">수량</th><th className="text-right">단가</th><th className="text-right">금액</th><th>최종 계량</th>
          </tr></thead>
          <tbody>
            {meters.map((meter, index) => {
              const costAvailable = meter.cost_state === 'recorded' || meter.cost_state === 'zero'
              return <tr key={`${meter.metric_type}-${meter.unit}-${meter.currency || ''}-${index}`} className="border-b border-gray-50">
                <td className="py-2 font-medium text-gray-700">{usageMetricLabel(meter.metric_type)}</td>
                <td><StateBadge state={meter.state} reason={meter.reason} reasonCode={meter.reason_code} /></td>
                <td><StateBadge state={meter.cost_state || 'unavailable'} reason={meter.cost_reason} reasonCode={meter.cost_reason_code} /></td>
                <td className="text-right font-mono">{formatUsageQuantity(meter)}</td>
                <td className="text-right font-mono text-gray-500">{costAvailable ? formatUsageRate(meter.rate_micros_per_unit, meter.currency, meter.unit) : '미확정'}</td>
                <td className="text-right font-mono">{costAvailable ? formatUsageAmount(meter.amount_micros, meter.currency) : '미확정'}</td>
                <td className="text-gray-400">{formatDateTime(meter.last_updated)}</td>
              </tr>
            })}
          </tbody>
        </table>
      </div>

      <details open>
        <summary className="cursor-pointer text-xs font-bold text-gray-700">원장 상세 · {pageNumber}페이지 · 총 {report.record_count.toLocaleString()}건</summary>
        <p className="text-[10px] text-gray-400 mt-1 mb-2">각 행은 집계 전 원본 계량 기록입니다. 수량·단가·금액과 관련 사용자 및 세션을 함께 확인할 수 있습니다.</p>
        <div className="overflow-x-auto max-h-96">
          <table className="w-full text-[10px]">
            <thead className="sticky top-0 bg-white"><tr className="text-left text-gray-400 border-b">
              <th className="py-2">발생 시각</th><th>항목</th><th>계량 상태</th><th>가격 상태</th><th className="text-right">수량</th><th className="text-right">단가</th><th className="text-right">금액</th><th>관련 항목</th><th>원장 ID</th>
            </tr></thead>
            <tbody>
              {ledger.map((row) => (
                <tr key={row.id} className={`border-b border-gray-50 ${row.included_in_totals ? '' : 'bg-red-50/50'}`}>
                  <td className="py-2 whitespace-nowrap">{formatDateTime(row.occurred_at)}</td>
                  <td>{usageMetricLabel(row.bucket)}</td>
                  <td><StateBadge state={row.meter_state || 'error'} reason={row.included_in_totals ? undefined : '이 행은 합계에서 제외되었습니다.'} reasonCode={row.reason_code} /></td>
                  <td><StateBadge state={row.pricing_state === 'priced' ? 'recorded' : row.pricing_state === 'error' ? 'error' : 'unavailable'} reason={row.pricing_state === 'pending' ? '단가 계산이 진행 중입니다.' : row.pricing_state === 'unpriced' ? '단가가 확정되지 않았습니다.' : undefined} /></td>
                  <td className="text-right font-mono">{formatUsageQuantity({ quantity: row.quantity, unit: row.unit })}</td>
                  <td className="text-right font-mono text-gray-500">
                    {row.applied_rate_micros_per_1k != null && (row.bucket === 'tokens_in' || row.bucket === 'tokens_out')
                      ? `${formatUsageAmount(row.applied_rate_micros_per_1k, row.currency)} / 1K 토큰`
                      : formatUsageRate(row.rate_micros_per_unit, row.currency, row.unit)}
                    {(row.applied_price_version || row.applied_price_source) && <div className="text-[9px] text-gray-400">{[row.applied_price_version, row.applied_price_source].filter(Boolean).join(' · ')}</div>}
                  </td>
                  <td className="text-right font-mono">{formatUsageAmount(row.amount_micros, row.currency)}</td>
                  <td className="space-x-2 whitespace-nowrap">
                    {profile === 'customer' ? <>
                      {row.user_resolved ? <Link title={row.user_label} className="text-blue-600 hover:underline" to={`/users/${row.user_id}?tab=usage`}>사용자</Link> : row.user_id && <span className="text-gray-400" title="삭제되었거나 열람할 수 없습니다">사용자(미확인)</span>}
                      {row.session_resolved ? <Link title={row.session_label} className="text-blue-600 hover:underline" to={`/sessions/${row.session_id}`}>세션</Link> : row.session_id && <span className="text-gray-400" title="삭제되었거나 열람할 수 없습니다">세션(미확인)</span>}
                      {row.harness_resolved ? <Link title={row.harness_label} className="text-blue-600 hover:underline" to={`/harnesses/${row.harness_id}`}>하네스</Link> : row.harness_id && <span className="text-gray-400" title="삭제되었거나 열람할 수 없습니다">하네스(미확인)</span>}
                      {row.model_resolved ? <Link title={row.model_label} className="text-blue-600 hover:underline" to={`/models/${row.model_package_id}`}>모델</Link> : row.model_package_id && <span className="text-gray-400" title="삭제되었거나 열람할 수 없습니다">모델(미확인)</span>}
                      {row.endpoint_resolved ? <Link title={row.endpoint_label} className="text-blue-600 hover:underline" to={`/endpoints/${row.endpoint_id}`}>엔드포인트</Link> : row.endpoint_id && <span className="text-gray-400" title="삭제되었거나 열람할 수 없습니다">엔드포인트(미확인)</span>}
                      {row.project_resolved ? <Link title={row.project_label} className="text-blue-600 hover:underline" to={`/projects/${row.project_id}`}>프로젝트</Link> : row.project_id && <span className="text-gray-400" title="삭제되었거나 열람할 수 없습니다">프로젝트(미확인)</span>}
					</> : <span className="text-gray-500">{[row.user_id && '사용자', row.session_id && '세션', row.harness_id && '하네스', row.model_package_id && '모델', row.endpoint_id && '엔드포인트', row.project_id && '프로젝트'].filter(Boolean).join(' · ') || '—'}</span>}
                  </td>
                  <td className="font-mono text-gray-400 break-all">{row.id}</td>
                </tr>
              ))}
              {ledger.length === 0 && <tr><td colSpan={9} className="py-6 text-center text-gray-400">선택한 기간의 원장 기록이 없습니다.</td></tr>}
            </tbody>
          </table>
        </div>
        {loadMore && (nextCursor || cursorHistory.length > 0) && (
          <div className="mt-3 flex justify-center gap-2">
            <button type="button" className="btn-sm btn-secondary" disabled={loadingMore || cursorHistory.length === 0} onClick={loadPreviousPage}>이전 페이지</button>
            <button type="button" className="btn-sm btn-secondary" disabled={loadingMore || !nextCursor} onClick={() => loadPage(nextCursor, 'next')}>{loadingMore ? '불러오는 중...' : '다음 페이지'}</button>
          </div>
        )}
        {loadMoreError && <p className="mt-2 text-center text-[10px] text-red-600">다음 원장 기록을 불러오지 못했습니다. 다시 시도해 주십시오.</p>}
      </details>
    </div>
  )
}
