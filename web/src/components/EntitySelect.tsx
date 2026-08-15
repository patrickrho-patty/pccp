import { useEffect, useState } from 'react'
import { api } from '../api'

// EntitySelect (00-cross-cutting A6) — replaces per-page hardcoded
// <select>s with entity-backed pickers. Options load from the admin
// API (BusinessUnit, Project, Repository, User, Harness, Catalog model,
// SCM connector, Policy pack) and are cached per entity for the
// session, so repeated opens don't re-fetch.
type EntityKind = 'user' | 'project' | 'repository' | 'business_unit' | 'harness' | 'catalog_model' | 'scm_connector' | 'policy_pack'

const FETCHERS: Record<EntityKind, () => Promise<any[]>> = {
  user: api.listUsers,
  project: api.listProjects,
  repository: api.listRepositories,
  business_unit: api.listBusinessUnits,
  harness: api.listHarnesses,
  catalog_model: api.catalogModels,
  scm_connector: () => api.listConnectors().then(cs => (Array.isArray(cs) ? cs : []).filter(c => (c.kind || c.type || '').includes('scm') || (c.kind || c.type || '') === 'git')),
  policy_pack: () => api.listPolicyPacks ? api.listPolicyPacks() : Promise.resolve([]),
}

const LABELS: Record<EntityKind, (item: any) => string> = {
  user: u => `${u.name_ko || u.name} (${u.email || ''})`,
  project: p => p.name_ko || p.name || p.slug,
  repository: r => r.name,
  business_unit: b => b.name_ko || b.name,
  harness: h => h.harness_id,
  catalog_model: m => m.display_name_ko || m.display_name || m.catalog_model_id,
  scm_connector: c => c.display_name || c.name || c.kind || c.id,
  policy_pack: p => `${p.name || p.name_ko || ''} v${p.version || ''}`.trim(),
}

const IDS: Record<EntityKind, (item: any) => string> = {
  user: u => u.id,
  project: p => p.id,
  repository: r => r.id,
  business_unit: b => b.id,
  harness: h => h.id || h.harness_id,
  catalog_model: m => m.catalog_model_id,
  scm_connector: c => c.id || c.kind,
  policy_pack: p => p.id,
}

const cache: Record<string, any[]> = {}

export function EntitySelect({ entity, value, onChange, allowNone = true, noneLabel = '선택...', placeholder }: {
  entity: EntityKind
  value: string
  onChange: (value: string) => void
  allowNone?: boolean
  noneLabel?: string
  placeholder?: string
}) {
  const [options, setOptions] = useState<any[]>(cache[entity] || [])
  const [loaded, setLoaded] = useState(!!cache[entity])

  useEffect(() => {
    let cancelled = false
    if (cache[entity]) return
    FETCHERS[entity]()
      .then(data => {
        if (cancelled) return
        const list = (Array.isArray(data) ? data : (data as any)?.data || []).filter(Boolean)
        cache[entity] = list
        setOptions(list)
        setLoaded(true)
      })
      .catch(() => { if (!cancelled) setLoaded(true) })
    return () => { cancelled = true }
  }, [entity])

  return (
    <select
      className="input"
      value={value}
      onChange={e => onChange(e.target.value)}
      disabled={!loaded && options.length === 0}
    >
      {allowNone && <option value="">{placeholder || noneLabel}</option>}
      {!loaded && options.length === 0 && <option value="">로딩 중...</option>}
      {options.map(item => (
        <option key={IDS[entity](item) || item.id || JSON.stringify(item)} value={IDS[entity](item)}>
          {LABELS[entity](item)}
        </option>
      ))}
    </select>
  )
}
