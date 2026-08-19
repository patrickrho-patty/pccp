// relationLinks.ts — PAT-1490: shared relation-link resolver.
//
// Every entity has ONE canonical detail route and named records must reach
// it; aggregate relationship counts must open a list scoped by the parent
// entity + the exact status the count represents. This module is the single
// source of canonical routes and scoped-queue URLs so no page hand-rolls a
// broad-landing link that loses the parent/status scope.
//
// Scope is always encoded in shareable query params (browser-back safe), and
// the destination list and the source count resolve through the SAME backend
// filter contract, so the visible destination count reconciles with the card.

export type RelationEntity =
  | 'user' | 'harness' | 'project' | 'repository' | 'session'
  | 'model' | 'finding' | 'audit' | 'endpoint' | 'account'

/** Canonical detail route for a named record. */
export function detailRoute(entity: RelationEntity, id: string): string {
  switch (entity) {
    case 'user': return `/users/${id}`
    case 'harness': return `/harnesses/${id}`
    case 'project': return `/projects/${id}`
    case 'repository': return `/repositories/${id}`
    case 'session': return `/sessions/${id}`
    case 'finding': return `/findings/${id}`
    case 'model': return `/models/${id}`
    case 'endpoint': return `/endpoints/${id}`
    default: return `/${entity}/${id}`
  }
}

export interface ListScope {
  /** Parent entity kind whose id scopes the destination list. */
  parent?: { entity: Exclude<RelationEntity, 'finding' | 'audit'>; id: string }
  status?: string
  severity?: string
  tab?: string
}

/**
 * Scoped work-queue URL for an aggregate relationship count. Parent scope is
 * encoded as the backend's filter key per destination surface:
 *  - sessions list:  ?repository= / ?user= / ?harness_id= / ?status=
 *  - findings list:  /security?tab=findings&repository=&severity=&status=
 * The returned path is shareable and reconciles with the count's scope.
 */
export function scopedListRoute(kind: 'sessions' | 'security' | 'audit', scope: ListScope): string {
  const p = new URLSearchParams()
  if (scope.parent) {
    const { entity, id } = scope.parent
    if (kind === 'sessions') {
      // Sessions list backend keys (web/02): repository_id / user / project.
      switch (entity) {
        case 'repository': p.set('repository', id); break
        case 'user': p.set('user', id); break
        case 'project': p.set('project', id); break
        case 'harness': p.set('harness_id', id); break
        case 'session': return detailRoute('session', id)
        default: break
      }
    }
    if (kind === 'security') {
      p.set('tab', 'findings')
      if (entity === 'repository') p.set('repository', id)
      else if (entity === 'session') p.set('session', id)
      else if (entity === 'harness') p.set('harness_id', id)
      else if (entity === 'project') p.set('project', id)
    }
  }
  if (scope.tab && kind === 'security') p.set('tab', scope.tab)
  if (scope.severity) p.set('severity', scope.severity)
  if (scope.status) p.set('status', scope.status)
  const qs = p.toString()
  return qs ? `${kind === 'security' ? '/security' : `/${kind}`}?${qs}` : `/${kind}`
}

/**
 * Preserve "back to the originating tab" on detail pages. Detail routes add
 * ?from=<scopedListUrl> and this reads it so the back link returns exactly to
 * the filtered queue / originating tab rather than a broad landing page.
 */
export function backLink(from?: string | null, fallback = '/'): string {
  return from && from.startsWith('/') ? from : fallback
}
