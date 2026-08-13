import { useState, useCallback, createContext, useContext, ReactNode } from 'react'
import ConfirmDialog from './ConfirmDialog'

type ConfirmOptions = {
  title: string
  message: string
  confirmLabel?: string
  danger?: boolean
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>

const ConfirmContext = createContext<ConfirmFn>(() => Promise.resolve(false))

export function useConfirm() {
  return useContext(ConfirmContext)
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{
    open: boolean
    opts: ConfirmOptions
    resolve?: (v: boolean) => void
  }>({ open: false, opts: { title: '', message: '' } })

  const confirm = useCallback((opts: ConfirmOptions) => {
    return new Promise<boolean>(resolve => {
      setState({ open: true, opts, resolve })
    })
  }, [])

  const handleClose = () => {
    state.resolve?.(false)
    setState(s => ({ ...s, open: false }))
  }

  const handleConfirm = () => {
    state.resolve?.(true)
    setState(s => ({ ...s, open: false }))
  }

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      <ConfirmDialog
        open={state.open}
        title={state.opts.title}
        message={state.opts.message}
        confirmLabel={state.opts.confirmLabel || '확인'}
        danger={state.opts.danger ?? true}
        onConfirm={handleConfirm}
        onCancel={handleClose}
      />
    </ConfirmContext.Provider>
  )
}
