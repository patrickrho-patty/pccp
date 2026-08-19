import { useSearchParams } from 'react-router-dom'

export function useTabParam(defaultTab: string, validTabs: string[]) {
  const [searchParams, setSearchParams] = useSearchParams()
  const valid = new Set(validTabs)
  const raw = searchParams.get('tab')
  const tab = raw && valid.has(raw) ? raw : defaultTab
  const setTab = (t: string) => setSearchParams(prev => {
    const next = new URLSearchParams(prev)
    if (t === defaultTab) next.delete('tab')
    else next.set('tab', t)
    return next
  }, { replace: true })
  return [tab, setTab] as const
}
