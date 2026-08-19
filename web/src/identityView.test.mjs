import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  buildIdentityContext, resolveUser, resolveHarness, resolveActor,
  freshnessLabel, editDeleteDecision, readReceiptLabel, userLabel,
} from './identityView.ts'

const ctx = buildIdentityContext(
  [{ id: 'usr_demo_park', name_ko: '박서연', email: 'park@patty.dev', role: 'engineer' }],
  [{ harness_id: 'hrn_demo_1', name: 'prod-runner-03' }],
)

test('user resolves to Korean label + exact route, never bare ID', () => {
  const v = resolveUser('usr_demo_park', ctx)
  assert.equal(v.label, '박서연')
  assert.equal(v.route, '/users/usr_demo_park')
  assert.equal(v.tombstone, false)
  assert.equal(v.role, 'engineer')
  assert.ok(!v.label.includes('usr_demo_park'))
})

test('unknown/deleted user gets a stable tombstone label retaining the raw id', () => {
  const v = resolveUser('usr_gone', ctx)
  assert.equal(v.tombstone, true)
  assert.match(v.label, /삭제/)
  assert.equal(v.raw, 'usr_gone')
})

test('harness resolves by peer id with exact fleet link', () => {
  const v = resolveHarness('hrn_demo_1', ctx)
  assert.equal(v.label, 'prod-runner-03')
  assert.equal(v.route, '/fleet?harness_id=hrn_demo_1')
  const gone = resolveHarness('hrn_missing', ctx)
  assert.equal(gone.tombstone, true)
})

test('actor resolves by sender type (system/service/user/harness)', () => {
  assert.equal(resolveActor('op', 'system', ctx).label, '시스템')
  assert.equal(resolveActor('svc1', 'service', ctx).label, '서비스')
  assert.equal(resolveActor('usr_demo_park', 'user', ctx).label, '박서연')
  assert.equal(resolveActor('hrn_demo_1', 'harness', ctx).label, 'prod-runner-03')
  assert.equal(resolveActor('usr_demo_park', undefined, ctx).label, '박서연')
})

test('freshness labels are relative and human-readable', () => {
  assert.match(freshnessLabel(new Date(Date.now() - 90_000).toISOString()), /1분 전/)
  assert.match(freshnessLabel(new Date(Date.now() - 7_200_000).toISOString()), /2시간 전/)
  assert.equal(freshnessLabel(undefined), '시각 미기록')
})

test('edit/delete distinguish author-owned vs moderation', () => {
  const mine = editDeleteDecision({ sender_id: 'u1', sender_type: 'user' }, { id: 'u1', isAdmin: false })
  assert.equal(mine.canEdit, true); assert.equal(mine.canDelete, true); assert.equal(mine.moderation, false)
  const admin = editDeleteDecision({ sender_id: 'u1', sender_type: 'user' }, { id: 'op', isAdmin: true })
  assert.equal(admin.canEdit, false); assert.equal(admin.canDelete, true); assert.equal(admin.moderation, true)
  const outsider = editDeleteDecision({ sender_id: 'u1', sender_type: 'user' }, { id: 'u2', isAdmin: false })
  assert.equal(outsider.canEdit, false); assert.equal(outsider.canDelete, false)
  const system = editDeleteDecision({ sender_id: 'svc', sender_type: 'system' }, { id: 'op', isAdmin: true })
  assert.equal(system.canEdit, false); assert.equal(system.canDelete, false)
})

test('read receipts are explainable with resolved names', () => {
  const label = readReceiptLabel(['usr_demo_park'], ctx)
  assert.match(label, /읽음 1 \(박서연\)/)
  const multi = readReceiptLabel(['usr_demo_park', 'usr_gone'], ctx)
  assert.match(multi, /읽음 2/)
})

test('userLabel prefers Korean name over email', () => {
  assert.equal(userLabel({ name_ko: '김민서', name: 'Kim', email: 'x@y.z' }), '김민서')
  assert.equal(userLabel({ email: 'a@b.c' }), 'a@b.c')
})
