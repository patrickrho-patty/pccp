import { useCallback, useEffect, useRef, useState } from 'react'

// useServerTable (00-cross-cutting A4) — shared server-side
// pagination/filter/sort hook. Every list page calls a fetcher that
// queries the API with {page, size, search, filters}; the backend does
// the filtering (each list endpoint accepts ?page=&size=&search= +
// entity filters), so the UI never client-slices full lists.
export type ServerPage<T> = { data: T[]; total: number; page: number; size: number }

export type ServerQuery = {
  page: number
  size: number
  search: string
  filters: Record<string, string>
  sort: string
}

export function useServerTable<T>(
  fetcher: (q: ServerQuery) => Promise<ServerPage<T> | T[]>,
  opts?: {
    initialFilters?: Record<string, string>
    initialSearch?: string
    size?: number
    debounceMs?: number
    sortFields?: string[]
  }
) {
  const size = opts?.size ?? 25
  const [rows, setRows] = useState<T[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState(opts?.initialSearch ?? '')
  const [filters, setFilters] = useState<Record<string, string>>(opts?.initialFilters ?? {})
  const [sort, setSort] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reloadTick, setReloadTick] = useState(0)
  const debounce = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reloadWaiters = useRef<Array<() => void>>([])
  const [debouncedSearch, setDebouncedSearch] = useState(search)

  // Debounce the search input so typing doesn't fire a request per key.
  useEffect(() => {
    if (debounce.current) clearTimeout(debounce.current)
    debounce.current = setTimeout(() => setDebouncedSearch(search), opts?.debounceMs ?? 250)
    return () => { if (debounce.current) clearTimeout(debounce.current) }
  }, [search])

  useEffect(() => { setPage(1) }, [debouncedSearch, filters])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    fetcher({ page, size, search: debouncedSearch, filters, sort })
      .then(result => {
        if (cancelled) return
        if (Array.isArray(result)) {
          setRows(result)
          setTotal(result.length)
        } else {
          setRows(result.data ?? [])
          setTotal(result.total ?? (result.data?.length ?? 0))
        }
      })
      .catch(err => {
        if (cancelled) return
        setError(err?.message || '로드 실패 · Load failed')
      })
      .finally(() => {
        if (cancelled) return
        setLoading(false)
        const waiters = reloadWaiters.current.splice(0)
        waiters.forEach(resolve => resolve())
      })
    return () => { cancelled = true }
  }, [page, size, debouncedSearch, filters, sort, reloadTick])

  const setFilter = useCallback((key: string, value: string) => {
    setFilters(prev => {
      const next = { ...prev }
      if (value) next[key] = value
      else delete next[key]
      return next
    })
  }, [])

  const reload = useCallback(() => new Promise<void>(resolve => {
    reloadWaiters.current.push(resolve)
    setReloadTick(t => t + 1)
  }), [])

  return {
    rows, total, page, setPage, size,
    search, setSearch, filters, setFilter,
    sort, setSort, loading, error, reload,
  }
}

// buildQuery composes the query string the backend list endpoints
// expect (?page=&size=&search=&<filters>). Pages pass this into their
// api fetchers.
export function buildQuery(q: ServerQuery, extra: Record<string, string> = {}) {
  const params = new URLSearchParams()
  params.set('page', String(q.page))
  params.set('size', String(q.size))
  if (q.search) params.set('search', q.search)
  for (const [k, v] of Object.entries({ ...q.filters, ...extra })) {
    if (v) params.set(k, v)
  }
  return params.toString()
}
