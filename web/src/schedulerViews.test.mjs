import test from 'node:test'
import assert from 'node:assert/strict'

import { kvDirViewModel, pdRows, programsViewModel, shadowViewModel } from './schedulerViews.ts'

test('kvDirViewModel orders tiers HBM-first and tolerates empty views', () => {
  const vm = kvDirViewModel({
    extents: 3,
    locations_verified: 2,
    locations_unverified: 1,
    by_tier: { 'L3-disk': 1, 'L1-hbm': 2 },
    hot_prefixes: [{ hash: 'h1', hits: 9, replicas: 1, tokens: 2048 }],
  })
  assert.equal(vm.extents, 3)
  assert.deepEqual(vm.tiers.map((t) => t.tier), ['L1-hbm', 'L3-disk'])
  assert.equal(vm.hotPrefixes[0].hits, 9)

  const empty = kvDirViewModel(undefined)
  assert.deepEqual(empty.tiers, [])
  assert.equal(empty.extents, 0)
})

test('pdRows flags engaged models missing a role as imbalanced', () => {
  const rows = pdRows([
    { model: 'b', prefill_share: 0.7, disaggregation_engaged: true, prefill_workers: 0, decode_workers: 1, aggregated_workers: 0 },
    { model: 'a', prefill_share: 0.2, disaggregation_engaged: false, prefill_workers: 0, decode_workers: 0, aggregated_workers: 3 },
  ])
  assert.equal(rows[0].model, 'b')
  assert.equal(rows[0].imbalance, true)
  assert.equal(rows[1].imbalance, false)
  assert.deepEqual(pdRows(null), [])
})

test('programsViewModel maps counters with zero defaults', () => {
  assert.deepEqual(programsViewModel({ programs: 2, tool_paused: 1, pause_prediction_errors: 3, turns: 9 }), {
    programs: 2,
    toolPaused: 1,
    predictionErrors: 3,
    turns: 9,
  })
  assert.deepEqual(programsViewModel(undefined), { programs: 0, toolPaused: 0, predictionErrors: 0, turns: 0 })
})

test('shadowViewModel computes agreement pct and canary state', () => {
  const vm = shadowViewModel({
    receipts: 10,
    shadowed: 4,
    agreement_rate: 0.75,
    filtered: { overloaded: 3, 'model-mismatch': 1 },
    canary: { capability: 'stage-planner/v1', state: 'active', active: true },
  })
  assert.equal(vm.agreementPct, 75)
  assert.deepEqual(vm.filtered, [
    { reason: 'overloaded', count: 3 },
    { reason: 'model-mismatch', count: 1 },
  ])
  assert.equal(vm.canary?.state, 'active')

  const none = shadowViewModel({ receipts: 2, shadowed: 0 })
  assert.equal(none.agreementPct, null)
  assert.equal(none.canary, null)
})
