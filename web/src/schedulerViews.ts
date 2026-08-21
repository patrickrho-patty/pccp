// schedulerViews.ts — pure view-model helpers for the PAT-1445 scheduler
// traffic-director panels (KV directory, P/D capacity, programs, shadow).
// Kept React-free so the shaping logic is node-testable.

export interface KVTierRow {
  tier: string
  locations: number
}

export interface KVDirViewModel {
  extents: number
  verified: number
  unverified: number
  tiers: KVTierRow[]
  hotPrefixes: Array<{ hash: string; hits: number; replicas: number; tokens: number }>
}

const TIER_ORDER = ['L1-hbm', 'L2-host', 'L3-disk', 'L4-remote']

export function kvDirViewModel(raw: any): KVDirViewModel {
  const byTier = (raw?.by_tier ?? {}) as Record<string, number>
  const tiers = Object.keys(byTier)
    .map((tier) => ({ tier, locations: byTier[tier] }))
    .sort((a, b) => {
      const ai = TIER_ORDER.indexOf(a.tier)
      const bi = TIER_ORDER.indexOf(b.tier)
      return (ai < 0 ? 99 : ai) - (bi < 0 ? 99 : bi) || a.tier.localeCompare(b.tier)
    })
  const hot = Array.isArray(raw?.hot_prefixes) ? raw.hot_prefixes : []
  return {
    extents: raw?.extents ?? 0,
    verified: raw?.locations_verified ?? 0,
    unverified: raw?.locations_unverified ?? 0,
    tiers,
    hotPrefixes: hot.map((h: any) => ({
      hash: String(h?.hash ?? ''),
      hits: h?.hits ?? 0,
      replicas: h?.replicas ?? 0,
      tokens: h?.tokens ?? 0,
    })),
  }
}

export interface PDRow {
  model: string
  prefillShare: number
  engaged: boolean
  prefill: number
  decode: number
  aggregated: number
  imbalance: boolean
}

export function pdRows(raw: any): PDRow[] {
  const rows = Array.isArray(raw) ? raw : []
  return rows
    .map((m: any) => {
      const engaged = !!m?.disaggregation_engaged
      const prefill = m?.prefill_workers ?? 0
      const decode = m?.decode_workers ?? 0
      const aggregated = m?.aggregated_workers ?? 0
      // An engaged model with no dedicated capacity on one side is
      // imbalanced; aggregated-only fleets are fine (co-located default).
      const imbalance = engaged && aggregated === 0 && (prefill === 0 || decode === 0)
      return {
        model: String(m?.model ?? ''),
        prefillShare: m?.prefill_share ?? 0,
        engaged,
        prefill,
        decode,
        aggregated,
        imbalance,
      }
    })
    .sort((a, b) => Number(b.imbalance) - Number(a.imbalance) || b.prefillShare - a.prefillShare)
}

export interface ProgramsViewModel {
  programs: number
  toolPaused: number
  predictionErrors: number
  turns: number
}

export function programsViewModel(raw: any): ProgramsViewModel {
  return {
    programs: raw?.programs ?? 0,
    toolPaused: raw?.tool_paused ?? 0,
    predictionErrors: raw?.pause_prediction_errors ?? 0,
    turns: raw?.turns ?? 0,
  }
}

export interface ShadowViewModel {
  receipts: number
  shadowed: number
  agreementPct: number | null
  filtered: Array<{ reason: string; count: number }>
  canary: { capability: string; state: string; active: boolean } | null
}

export function shadowViewModel(raw: any): ShadowViewModel {
  const shadowed = raw?.shadowed ?? 0
  const agreementPct = shadowed > 0 ? Math.round((raw?.agreement_rate ?? 0) * 1000) / 10 : null
  const filtered = Object.entries((raw?.filtered ?? {}) as Record<string, number>)
    .map(([reason, count]) => ({ reason, count }))
    .sort((a, b) => b.count - a.count)
  const canary = raw?.canary
    ? {
        capability: String(raw.canary.capability ?? ''),
        state: String(raw.canary.state ?? ''),
        active: !!raw.canary.active,
      }
    : null
  return {
    receipts: raw?.receipts ?? 0,
    shadowed,
    agreementPct,
    filtered,
    canary,
  }
}
