import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  approvalView, approvalTypeKo, approvalRiskKo, approvalAgeLabel,
  approvalExpiryLabel, rankApprovals,
} from './approvalView.ts'

test('approvalTypeKo renders Korean labels, not raw tool_use', () => {
  assert.equal(approvalTypeKo('tool_use_bash'), '도구 실행')
  assert.equal(approvalTypeKo('file_write'), '파일 작성')
  assert.equal(approvalTypeKo('model_use'), '모델 사용')
  assert.equal(approvalTypeKo('network_egress'), '네트워크')
  assert.equal(approvalTypeKo(undefined), '승인')
})

test('risk labels are Korean', () => {
  assert.equal(approvalRiskKo('critical'), '심각')
  assert.equal(approvalRiskKo('high'), '높음')
  assert.equal(approvalRiskKo(''), '중간')
})

test('age and expiry labels are relative + human-readable', () => {
  assert.equal(approvalAgeLabel(0), '0초')
  assert.equal(approvalAgeLabel(90), '1분')
  assert.equal(approvalAgeLabel(7200), '2시간')
  assert.equal(approvalAgeLabel(3 * 86400), '3일')
  assert.equal(approvalAgeLabel(undefined), '—')
  assert.equal(approvalExpiryLabel(300, false), '5분 내')
  assert.equal(approvalExpiryLabel(undefined, false), undefined)
  assert.equal(approvalExpiryLabel(-1, true), '만료됨')
})

test('approvalView builds the governed decision contract', () => {
  const v = approvalView({
    approval_type: 'tool_use_bash', approval_type_ko: '도구 실행',
    tool_name: 'bash', risk: 'critical', requested_by_name: '김민서',
    session_title: '환불 로직 구현', waiting_age_seconds: 300,
    remaining_seconds: 1800, expired: false, detail_route: '/sessions/ses_1',
  })
  assert.ok(v.title.includes('도구 실행'))
  assert.ok(v.title.includes('bash'))
  assert.ok(v.title.includes('심각'))
  assert.equal(v.requestedBy, '김민서')
  assert.equal(v.sessionTitle, '환불 로직 구현')
  assert.equal(v.ageLabel, '5분')
  assert.equal(v.expiresLabel, '30분 내')
  assert.equal(v.detailRoute, '/sessions/ses_1')
})

test('ranking is documented: expired first, then risk, then oldest', () => {
  const rows = [
    { id: 'old-low', risk: 'low', waiting_age_seconds: 900, expired: false },
    { id: 'urgent', risk: 'high', waiting_age_seconds: 60, expired: false },
    { id: 'expired', risk: 'low', waiting_age_seconds: 300, expired: true },
    { id: 'critical-fresh', risk: 'critical', waiting_age_seconds: 5, expired: false },
  ]
  const ranked = rankApprovals(rows).map(r => r.id)
  assert.equal(ranked[0], 'expired')           // expired always first
  assert.equal(ranked[1], 'critical-fresh')    // then highest risk
  assert.equal(ranked[2], 'urgent')            // then next risk
  assert.equal(ranked[3], 'old-low')           // then oldest
})
