// a11yLogic.ts — PAT-1517: pure, testable accessibility navigation logic.
//
// Kept separate from a11y.tsx (components) so node:test can import it
// without JSX. Mirrors the keyboard behavior of the AccessibleTabs
// primitive.

/** Pure arrow/Home/End tab navigation index (mirrors AccessibleTabs). */
export function tabNavNextIndex(key: string, current: number, length: number): number {
  if (length <= 0) return 0
  if (key === 'ArrowRight') return (current + 1) % length
  if (key === 'ArrowLeft') return (current - 1 + length) % length
  if (key === 'Home') return 0
  if (key === 'End') return length - 1
  return current
}
