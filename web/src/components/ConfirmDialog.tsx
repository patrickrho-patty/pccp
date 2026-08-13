import { ReactNode } from 'react'

interface ConfirmDialogProps {
  open: boolean
  title: string
  message: string | ReactNode
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export default function ConfirmDialog({
  open, title, message, confirmLabel = '확인', cancelLabel = '취소', danger, onConfirm, onCancel,
}: ConfirmDialogProps) {
  if (!open) return null
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 animate-fadeIn" onClick={onCancel}>
      <div className="bg-white rounded-xl shadow-xl max-w-md w-full mx-4 animate-scaleIn" onClick={e => e.stopPropagation()}>
        <div className="p-5">
          <h3 className={`text-sm font-semibold ${danger ? 'text-red-600' : ''}`}>{title}</h3>
          <p className="text-xs text-gray-500 mt-2">{message}</p>
        </div>
        <div className="flex gap-2 p-4 border-t border-gray-100 justify-end">
          <button onClick={onCancel} className="btn-sm btn-secondary">{cancelLabel}</button>
          <button onClick={onConfirm} className={`btn-sm ${danger ? 'btn-danger' : 'btn-primary'}`}>{confirmLabel}</button>
        </div>
      </div>
    </div>
  )
}
