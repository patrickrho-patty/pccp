import { test } from 'node:test'
import assert from 'node:assert/strict'

import {
  effectiveColor, daySegmentColor, daySegmentLabel, buildNinetyDayBar,
  COLOR_KO, INCIDENT_STATE_KO,
} from './publicStatusView.ts'

test('effectiveColor: unexpired override wins, expired falls back to measured', () => {
  const now = new Date('2026-08-19T00:00:00Z')
  assert.equal(effectiveColor({ measured_color: 'red', override_color: 'green', override_expires_at: '2026-08-19T01:00:00Z' }, now), 'green')
  assert.equal(effectiveColor({ measured_color: 'red', override_color: 'green', override_expires_at: '2026-08-18T23:00:00Z' }, now), 'red')
  assert.equal(effectiveColor({ measured_color: 'orange' }, now), 'orange')
  assert.equal(effectiveColor({}, now), 'gray')
})

test('daySegmentColor thresholds distinguish outage, partial, maintenance, no-data', () => {
  assert.equal(daySegmentColor({ availability_pct: 100 }), 'green')
  assert.equal(daySegmentColor({ availability_pct: 97 }), 'yellow')
  assert.equal(daySegmentColor({ availability_pct: 85 }), 'orange')
  assert.equal(daySegmentColor({ availability_pct: 40 }), 'red')
  assert.equal(daySegmentColor({ availability_pct: 100, maintenance_seconds: 600 }), 'blue')
  assert.equal(daySegmentColor({ availability_pct: 0, no_data_seconds: 86400 }), 'gray')
})

test('daySegmentLabel is Korean and includes impact + maintenance minutes', () => {
  const label = daySegmentLabel({ date_kst: '2026-08-19', availability_pct: 98.5, impacted_seconds: 1200, maintenance_seconds: 300 })
  assert.ok(label.includes('2026-08-19 (KST)'))
  assert.ok(label.includes('98.50%'))
  assert.ok(label.includes('영향 20분'))
  assert.ok(label.includes('점검 5분'))
})

test('buildNinetyDayBar pads to 90 segments ending today (KST), newest last', () => {
  const now = new Date('2026-08-19T15:00:00Z') // 2026-08-20 00:00 KST
  const bar = buildNinetyDayBar([{ date_kst: '2026-08-20' }], now)
  assert.equal(bar.length, 90)
  assert.equal(bar[89].date_kst, '2026-08-20') // today KST, has data
  assert.equal(bar[0].date_kst, null) // oldest slot has no data → gray no-data segment
})

test('Korean label registry covers every color and incident lifecycle state', () => {
  for (const c of ['green', 'yellow', 'orange', 'red', 'blue', 'gray']) assert.ok(COLOR_KO[c], c)
  for (const s of ['investigating', 'mitigating', 'monitoring', 'resolved', 'maintenance_scheduled', 'maintenance_in_progress'])
    assert.ok(INCIDENT_STATE_KO[s], s)
})
