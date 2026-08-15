import { ReactNode } from 'react'
import { useMediaQuery } from '../hooks/useMediaQuery'

// ResponsiveTable (00-cross-cutting A14) — table→card breakpoint.
// On md+ screens renders a dense enterprise table; below md each row
// becomes a card with label/value pairs. `expand` renders the
// expanded-row detail on desktop and is appended to the card on mobile.
export type Column<T> = {
  key: string
  header: string
  render: (row: T) => ReactNode
  cardLabel?: string
  className?: string
  onClick?: (row: T) => void
}

export function ResponsiveTable<T extends { id?: string }>({ columns, rows, rowKey, expand, empty }: {
  columns: Column<T>[]
  rows: T[]
  rowKey: (row: T, index: number) => string
  expand?: (row: T) => ReactNode
  empty?: ReactNode
}) {
  const isDesktop = useMediaQuery('(min-width: 768px)')

  if (rows.length === 0 && empty) return <>{empty}</>

  if (!isDesktop) {
    return (
      <div className="space-y-3 p-3">
        {rows.map((row, i) => (
          <div key={rowKey(row, i)} className="card !p-3 space-y-1.5">
            {columns.map(col => (
              <div key={col.key} className="flex items-start justify-between gap-3 text-xs">
                <span className="text-gray-400 flex-shrink-0">{col.cardLabel || col.header}</span>
                <span className="text-right break-all">{col.render(row)}</span>
              </div>
            ))}
            {expand && <div className="pt-2 border-t border-gray-100">{expand(row)}</div>}
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className="table-responsive">
      <table className="w-full min-w-[700px]">
        <thead>
          <tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
            {columns.map(col => <th key={col.key} className={`pb-3 ${col.className || ''}`}>{col.header}</th>)}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <FragmentRow key={rowKey(row, i)} row={row} columns={columns} expand={expand} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

import { Fragment } from 'react'
function FragmentRow<T extends { id?: string }>({ row, columns, expand }: {
  row: T
  columns: Column<T>[]
  expand?: (row: T) => ReactNode
}) {
  return (
    <Fragment>
      <tr className="border-b border-gray-100 last:border-0 hover:bg-gray-50 transition-colors row-hover">
        {columns.map(col => (
          <td key={col.key} className={`py-3 ${col.className || ''}`} onClick={col.onClick ? () => col.onClick!(row) : undefined}>
            {col.render(row)}
          </td>
        ))}
      </tr>
      {expand && (
        <tr className="bg-gray-50 border-b border-gray-100 expand-row">
          <td colSpan={columns.length} className="p-4">{expand(row)}</td>
        </tr>
      )}
    </Fragment>
  )
}
