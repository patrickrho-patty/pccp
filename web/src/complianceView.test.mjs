import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  taskState, dueAgeLabel, evidenceSourceKo, evidenceFreshnessLabel,
  parseControlResults, groupAssessmentRuns,
} from './complianceView.ts'

test('task states are current-state only with Korean labels + next action', () => {
  assert.equal(taskState('open').labelKo, '미착수')
  assert.equal(taskState('open').nextActionKo, '담당자 배정 후 시작')
  assert.equal(taskState('in_progress').labelKo, '진행 중')
  assert.equal(taskState('done').labelKo, '완료')
  // unknown state must not crash and returns a safe generic label
  assert.equal(taskState('weird').labelKo, 'weird')
  assert.equal(taskState(undefined).labelKo, '미심판')
})

test('due date age labels', () => {
  const past = new Date(Date.now() - 3 * 86400000).toISOString().slice(0, 10)
  assert.match(dueAgeLabel(past) || '', /기한 초과 [34]일/) // DST/timezone tolerant
  const soon = new Date(Date.now() + 2 * 86400000).toISOString().slice(0, 10)
  assert.match(dueAgeLabel(soon) || '', /[12]일 내|마감 D-[12]/)
  assert.equal(dueAgeLabel(undefined), undefined)
})

test('evidence source + freshness labels', () => {
  assert.equal(evidenceSourceKo('audit'), '감사')
  assert.equal(evidenceSourceKo('manual'), '수동 등록')
  assert.equal(evidenceSourceKo(undefined), '기록')
  const fresh = new Date().toISOString()
  assert.match(evidenceFreshnessLabel(fresh), /오늘 수집|0일 전 수집/)
  assert.equal(evidenceFreshnessLabel(undefined), '시각 미기록')
})

test('parseControlResults reads the assessment snapshot JSON', () => {
  const raw = JSON.stringify([
    { control_id: '2.4.1', status: 'gap', gap_description_ko: '갭 설명' },
    { control_id: '1.1.1', status: 'compliant' },
  ])
  const map = parseControlResults(raw)
  assert.equal(map['2.4.1'][0].status, 'gap')
  assert.equal(map['1.1.1'][0].status, 'compliant')
  assert.deepEqual(parseControlResults(undefined), {})
})

test('repeated identical runs group into one row and preserve every snapshot', () => {
  const runs = [
    { id: 'a1', scope: 'SaaS', level: '일반', overall_status: 'gap', open_gaps: 2, results: '', assessed_at: '2026-08-18T10:00:00Z' },
    { id: 'a2', scope: 'SaaS', level: '일반', overall_status: 'gap', open_gaps: 2, results: '', assessed_at: '2026-08-18T10:01:00Z' },
    { id: 'a3', scope: 'SaaS', level: '일반', overall_status: 'gap', open_gaps: 2, results: '', assessed_at: '2026-08-18T10:02:00Z' },
    { id: 'b1', scope: 'SaaS', level: '일반', overall_status: 'compliant', open_gaps: 0, results: '', assessed_at: '2026-08-18T12:00:00Z' },
  ]
  const { grouped } = groupAssessmentRuns(runs)
  assert.equal(grouped.length, 2)               // 3 identical + 1 distinct
  assert.equal(grouped[0].count, 3)             // burst grouped with count
  assert.equal(grouped[0].id, 'a1')             // newest of the group is the drill target
  assert.equal(grouped[1].count, 1)
})

test('change summary lists controls whose status changed between runs', () => {
  const runs = [
    { id: 'a1', scope: 'SaaS', level: '일반', overall_status: 'gap', open_gaps: 1, assessed_at: '2026-08-18T10:00:00Z', results: JSON.stringify([{ control_id: '2.4.1', status: 'gap' }]) },
    { id: 'a2', scope: 'SaaS', level: '일반', overall_status: 'gap', open_gaps: 1, assessed_at: '2026-08-18T10:01:00Z', results: JSON.stringify([{ control_id: '2.4.1', status: 'gap' }]) },
    { id: 'b1', scope: 'SaaS', level: '일반', overall_status: 'compliant', open_gaps: 0, assessed_at: '2026-08-18T12:00:00Z', results: JSON.stringify([{ control_id: '2.4.1', status: 'compliant' }]) },
  ]
  const { changedControls } = groupAssessmentRuns(runs)
  assert.ok(changedControls['b1'])
  assert.match(changedControls['b1'][0], /2.4.1/)
  assert.match(changedControls['b1'][0], /gap → compliant/)
})
