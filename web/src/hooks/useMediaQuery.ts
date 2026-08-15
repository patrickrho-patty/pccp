import { useEffect, useState } from 'react'

// useMediaQuery (00-cross-cutting A14) — reactive breakpoint state so
// pages can swap table→card layouts on narrow screens.
export function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches)
  useEffect(() => {
    const mql = window.matchMedia(query)
    const handler = (e: MediaQueryListEvent) => setMatches(e.matches)
    setMatches(mql.matches)
    mql.addEventListener('change', handler)
    return () => mql.removeEventListener('change', handler)
  }, [query])
  return matches
}

export const isNarrow = () => window.matchMedia('(max-width: 768px)').matches
