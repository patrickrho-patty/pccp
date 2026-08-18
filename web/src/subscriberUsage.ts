export interface SubscriberUsageSummary {
  input_tokens?: string | number
  output_tokens?: string | number
  record_count?: string | number
  state?: 'recorded' | 'zero' | 'unavailable' | 'delayed' | 'error'
  reason_code?: string
  complete?: boolean
}

function exactInteger(value: string | number | undefined): bigint | null {
  if (typeof value === 'number') {
    return Number.isSafeInteger(value) ? BigInt(value) : null
  }
  if (typeof value !== 'string' || !/^-?\d+$/.test(value)) return null
  try { return BigInt(value) } catch { return null }
}

export function summarizeSubscriberUsage(summary?: SubscriberUsageSummary) {
  if (!summary) {
    return {
      available: false, complete: false, state: 'unavailable', reasonCode: 'no_usage_summary',
      tokensIn: 0n, tokensOut: 0n, total: 0n, records: 0n,
    }
  }
  const state = summary?.state || 'unavailable'
  const tokensIn = exactInteger(summary?.input_tokens)
  const tokensOut = exactInteger(summary?.output_tokens)
  const records = exactInteger(summary?.record_count)
  const complete = summary?.complete === true
  const valid = tokensIn !== null && tokensOut !== null && records !== null && records >= 0n
  const available = complete && valid && (state === 'recorded' || state === 'zero' || state === 'delayed')
  const safeInput = tokensIn ?? 0n
  const safeOutput = tokensOut ?? 0n
  return {
    available,
    complete: complete && valid,
    state: valid ? state : 'error',
    reasonCode: valid ? summary?.reason_code : 'invalid_usage_summary',
    tokensIn: safeInput,
    tokensOut: safeOutput,
    total: safeInput + safeOutput,
    records: records ?? 0n,
  }
}

export function formatCompactTokens(value: bigint): string {
  const negative = value < 0n
  const absolute = negative ? -value : value
  const whole = absolute / 1_000n
  const tenth = (absolute % 1_000n) / 100n
  return `${negative ? '-' : ''}${whole.toLocaleString('ko-KR')}.${tenth}K`
}
