import { useRef } from 'react'
import { useTabParam } from '../hooks/useTabParam'

type TabDef = { id: string; label: string }

/**
 * Shared route-backed tab bar (PAT-1495): selection lives in the URL
 * (?tab=<id>), refresh/back/forward/share restore the exact section.
 * a11y: role=tablist/tab, aria-selected, roving tabIndex, arrow keys.
 * Unknown ?tab values fall back to the default while keeping the URL
 * canonical (the hook omits the param for the default tab).
 */
export function RouteTabs({ tabs, label, size = 'sm' }: {
  tabs: TabDef[]
  label: string
  size?: 'xs' | 'sm'
}) {
  const [tab, setTab] = useTabParam(tabs[0].id, tabs.map(t => t.id)) as [string, (t: string) => void]
  const refs = useRef<(HTMLButtonElement | null)[]>([])

  const onKeyDown = (e: React.KeyboardEvent, i: number) => {
    let next = -1
    if (e.key === 'ArrowRight') next = (i + 1) % tabs.length
    if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length
    if (next < 0) return
    e.preventDefault()
    setTab(tabs[next].id)
    refs.current[next]?.focus()
  }

  const pad = size === 'xs' ? 'px-3 py-2 text-xs' : 'px-4 py-2 text-sm'

  return (
    <div className={`flex gap-1 mb-6 border-b border-gray-200 flex-wrap`} role="tablist" aria-label={label}>
      {tabs.map((t, i) => (
        <button
          key={t.id}
          ref={el => { refs.current[i] = el }}
          role="tab"
          aria-selected={tab === t.id}
          tabIndex={tab === t.id ? 0 : -1}
          onKeyDown={e => onKeyDown(e, i)}
          onClick={() => setTab(t.id)}
          className={`${pad} font-medium border-b-2 transition-colors whitespace-nowrap ${
            tab === t.id
              ? 'border-blue-600 text-blue-600 font-semibold'
              : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
          {t.label}
        </button>
      ))}
    </div>
  )
}

/** Hook form for pages that need the active tab value for content sections. */
export function useRouteTabs(defaultTab: string, validTabs: string[]) {
  return useTabParam(defaultTab, validTabs)
}
