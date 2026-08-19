// GovernedActionModal — shared shell for "this mutation requires a reason
// and may require an explicit confirm" modals. Each wave-A admin surface
// (tool gate, tool allowlist, broadcast send, feature toggle, sandbox
// destroy) repeats the same skeleton: title/subtitle, warning banners,
// page-supplied preview node, reason input, optional confirm checkbox,
// footer with cancel/confirm + danger styling. This component owns the
// shared mechanics so the body can stay focused on the page's bespoke
// preview content.

import { ReactNode } from 'react'
import { Modal, ModalFooter } from './Modal'

export interface GovernedWarning {
  kind: 'high' | 'medium'
  text: string
}

export interface GovernedActionModalProps {
  open: boolean
  title: string
  subtitle?: string
  warnings?: GovernedWarning[]
  preview: ReactNode
  reason: string
  onReasonChange: (next: string) => void
  reasonPlaceholder?: string
  confirmLabel: string
  onCancel: () => void
  onConfirm: () => void | Promise<void>
  // When true, renders a confirm checkbox the user must tick before the
  // confirm action is allowed (used for weakening / high-risk changes).
  requireConfirmPhrase?: boolean
  confirmPhraseLabel?: string
  confirmChecked?: boolean
  onConfirmCheckedChange?: (next: boolean) => void
  // When true, the confirm button is rendered as danger.
  danger?: boolean
  // Returns true if the modal is currently allowed to commit (used by
  // the caller to disable the confirm button while submitting).
  canConfirm?: boolean
}

export function GovernedActionModal(props: GovernedActionModalProps) {
  const {
    open, title, subtitle, warnings = [], preview,
    reason, onReasonChange, reasonPlaceholder,
    confirmLabel, onCancel, onConfirm,
    requireConfirmPhrase = false,
    confirmPhraseLabel,
    confirmChecked = false,
    onConfirmCheckedChange,
    danger = false,
    canConfirm,
  } = props

  const reasonOK = reason.trim().length > 0
  const confirmOK = !requireConfirmPhrase || confirmChecked
  const allowed = canConfirm ?? (reasonOK && confirmOK)

  return (
    <Modal open={open} title={title} subtitle={subtitle} onClose={onCancel}
      footer={
        <ModalFooter
          onCancel={onCancel}
          onConfirm={onConfirm}
          confirmLabel={confirmLabel}
          disabled={!allowed}
          danger={danger}
        />
      }>
      <div className="space-y-3 text-xs">
        {warnings.map((w, i) => (
          <div key={i}
            className={`text-[11px] px-3 py-2 rounded-lg border ${
              w.kind === 'high'
                ? 'bg-red-50 text-red-700 border-red-200'
                : 'bg-yellow-50 text-yellow-800 border-yellow-200'
            }`}>
            {w.text}
          </div>
        ))}
        {preview}
        <div>
          <label className="text-[10px] text-gray-500">
            변경 사유 (필수 — 감사 기록에 남습니다)
          </label>
          <input className="input text-xs w-full" value={reason}
            onChange={e => onReasonChange(e.target.value)}
            placeholder={reasonPlaceholder || '예: 긴급 장애 대응 / 보안 검토 완료'} />
        </div>
        {requireConfirmPhrase && onConfirmCheckedChange && (
          <label className="flex items-center gap-2 text-xs text-gray-700">
            <input type="checkbox" checked={confirmChecked}
              onChange={e => onConfirmCheckedChange(e.target.checked)} />
            {confirmPhraseLabel || '변경의 영향을 확인했습니다'}
          </label>
        )}
      </div>
    </Modal>
  )
}