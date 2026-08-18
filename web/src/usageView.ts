export type UsageConsoleProfile = 'patty_ops' | 'customer' | 'portal'
export type UsageDimension = 'model' | 'user'

const REASON_LABELS: Record<string, string> = {
  no_meter_event: '선택한 기간의 계량 기록이 없습니다.',
  no_ledger_records: '선택한 기간의 원장 기록이 없습니다.',
  meter_delayed: '계량 이벤트 수집이 지연되었습니다.',
  fallback_timing: '발생 시각이 없는 일부 기록을 생성 시각 기준으로 집계했습니다.',
  invalid_meter_unit: '계량 항목과 단위가 일치하지 않습니다.',
  pricing_unavailable: '단가가 확정되지 않았습니다.',
  pricing_pending: '단가 계산이 진행 중입니다.',
  pricing_error: '단가 정보가 올바르지 않습니다.',
  currency_missing: '비용 통화가 누락되었습니다.',
  fx_rate_missing: '표시 통화 환율이 설정되지 않았습니다.',
  fx_rate_invalid: '표시 통화 환율 정보가 올바르지 않습니다.',
  fx_conversion_overflow: '환산 금액이 지원 범위를 초과했습니다.',
  quantity_total_overflow: '사용량 합계가 지원 범위를 초과했습니다.',
  cost_total_overflow: '표시 통화 합계가 지원 범위를 초과했습니다.',
}

export function usageReasonLabel(reason?: string, reasonCode?: string): string | undefined {
  if (reasonCode && REASON_LABELS[reasonCode]) return REASON_LABELS[reasonCode]
  if (!reason) return undefined
  if (reason === 'no usage ledger records in selected window') return '선택한 기간의 원장 기록이 없습니다.'
  if (reason === 'no meter event in selected window') return '선택한 기간의 계량 기록이 없습니다.'
  if (reason.includes('meter event arrived more than 15 minutes')) return '계량 이벤트가 발생 시각보다 15분 이상 늦게 수집되었습니다.'
  if (reason.startsWith('missing conversion rate from ')) return `표시 통화 환율이 설정되지 않았습니다: ${reason.slice('missing conversion rate from '.length)}`
  if (reason.startsWith('invalid conversion rate for ')) return `표시 통화 환율 값이 올바르지 않습니다: ${reason.slice('invalid conversion rate for '.length)}`
  if (reason.includes('has no occurred_at')) return `원장 발생 시각이 없어 생성 시각으로 임시 집계했습니다: ${reason.split(' ')[2] || ''}`
  if (reason.includes('has no recognized unit')) return `원장 단위를 확인할 수 없습니다: ${reason.split(' ')[2] || ''}`
  if (reason.includes('has cost without currency')) return `원장 비용 통화가 누락되었습니다: ${reason.split(' ')[2] || ''}`
  return reason
}

export function previousUsageLedgerPage(cursorHistory: string[]): { restoreInitial: boolean; cursor?: string } {
  if (cursorHistory.length === 0) return { restoreInitial: false }
  if (cursorHistory.length === 1) return { restoreInitial: true }
  return { restoreInitial: false, cursor: cursorHistory[cursorHistory.length - 2] }
}

export function usageDimensionHref(profile: UsageConsoleProfile, dimension: UsageDimension, id: string, resolved: boolean): string | undefined {
  if (profile !== 'customer' || !resolved || !id || id.startsWith('__')) return undefined
  return dimension === 'model' ? `/models/${id}` : `/users/${id}?tab=usage`
}

export function usageReconciliationRows(
  issues?: Array<{ code: string; message: string }>,
  legacyErrors?: string[],
): Array<{ key: string; label: string; diagnostic: string }> {
  if (issues?.length) {
    return issues.map(issue => ({
      key: `${issue.code}:${issue.message}`,
      label: usageReasonLabel(issue.message, issue.code) || issue.message,
      diagnostic: issue.message,
    }))
  }
  return (legacyErrors || []).map(message => ({
    key: message,
    label: usageReasonLabel(message) || message,
    diagnostic: message,
  }))
}
