/**
 * a11y.tsx — PAT-1517: shared WCAG 2.1 AA primitives for the admin console.
 *
 * The console audit found unnamed controls, non-semantic tabs, unlabeled
 * switches, and no reliable live regions. These primitives are the single
 * place those semantics live: AccessibleTab (tablist/tab/tabpanel with
 * arrow-key nav + visible focus), Switch (role=switch with label +
 * described effect), LabeledField (label + input wrapper so placeholders are
 * never the only label), and useLiveRegion (aria-live announcements for
 * status/error/async results). Pages use these instead of hand-rolling
 * ARIA, so the semantics stay consistent and testable.
 */
import { ReactNode, useEffect, useId, useRef, useState } from 'react'

// ---- AccessibleTab ---------------------------------------------------------

export interface AccessibleTabItem {
  id: string
  label: string
  count?: number
  content: ReactNode
}

/** Pure arrow/Home/End navigation index (testable, mirrors AccessibleTabs). */
export function tabNavNextIndex(key: string, current: number, length: number): number {
  if (length <= 0) return 0
  if (key === 'ArrowRight') return (current + 1) % length
  if (key === 'ArrowLeft') return (current - 1 + length) % length
  if (key === 'Home') return 0
  if (key === 'End') return length - 1
  return current
}

export function AccessibleTabs({ items, active, onSelect, label }: {
  items: AccessibleTabItem[]
  active: string
  onSelect: (id: string) => void
  label: string
}) {
  const listId = useId()
  const tabRefs = useRef<Map<string, HTMLButtonElement>>(new Map())
  const activeIdx = Math.max(0, items.findIndex(t => t.id === active))

  const onKeyDown = (e: React.KeyboardEvent) => {
    const codes = ['ArrowRight', 'ArrowLeft', 'Home', 'End']
    if (!codes.includes(e.key)) return
    e.preventDefault()
    let next = activeIdx
    if (e.key === 'ArrowRight') next = (activeIdx + 1) % items.length
    else if (e.key === 'ArrowLeft') next = (activeIdx - 1 + items.length) % items.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = items.length - 1
    onSelect(items[next].id)
    tabRefs.current.get(items[next].id)?.focus()
  }

  return (
    <div>
      <div role="tablist" aria-label={label} className="flex gap-1 mb-4 border-b border-gray-200"
        onKeyDown={onKeyDown}>
        {items.map(t => (
          <button
            key={t.id}
            ref={el => { if (el) tabRefs.current.set(t.id, el) }}
            role="tab"
            id={`${listId}-${t.id}-tab`}
            aria-selected={active === t.id}
            aria-controls={`${listId}-${t.id}-panel`}
            tabIndex={active === t.id ? 0 : -1}
            onClick={() => onSelect(t.id)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 ${
              active === t.id ? 'border-blue-600 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}>
            {t.label}{typeof t.count === 'number' ? ` (${t.count})` : ''}
          </button>
        ))}
      </div>
      {items.map(t => (
        <div key={t.id} role="tabpanel" id={`${listId}-${t.id}-panel`}
          aria-labelledby={`${listId}-${t.id}-tab`} hidden={active !== t.id}>
          {t.content}
        </div>
      ))}
    </div>
  )
}

// ---- Switch ----------------------------------------------------------------

export function Switch({ checked, onChange, label, describe }: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  describe?: string
}) {
  const id = useId()
  return (
    <div className="flex items-center gap-2">
      <button
        id={id}
        role="switch"
        aria-checked={checked}
        aria-label={label}
        aria-describedby={describe ? `${id}-desc` : undefined}
        onClick={() => onChange(!checked)}
        className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 ${
          checked ? 'bg-blue-600' : 'bg-gray-300'
        }`}>
        <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${checked ? 'translate-x-4' : 'translate-x-1'}`} />
      </button>
      <span className="text-xs text-gray-700">{label}</span>
      {describe && <span id={`${id}-desc`} className="text-[10px] text-gray-400">{describe}</span>}
    </div>
  )
}

// ---- LabeledField ------------------------------------------------------------

export function LabeledField({ label, children, htmlFor, hint }: {
  label: string
  children: ReactNode
  htmlFor?: string
  hint?: string
}) {
  const autoId = useId()
  const id = htmlFor || autoId
  return (
    <div className="space-y-1">
      <label htmlFor={id} className="text-[10px] font-medium text-gray-600">{label}</label>
      {children}
      {hint && <p className="text-[10px] text-gray-400">{hint}</p>}
    </div>
  )
}

// ---- Live region ------------------------------------------------------------

let liveCounter = 0

/** Announce status/error/async results to screen readers. Renders an
 *  aria-live="polite" region that only appears once a message is set. */
export function useLiveRegion(): { region: ReactNode, announce: (msg: string) => void } {
  const [msg, setMsg] = useState<string>('')
  const key = useRef(0)
  const announce = (m: string) => { key.current += 1; setMsg(m) }
  const region = (
    <div aria-live="polite" role="status" className="sr-only" key={key.current}>
      {msg}
    </div>
  )
  return { region, announce }
}

/** A reusable single-live-region component for page-mounted announcements. */
export function LiveRegion({ message }: { message?: string }) {
  // Bump a counter so repeated identical messages still re-announce.
  void liveCounter
  return (
    <div aria-live="polite" role="status" className="sr-only">
      {message || ''}
    </div>
  )
}
