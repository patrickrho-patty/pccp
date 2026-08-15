import { useCallback, useEffect, useState } from 'react'

// useFavorites (00-cross-cutting A1) — per-operator, per-entity pinned
// items persisted in localStorage. Pinned items sort first via
// sortPinnedFirst. The operator's browser is the persistence boundary
// (one operator per device profile), matching the plan's "persisted per
// operator" requirement without inventing a server-side identity store.
const storageKey = (entity: string) => `pccp_favorites_${entity}`

export function useFavorites(entity: string) {
  const [favorites, setFavorites] = useState<Set<string>>(new Set())

  useEffect(() => {
    try {
      const raw = localStorage.getItem(storageKey(entity))
      setFavorites(new Set(raw ? (JSON.parse(raw) as string[]) : []))
    } catch {
      setFavorites(new Set())
    }
  }, [entity])

  const toggle = useCallback((id: string) => {
    setFavorites(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      localStorage.setItem(storageKey(entity), JSON.stringify([...next]))
      return next
    })
  }, [entity])

  const isFavorite = useCallback((id: string) => favorites.has(id), [favorites])

  const sortPinnedFirst = useCallback(<T,>(items: T[], keyOf: (item: T) => string): T[] => {
    return [...items].sort((a, b) =>
      Number(favorites.has(keyOf(b))) - Number(favorites.has(keyOf(a)))
    )
  }, [favorites])

  return { favorites, toggle, isFavorite, sortPinnedFirst }
}

// FavoriteStar — compact pin/star control for list rows and detail
// headers. Clicking toggles the favorite without bubbling row clicks.
export function FavoriteStar({ entity, id, onToggle }: {
  entity: string
  id: string
  onToggle?: (id: string, pinned: boolean) => void
}) {
  const { favorites, toggle } = useFavorites(entity)
  const pinned = favorites.has(id)
  return (
    <button
      type="button"
      aria-label={pinned ? '고정 해제 · Unpin' : '고정 · Pin'}
      title={pinned ? '고정 해제 · Unpin' : '고정 · Pin'}
      onClick={e => {
        e.stopPropagation()
        toggle(id)
        onToggle?.(id, !pinned)
      }}
      className={`text-sm leading-none transition-transform hover:scale-110 ${pinned ? 'text-yellow-500' : 'text-gray-300 hover:text-yellow-400'}`}
    >
      {pinned ? '★' : '☆'}
    </button>
  )
}
