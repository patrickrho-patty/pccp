interface EmptyStateProps {
  icon?: string
  title: string
  message?: string
  action?: { label: string; onClick: () => void }
}

export default function EmptyState({ icon = '📭', title, message, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-center">
      <div className="text-3xl mb-2 opacity-30">{icon}</div>
      <p className="text-sm font-medium text-gray-500">{title}</p>
      {message && <p className="text-xs text-gray-400 mt-1">{message}</p>}
      {action && <button onClick={action.onClick} className="btn-sm btn-primary mt-3">{action.label}</button>}
    </div>
  )
}
