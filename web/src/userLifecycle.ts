// Canonical user lifecycle state machine (PAT-1489) — the web mirror of
// userLifecycleTransitions in internal/api/users_lifecycle.go. List rows,
// detail headers, status badges, and confirmations all derive from this
// single module so no surface can disagree about what a valid next move is.
import { api } from './api'

export type UserLifecycleAction = 'suspend' | 'resume' | 'offboard'

export interface UserActionSpec {
  action: UserLifecycleAction
  label: string
  title: string
  danger: boolean
  effect: string
}

export const STATUS_KO: Record<string, string> = { active: '활성', suspended: '정지', offboarded: '퇴사' }
export const STATUS_BADGE: Record<string, string> = {
  active: 'bg-green-50 text-green-700 border-green-200',
  suspended: 'bg-amber-50 text-amber-700 border-amber-200',
  offboarded: 'bg-gray-100 text-gray-500 border-gray-200',
}

const TRANSITIONS: Record<string, UserLifecycleAction[]> = {
  active: ['suspend', 'offboard'],
  suspended: ['resume', 'offboard'],
  offboarded: [], // terminal — read-only history, no actions
}

const SPECS: Record<UserLifecycleAction, UserActionSpec> = {
  suspend: {
    action: 'suspend', label: '정지', title: '계정 정지', danger: true,
    effect: '계정이 정지 상태로 전환되며, 재활성화 전까지 관리 대상에서 정지 상태로 유지됩니다.',
  },
  resume: {
    action: 'resume', label: '재활성화', title: '계정 재활성화', danger: false,
    effect: '정지가 해제되고 계정이 활성 상태로 복원됩니다.',
  },
  offboard: {
    action: 'offboard', label: '퇴사', title: '퇴사 처리', danger: true,
    effect: '퇴사 처리 시 모든 세션이 종료되고 하네스 바인딩이 해제됩니다.',
  },
}

// Precomputed per-status action lists — static data, zero per-render allocation.
const ACTIONS_BY_STATUS: Record<string, UserActionSpec[]> = Object.fromEntries(
  Object.entries(TRANSITIONS).map(([status, actions]) => [status, actions.map(a => SPECS[a])])
)

// userActions returns the valid next actions for a current account state.
export function userActions(status: string): UserActionSpec[] {
  return ACTIONS_BY_STATUS[status] || []
}

// userActionSpec returns the shared spec (title, label, tone) for one action.
export function userActionSpec(action: UserLifecycleAction): UserActionSpec {
  return SPECS[action]
}

// canIssueEnrollment — enrollment codes grant access, so only active
// accounts are eligible.
export function canIssueEnrollment(status: string): boolean {
  return status === 'active'
}

// applyUserLifecycle dispatches one lifecycle mutation through its
// dedicated endpoint. Both list and detail surfaces call this — never
// api.updateUser — so status changes cannot bypass the state machine.
export async function applyUserLifecycle(action: UserLifecycleAction, id: string, reason: string) {
  if (action === 'offboard') return api.offboardUser(id, reason)
  if (action === 'suspend') return api.suspendUser(id, reason)
  return api.resumeUser(id, reason)
}
