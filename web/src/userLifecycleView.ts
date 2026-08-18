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

const DENIAL_LABELS: Record<string, string> = {
  insufficient_role: '사용자 수명주기를 변경할 관리자 권한이 없습니다.',
  self_action: '자신의 계정 상태는 직접 변경할 수 없습니다.',
  last_administrator: '조직의 마지막 관리자는 정지하거나 퇴사 처리할 수 없습니다.',
  terminal_state: '퇴사 처리된 계정은 읽기 전용입니다.',
}

export function lifecycleDenialLabel(reason: unknown): string {
  return typeof reason === 'string' ? DENIAL_LABELS[reason] || '' : ''
}

const SPECS: Record<UserLifecycleAction, UserActionSpec> = {
  suspend: {
    action: 'suspend', label: '정지', title: '계정 정지', danger: true,
    effect: '활성 세션과 권한 임대가 즉시 종료되며, 재활성화 전까지 새 작업을 시작할 수 없습니다.',
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

export function userActions(allowedActions: unknown): UserActionSpec[] {
  if (!Array.isArray(allowedActions)) return []
  return allowedActions.filter((action): action is UserLifecycleAction => action in SPECS).map(action => SPECS[action])
}

export function userActionSpec(action: UserLifecycleAction): UserActionSpec {
  return SPECS[action]
}

export function canIssueEnrollment(status: string, canManage = false): boolean {
  return canManage && status === 'active'
}
