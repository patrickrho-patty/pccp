import { ReactNode, useEffect, useId } from 'react'

// Modal (00-cross-cutting A10) — shared overlay with sticky header,
// scrollable body capped at viewport height, consistent cancel/confirm
// button order (cancel left, primary right), and responsive width.
// Every destructive action routes through ConfirmDialog; this Modal
// covers the form/editor modals. Exposes dialog semantics to assistive
// tech (role/aria-modal/aria-labelledby → the title).
export function Modal({ open, title, subtitle, onClose, children, footer, size = 'md' }: {
  open: boolean
  title: string
  subtitle?: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl'
}) {
  const titleId = useId()
  // Escape closes; scroll lock while open.
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', handler)
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', handler)
      document.body.style.overflow = ''
    }
  }, [open, onClose])

  if (!open) return null
  const widths: Record<string, string> = { sm: 'max-w-sm', md: 'max-w-md', lg: 'max-w-2xl', xl: 'max-w-4xl' }

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4 animate-fadeIn" onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-labelledby={titleId}
        className={`bg-white rounded-xl shadow-xl w-full ${widths[size] || widths.md} flex flex-col max-h-[85vh] animate-scaleIn`} onClick={e => e.stopPropagation()}>
        <div className="px-5 py-4 border-b border-gray-100 flex items-start justify-between gap-4 flex-shrink-0 sticky top-0 bg-white rounded-t-xl">
          <div>
            <h3 id={titleId} className="text-sm font-semibold">{title}</h3>
            {subtitle && <p className="text-xs text-gray-500 mt-0.5">{subtitle}</p>}
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none" aria-label="닫기 · Close">✕</button>
        </div>
        <div className="px-5 py-4 overflow-y-auto flex-1">{children}</div>
        {footer && (
          <div className="flex gap-2 px-5 py-4 border-t border-gray-100 justify-end flex-shrink-0 bg-white rounded-b-xl">
            {footer}
          </div>
        )}
      </div>
    </div>
  )
}

// ModalFooter — consistent button order: cancel (secondary) left,
// primary action right (00 A10).
export function ModalFooter({ onCancel, onConfirm, confirmLabel, danger, disabled, cancelLabel = '취소' }: {
  onCancel: () => void
  onConfirm: () => void
  confirmLabel: string
  danger?: boolean
  disabled?: boolean
  cancelLabel?: string
}) {
  return (
    <>
      <button onClick={onCancel} className="btn-sm btn-secondary">{cancelLabel}</button>
      <button onClick={onConfirm} disabled={disabled} className={`btn-sm ${danger ? 'btn-danger' : 'btn-primary'}`}>{confirmLabel}</button>
    </>
  )
}
