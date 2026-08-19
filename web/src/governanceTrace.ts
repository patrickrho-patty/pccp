// Governance trace — server-state mirror: parses the persisted `config`
// JSON, derives the current scope + rollout history, and evaluates harness
// applicability against the catalog. Used by the changeset validator and
// by the page UI to render scope, head epoch, and per-harness results.

import {
  defaultScope,
  isHarnessOnlineNow,
  versionAtLeast,
  type CatalogEntry,
  type FeatureLike,
  type Governance,
  type HarnessEval,
  type HarnessInfo,
  type RolloutRecord,
  type Scope,
} from './enterpriseCatalog.ts'

export function parseGovernance(config: string | undefined | null): Governance {
  const fallback: Governance = { scope: defaultScope(), rollouts: [] }
  if (!config) return fallback
  try {
    const parsed = JSON.parse(config)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return fallback
    const scope = parsed.scope && typeof parsed.scope === 'object'
      ? {
          type: parsed.scope.type === 'selected' ? 'selected' as const : 'org' as const,
          harness_ids: Array.isArray(parsed.scope.harness_ids) ? parsed.scope.harness_ids.filter((x: unknown) => typeof x === 'string') : [],
          exceptions: Array.isArray(parsed.scope.exceptions) ? parsed.scope.exceptions.filter((x: unknown) => typeof x === 'string') : [],
        }
      : defaultScope()
    const rollouts = Array.isArray(parsed.rollouts)
      ? parsed.rollouts.filter((r: unknown) => r && typeof r === 'object' && typeof (r as RolloutRecord).epoch === 'number')
      : []
    return { scope, rollouts }
  } catch {
    return fallback
  }
}

export function headEpoch(g: Governance): number {
  return g.rollouts.length === 0 ? 0 : Math.max(...g.rollouts.map(r => r.epoch))
}

export function headEpochOf(config: string | undefined | null): number {
  return headEpoch(parseGovernance(config))
}

export function scopeHarnessIds(scope: Scope, allHarnessIds: string[]): string[] {
  if (scope.type === 'selected') return scope.harness_ids.filter(id => !scope.exceptions.includes(id))
  return allHarnessIds.filter(id => !scope.exceptions.includes(id))
}

// Per-harness applicability for a scope: exact harness, online, and
// version-compatible against the catalog requirement.
export function evaluateHarnesses(entry: CatalogEntry, harnesses: HarnessInfo[], scope: Scope, now: number): HarnessEval[] {
  const ids = scopeHarnessIds(scope, harnesses.map(h => h.harness_id))
  const byId = new Map(harnesses.map(h => [h.harness_id, h]))
  return ids.map(id => {
    const h = byId.get(id)
    const online = h ? isHarnessOnlineNow(h, now) : false
    const compatible = h ? versionAtLeast(h.binary_version, entry.minHarnessVersion) : false
    return {
        harness_id: id,
        name: h?.name || id,
        version: h?.binary_version || '알 수 없음',
        online,
        compatible,
        result: !online ? '오프라인 대기' : !compatible ? '버전 미충족' : '적용 가능',
      }
  })
}

// Pull in HarnessEval for convenience re-export.
export type { HarnessEval, HarnessInfo, Scope, Governance, RolloutRecord, CatalogEntry, FeatureLike }