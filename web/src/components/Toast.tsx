import { useState, useCallback } from 'react'

interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'info'
}

let toastId = 0
const listeners: ((toasts: Toast[]) => void)[] = []
let currentToasts: Toast[] = []

export function showToast(message: string, type: 'success' | 'error' | 'info' = 'info') {
  const toast: Toast = { id: ++toastId, message, type }
  currentToasts = [...currentToasts, toast]
  listeners.forEach(fn => fn(currentToasts))
  setTimeout(() => {
    currentToasts = currentToasts.filter(t => t.id !== toast.id)
    listeners.forEach(fn => fn(currentToasts))
  }, 4000)
}

export function useToasts() {
  const [toasts, setToasts] = useState<Toast[]>([])
  const update = useCallback((newToasts: Toast[]) => setToasts(newToasts), [])
  if (!listeners.includes(update)) listeners.push(update)
  return toasts
}

export function ToastContainer() {
  const toasts = useToasts()
  const latest = toasts[toasts.length - 1]
  return (
    <>
      {/* PAT-1517: announce async/status results to assistive tech. */}
      <div aria-live="polite" role="status" className="sr-only">
        {latest ? `${latest.message}` : ''}
      </div>
      {toasts.length > 0 && (
        <div className="fixed bottom-4 right-4 z-50 space-y-2">
          {toasts.map(t => (
            <div key={t.id} className={`px-4 py-2.5 rounded-lg shadow-lg text-sm animate-fadeIn max-w-sm ${
              t.type === 'success' ? 'bg-green-600 text-white' :
              t.type === 'error' ? 'bg-red-600 text-white' :
              'bg-gray-800 text-white'
            }`}>
              {t.message}
            </div>
          ))}
        </div>
      )}
    </>
  )
}
