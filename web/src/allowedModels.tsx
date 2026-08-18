// Allowed-model view model (PAT-1491): projects store allowed model
// classes as a serialized JSON string; this module parses, dedupes, and
// resolves each class against the canonical model registry so the UI
// never renders raw JSON or builds a URL from a serialized collection.
import { Link } from 'react-router-dom'

export type AllowedModelState = 'single' | 'many' | 'retired' | 'unknown'

export interface AllowedModelItem {
  id: string            // stable class identifier
  label: string         // canonical Korean display label
  state: AllowedModelState
  to: string            // valid destination (detail page or class filter)
  packageId?: string    // set when state === 'single'
}

export interface AnyModelPackage {
  package_id?: string
  model_id?: string
  name?: string
  name_ko?: string
  family?: string
  entitlement_class?: string
  state?: string // draft, published, deprecated, recalled
}

const CLASS_LABEL_KO: Record<string, string> = {
  code: '코드 생성',
  chat: '대화',
  reasoning: '추론',
  text: '텍스트',
  vision: '비전',
  embed: '임베딩',
  audio: '오디오',
  'enterprise-code': '엔터프라이즈 코드',
}

const RETIRED_STATES = ['deprecated', 'recalled']

// parseAllowedModelClasses accepts whatever the API returned (array from
// the typed view model, or a legacy serialized string) and yields a
// deduped, order-preserving class list. Unknown shapes never leak.
export function parseAllowedModelClasses(value: unknown): string[] {
  let raw: unknown = value
  if (typeof raw === 'string') {
    try { raw = JSON.parse(raw) } catch { return [] }
  }
  if (!Array.isArray(raw)) return []
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of raw) {
    const c = typeof item === 'string' ? item.trim() : ''
    if (!c || seen.has(c)) continue
    seen.add(c)
    out.push(c)
  }
  return out
}

// classLabel returns the canonical Korean label with the raw identifier
// as fallback — the identifier always stays visible in the title.
export function classLabel(cls: string): string {
  return CLASS_LABEL_KO[cls] || cls
}

// modelClassOptions derives the Models-page class dropdown from the same
// canonical label map so the two surfaces cannot diverge (PAT-1491).
export function modelClassOptions() {
  return Object.entries(CLASS_LABEL_KO).map(([value, label]) => ({ value, label }))
}

// resolveAllowedModel maps one class to its destination against the
// registry (ModelPackage list from api.listModels()).
//  - exactly one published package → its detail page
//  - anything else (several matches, only retired matches, no match) →
//    the Models page with an explicit class filter; retired/unknown are
//    additionally labeled. Every destination is a valid URL — a
//    serialized collection never enters a path segment.
export function resolveAllowedModel(cls: string, registry: AnyModelPackage[]): AllowedModelItem {
  const matches = registry.filter(p => p.entitlement_class === cls || p.family === cls)
  const published = matches.filter(p => p.state === 'published')
  if (published.length === 1) {
    return { id: cls, label: classLabel(cls), state: 'single', to: `/models/${encodeURIComponent(published[0].package_id || published[0].model_id || cls)}`, packageId: published[0].package_id }
  }
  if (matches.length > 0 && published.length === 0 && matches.every(p => RETIRED_STATES.includes(p.state || ''))) {
    return { id: cls, label: classLabel(cls), state: 'retired', to: `/models?class=${encodeURIComponent(cls)}` }
  }
  if (matches.length > 0) {
    return { id: cls, label: classLabel(cls), state: 'many', to: `/models?class=${encodeURIComponent(cls)}` }
  }
  return { id: cls, label: classLabel(cls), state: 'unknown', to: `/models?class=${encodeURIComponent(cls)}` }
}

export function resolveAllowedModels(value: unknown, registry: AnyModelPackage[]): AllowedModelItem[] {
  return parseAllowedModelClasses(value).map(c => resolveAllowedModel(c, registry))
}

export const ALLOWED_MODEL_COLLAPSE_AT = 5

// AllowedModelChips renders the resolved items: one valid destination
// per item, retired/unknown labeled, collapsed with an explicit 외 N개
// expansion that keeps every item keyboard accessible.
export function AllowedModelChips({ items }: { items: AllowedModelItem[] }) {
  if (items.length === 0) {
    return <span className="text-xs text-gray-400">제한 없음 · 모든 모델 클래스 허용</span>
  }
  const chip = (m: AllowedModelItem, compact: boolean) => (
    <Link key={m.id} to={m.to} title={m.state === 'unknown' ? `${m.id} (레지스트리에 없음 — 필터로 이동)` : m.state === 'retired' ? `${m.id} (사용중단)` : m.id}
      className={`badge ${compact ? 'text-[10px] px-1.5 py-0.5' : 'text-xs'} ${m.state === 'unknown' || m.state === 'retired'
        ? 'badge-gray border-dashed'
        : 'badge-blue'}`}>
      {m.label}{m.state === 'retired' && !compact ? ' (사용중단)' : ''}{m.state === 'unknown' && !compact ? ' (알 수 없음)' : ''}
    </Link>
  )
  const shown = items.slice(0, ALLOWED_MODEL_COLLAPSE_AT)
  const rest = items.length - shown.length
  return (
    <span className="inline-flex flex-wrap gap-1 items-center">
      {shown.map(m => chip(m, false))}
      {rest > 0 && (
        <details className="inline">
          <summary className="text-xs text-blue-600 cursor-pointer select-none list-none">외 {rest}개</summary>
          <span className="inline-flex flex-wrap gap-1 items-center ml-1">
            {items.slice(ALLOWED_MODEL_COLLAPSE_AT).map(m => chip(m, true))}
          </span>
        </details>
      )}
    </span>
  )
}
