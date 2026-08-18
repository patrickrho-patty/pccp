import test from 'node:test'
import assert from 'node:assert/strict'

import { formatCompactTokens, summarizeSubscriberUsage } from './subscriberUsage.ts'

test('subscriber usage consumes the uncapped server aggregate and preserves integers above Number.MAX_SAFE_INTEGER', () => {
  const summary = summarizeSubscriberUsage({
    input_tokens: '9007199254740993',
    output_tokens: '7',
    record_count: '202',
    state: 'recorded',
    complete: true,
  })
  assert.equal(summary.available, true)
  assert.equal(summary.total, 9007199254741000n)
  assert.equal(summary.records, 202n)
})

test('subscriber usage refuses incomplete or malformed server aggregates', () => {
  assert.equal(summarizeSubscriberUsage({ input_tokens: '10', output_tokens: '2', record_count: '2', state: 'recorded', complete: false }).available, false)
  assert.equal(summarizeSubscriberUsage({ input_tokens: 'not-an-integer', output_tokens: '2', record_count: '2', state: 'recorded', complete: true }).available, false)
  assert.equal(summarizeSubscriberUsage({ input_tokens: '10', output_tokens: '2', record_count: '2', state: 'error', complete: true }).available, false)
})

test('subscriber usage keeps an exact delayed aggregate available', () => {
  const summary = summarizeSubscriberUsage({ input_tokens: '10', output_tokens: '2', record_count: '2', state: 'delayed', reason_code: 'meter_delayed', complete: true })
  assert.equal(summary.available, true)
  assert.equal(summary.state, 'delayed')
  assert.equal(summary.total, 12n)
})

test('subscriber token formatting supports bigint values without Number coercion', () => {
  assert.equal(formatCompactTokens(9_007_199_254_740_993n), '9,007,199,254,740.9K')
})

test('missing usage is unavailable rather than fabricated as zero', () => {
  const summary = summarizeSubscriberUsage(undefined)
  assert.equal(summary.available, false)
  assert.equal(summary.state, 'unavailable')
})
