import { ReactNode, useEffect, useId, useRef } from 'react'

// Modal (00-cross-cutting A10) — shared overlay with sticky header,
// scrollable body capped at viewport height, consistent cancel/confirm
// button order (cancel left, primary right), and responsive width.
// Every destructive action routes through ConfirmDialog; this Modal
// covers the form/editor modals. Exposes dialog semantics to assistive
// tech (role/aria-modal/aria-labelledby → the title).
//
// A11y: focus is moved into the dialog on open (initial focus to the
// first focusable child, or the close button if none), trapped inside
// the dialog while open, and restored to the trigger element on close.
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
  const dialogRef = useRef<HTMLDivElement>(null)
  const previouslyFocused = useRef<Element | null>(null)

  // Escape closes; scroll lock while open; focus trap; initial focus;
  // restore focus to the trigger element on close.
  useEffect(() => {
    if (!open) return
    previouslyFocused.current = document.activeElement
    const prevOverflow = document.body.style.overflow
    let isActive = true
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); onClose(); return }
      if (e.key === 'Tab') {
        const root = dialogRef.current
        if (!root) return
        const active = document.activeElement as HTMLElement | null
        if (!root.contains(active as Node)) return
        const focusables = root.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
        )
        if (focusables.length === 0) { e.preventDefault(); return }
        const first = focusables[0]
        const last = focusables[focusables.length - 1]
        if (e.shiftKey && active === first) { e.preventDefault(); last.focus() }
        else if (!e.shiftKey && active === last) { e.preventDefault(); first.focus() }
      }
    }
    window.addEventListener('keydown', handler)
    document.body.style.overflow = 'hidden'
    // Initial focus: first focusable child, falling back to the dialog
    // itself so screen readers announce the title.
    const t = window.setTimeout(() => {
      if (!isActive) return
      const root = dialogRef.current
      if (!root) return
      const focusables = root.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )
      if (focusables.length > 0) focusables[0].focus()
      else root.focus()
    }, 0)
    return () => {
      isActive = false
      window.clearTimeout(t)
      window.removeEventListener('keydown', handler)
      document.body.style.overflow = prevOverflow
      const prev = previouslyFocused.current as HTMLElement | null
      if (prev && typeof prev.focus === 'function' && document.contains(prev)) prev.focus()
    }
  }, [open, onClose])

  if (!open) return null
  const widths: Record<string, string> = { sm: 'max-w-sm', md: 'max-w-md', lg: 'max-w-2xl', xl: 'max-w-4xl' }

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4 animate-fadeIn" onClick={onClose}>
      <div ref={dialogRef} tabIndex={-1} role="dialog" aria-modal="true" aria-labelledby={titleId}
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
