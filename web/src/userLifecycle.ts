// User lifecycle presentation (PAT-1489). The server supplies allowed_actions
// from the canonical model state machine after applying operator RBAC and
// self-action rules; this module only maps those action names to Korean UI copy.
import { api } from './api'
import type { UserLifecycleAction } from './userLifecycleView'

export * from './userLifecycleView'

// applyUserLifecycle dispatches one lifecycle mutation through its
// dedicated endpoint. Both list and detail surfaces call this — never
// api.updateUser — so status changes cannot bypass the state machine.
export async function applyUserLifecycle(action: UserLifecycleAction, id: string, reason: string) {
  if (action === 'offboard') return api.offboardUser(id, reason)
  if (action === 'suspend') return api.suspendUser(id, reason)
  return api.resumeUser(id, reason)
}
