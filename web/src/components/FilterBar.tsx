import { useState, useMemo } from 'react'

// Common filter types
export type FilterConfig = {
  // Text search fields
  searchFields?: string[]
  searchPlaceholder?: string
  // Date range
  dateField?: string
  // Dropdown filters
  dropdowns?: {
    key: string
    label: string
    options: { value: string; label: string }[]
    defaultValue?: string
  }[]
}

// Reusable filter bar component
export function FilterBar({ config, onChange }: {
  config: FilterConfig
  onChange: (filters: {
    search: string
    dateFrom: string
    dateTo: string
    dropdowns: Record<string, string>
  }) => void
}) {
  const [search, setSearch] = useState('')
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  const [dropdownValues, setDropdownValues] = useState<Record<string, string>>(
    Object.fromEntries(
      (config.dropdowns || []).map(d => [d.key, d.defaultValue || ''])
    )
  )

  const emitChange = (updates: Partial<{
    search: string
    dateFrom: string
    dateTo: string
    dropdowns: Record<string, string>
  }>) => {
    const finalSearch = updates.search !== undefined ? updates.search : search
    const finalDateFrom = updates.dateFrom !== undefined ? updates.dateFrom : dateFrom
    const finalDateTo = updates.dateTo !== undefined ? updates.dateTo : dateTo
    const finalDropdowns = updates.dropdowns || dropdownValues
    onChange({
      search: finalSearch,
      dateFrom: finalDateFrom,
      dateTo: finalDateTo,
      dropdowns: finalDropdowns,
    })
  }

  return (
    <div className="flex flex-wrap items-center gap-2 mb-4">
      {/* Search */}
      {config.searchFields && (
        <input
          type="text"
          className="input flex-1 min-w-[200px]"
          placeholder={config.searchPlaceholder || '검색...'}
          value={search}
          onChange={e => {
            setSearch(e.target.value)
            emitChange({ search: e.target.value })
          }}
        />
      )}

      {/* Date range */}
      {config.dateField && (
        <>
          <div className="flex items-center gap-1">
            <input
              type="date"
              className="input max-w-[150px] text-xs"
              value={dateFrom}
              onChange={e => {
                setDateFrom(e.target.value)
                emitChange({ dateFrom: e.target.value })
              }}
            />
            <span className="text-xs text-gray-400">~</span>
            <input
              type="date"
              className="input max-w-[150px] text-xs"
              value={dateTo}
              onChange={e => {
                setDateTo(e.target.value)
                emitChange({ dateTo: e.target.value })
              }}
            />
          </div>
        </>
      )}

      {/* Dropdown filters */}
      {(config.dropdowns || []).map(dd => (
        <select
          key={dd.key}
          className="input max-w-[140px] text-xs"
          value={dropdownValues[dd.key] || ''}
          onChange={e => {
            const newVals = { ...dropdownValues, [dd.key]: e.target.value }
            setDropdownValues(newVals)
            emitChange({ dropdowns: newVals })
          }}
        >
          <option value="">{dd.label}: 전체</option>
          {dd.options.map(opt => (
            <option key={opt.value} value={opt.value}>{opt.label}</option>
          ))}
        </select>
      ))}
    </div>
  )
}

// Hook that applies filters to data
export function useFilteredData<T extends Record<string, any>>(
  data: T[],
  filters: {
    search: string
    dateFrom: string
    dateTo: string
    dropdowns: Record<string, string>
  },
  config: FilterConfig
): T[] {
  return useMemo(() => {
    let result = [...data]

    // Text search
    if (filters.search && config.searchFields) {
      const q = filters.search.toLowerCase()
      result = result.filter(item =>
        config.searchFields!.some(field => {
          const val = item[field]
          return val && String(val).toLowerCase().includes(q)
        })
      )
    }

    // Date range filter
    if (config.dateField && (filters.dateFrom || filters.dateTo)) {
      result = result.filter(item => {
        const dateStr = item[config.dateField!]
        if (!dateStr) return false
        const date = dateStr.slice(0, 10) // YYYY-MM-DD
        if (filters.dateFrom && date < filters.dateFrom) return false
        if (filters.dateTo && date > filters.dateTo) return false
        return true
      })
    }

    // Dropdown filters
    Object.entries(filters.dropdowns).forEach(([key, value]) => {
      if (value) {
        result = result.filter(item => String(item[key] || '') === value)
      }
    })

    return result
  }, [data, filters, config])
}

// Pagination component
export function Pagination({ total, page, pageSize, onPageChange }: {
  total: number
  page: number
  pageSize: number
  onPageChange: (page: number) => void
}) {
  const totalPages = Math.ceil(total / pageSize)
  if (totalPages <= 1) return null

  const start = (page - 1) * pageSize + 1
  const end = Math.min(page * pageSize, total)

  return (
    <div className="flex items-center justify-between mt-4 text-xs text-gray-500">
      <span>{start}-{end} / {total}건</span>
      <div className="flex gap-1">
        <button
          onClick={() => onPageChange(page - 1)}
          disabled={page === 1}
          className="btn-sm btn-secondary"
        >
          이전
        </button>
        <span className="px-2 py-1">
          {page} / {totalPages}
        </span>
        <button
          onClick={() => onPageChange(page + 1)}
          disabled={page >= totalPages}
          className="btn-sm btn-secondary"
        >
          다음
        </button>
      </div>
    </div>
  )
}
