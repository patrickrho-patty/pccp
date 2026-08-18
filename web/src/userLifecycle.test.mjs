import test from 'node:test'
import assert from 'node:assert/strict'

import { canIssueEnrollment, lifecycleDenialLabel, userActions } from './userLifecycleView.ts'

test('lifecycle controls are derived only from server-authorized actions', () => {
  assert.deepEqual(userActions(['suspend', 'offboard']).map(action => action.action), ['suspend', 'offboard'])
  assert.deepEqual(userActions([]), [])
  assert.deepEqual(userActions(undefined), [])
  assert.deepEqual(userActions(['resume', 'unknown']), [
    {
      action: 'resume',
      label: '재활성화',
      title: '계정 재활성화',
      danger: false,
      effect: '정지가 해제되고 계정이 활성 상태로 복원됩니다.',
    },
  ])
})

test('enrollment requires both active state and server management permission', () => {
  assert.equal(canIssueEnrollment('active', true), true)
  assert.equal(canIssueEnrollment('active', false), false)
  assert.equal(canIssueEnrollment('suspended', true), false)
  assert.equal(canIssueEnrollment('offboarded', true), false)
})

test('stable lifecycle denial codes render actionable Korean explanations', () => {
	assert.equal(lifecycleDenialLabel('insufficient_role'), '사용자 수명주기를 변경할 관리자 권한이 없습니다.')
	assert.equal(lifecycleDenialLabel('self_action'), '자신의 계정 상태는 직접 변경할 수 없습니다.')
	assert.equal(lifecycleDenialLabel('last_administrator'), '조직의 마지막 관리자는 정지하거나 퇴사 처리할 수 없습니다.')
	assert.equal(lifecycleDenialLabel('terminal_state'), '퇴사 처리된 계정은 읽기 전용입니다.')
	assert.equal(lifecycleDenialLabel('unknown'), '')
})
