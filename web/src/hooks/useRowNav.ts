import { useEffect, useState, useCallback } from 'react'

// useRowNav provides j/k/enter keyboard navigation for table rows.
// Pressing j/k moves the selection up/down, Enter triggers onActivate.
// The hook returns { selectedIndex, setSelectedIndex } to highlight the active row.
export function useRowNav(totalRows: number, onActivate: (index: number) => void, enabled: boolean = true) {
  const [selectedIndex, setSelectedIndex] = useState(-1)

  const handleKey = useCallback((e: KeyboardEvent) => {
    if (!enabled) return
    // Don't interfere when typing in inputs
    const tag = (e.target as HTMLElement)?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return

    if (e.key === 'j' || e.key === 'ArrowDown') {
      e.preventDefault()
      setSelectedIndex(i => Math.min(i + 1, totalRows - 1))
    } else if (e.key === 'k' || e.key === 'ArrowUp') {
      e.preventDefault()
      setSelectedIndex(i => Math.max(i - 1, 0))
    } else if (e.key === 'Enter' && selectedIndex >= 0) {
      e.preventDefault()
      onActivate(selectedIndex)
    }
  }, [enabled, totalRows, selectedIndex, onActivate])

  useEffect(() => {
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [handleKey])

  return { selectedIndex, setSelectedIndex }
}
