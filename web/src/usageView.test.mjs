import test from 'node:test'
import assert from 'node:assert/strict'

import { previousUsageLedgerPage, usageDimensionHref, usageReconciliationRows } from './usageView.ts'

test('returning from page two restores the frozen initial ledger instead of issuing an unsigned request', () => {
  assert.deepEqual(previousUsageLedgerPage(['cursor-page-2']), { restoreInitial: true })
  assert.deepEqual(previousUsageLedgerPage(['cursor-page-2', 'cursor-page-3']), { restoreInitial: false, cursor: 'cursor-page-2' })
  assert.deepEqual(previousUsageLedgerPage([]), { restoreInitial: false })
})

test('usage dimension links exist only where exact detail routes are registered', () => {
  assert.equal(usageDimensionHref('customer', 'model', 'pmp-1', true), '/models/pmp-1')
  assert.equal(usageDimensionHref('customer', 'user', 'usr-1', true), '/users/usr-1?tab=usage')
  assert.equal(usageDimensionHref('patty_ops', 'model', 'pmp-1', true), undefined)
  assert.equal(usageDimensionHref('patty_ops', 'user', 'usr-1', true), undefined)
  assert.equal(usageDimensionHref('customer', 'model', '__other__', true), undefined)
  assert.equal(usageDimensionHref('customer', 'model', 'deleted', false), undefined)
})

test('reconciliation rows localize stable reason codes while retaining diagnostics', () => {
  assert.deepEqual(usageReconciliationRows(
    [{ code: 'pricing_unavailable', message: '1 usage records are metered but unpriced' }],
    ['legacy fallback should not win'],
  ), [{ key: 'pricing_unavailable:1 usage records are metered but unpriced', label: '단가가 확정되지 않았습니다.', diagnostic: '1 usage records are metered but unpriced' }])
  assert.deepEqual(usageReconciliationRows(undefined, ['missing conversion rate from EUR to KRW']), [
    { key: 'missing conversion rate from EUR to KRW', label: '표시 통화 환율이 설정되지 않았습니다: EUR to KRW', diagnostic: 'missing conversion rate from EUR to KRW' },
  ])
  assert.deepEqual(usageReconciliationRows(
    [{ code: 'fallback_timing', message: '2 usage ledger records have no occurred_at; created_at fallback used' }],
  ), [{
    key: 'fallback_timing:2 usage ledger records have no occurred_at; created_at fallback used',
    label: '발생 시각이 없는 일부 기록을 생성 시각 기준으로 집계했습니다.',
    diagnostic: '2 usage ledger records have no occurred_at; created_at fallback used',
  }])
  assert.equal(usageReconciliationRows([
    { code: 'fx_conversion_overflow', message: 'conversion result exceeds the supported range for KRW->USD' },
  ])[0].label, '환산 금액이 지원 범위를 초과했습니다.')
  assert.equal(usageReconciliationRows([
    { code: 'quantity_total_overflow', message: 'input token total exceeds the supported range' },
  ])[0].label, '사용량 합계가 지원 범위를 초과했습니다.')
})
