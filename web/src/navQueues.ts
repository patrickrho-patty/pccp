// navQueues.ts — PAT-1518: exact nav-queue count → destination contract.
//
// A nav badge must only appear for queues with an exact, consistently scoped
// destination, and it must open that exact filtered queue. This module is the
// single source of that mapping (shared with Layout): every path's badge is
// backed by the canonical dashboard metric (PAT-1487/1488) and a scoped href.

export interface NavQueueSpec {
  path: string
  metric: string            // dashboard field that feeds the count
  href: string              // exact scoped destination the badge opens
  severity?: 'critical' | 'warning' | 'neutral' // color is never the only signal
}

/** Actionable nav que   ues — only exact-scoped destinations. */
export const NAV_QUEUES: NavQueueSpec[] = [
  { path: '/security', metric: 'open_critical_findings', href: '/security?tab=findings&severity=critical,high&status=unresolved', severity: 'critical' },
  { path: '/compliance', metric: 'open_remediations', href: '/compliance?tab=remediation&status=unresolved', severity: 'warning' },
  { path: '/tools', metric: 'pending_approvals', href: '/tools?tab=approvals', severity: 'neutral' },
  { path: '/harnesses', metric: 'quarantined_harnesses', href: '/fleet', severity: 'neutral' },
]

export function navQueueFor(path: string): NavQueueSpec | undefined {
  return NAV_QUEUES.find(q => q.path === path)
}

/** Build the counts map from a dashboard payload (unknown metrics → 0). */
export function navCountsFromDashboard(dash: Record<string, any>): Record<string, number> {
  const out: Record<string, number> = {}
  for (const q of NAV_QUEUES) {
    const v = dash[q.metric]
    out[q.path] = typeof v === 'number' && v > 0 ? v : 0
  }
  return out
}

/** Severity tint (color + label so color is never the only signal). */
export function navSeverityTint(severity?: string): string {
  if (severity === 'critical') return 'bg-red-500'
  if (severity === 'warning') return 'bg-amber-500'
  return 'bg-gray-600'
}
