import { Link } from 'react-router-dom'
import {
  AllowedModelItem,
  ALLOWED_MODEL_STATE_LABEL_KO,
  allowedModelDestination,
  classLabel,
  modelClassOptions,
} from './allowedModelView'

export type { AllowedModelEntityKind, AllowedModelItem, AllowedModelState } from './allowedModelView'
export {
  ALLOWED_MODEL_STATE_LABEL_KO,
  DEFAULT_ALLOWED_MODELS,
  allowedModelDestination,
  allowedModelPolicySummary,
  classLabel,
  filterCatalogModels,
  modelClassOptions,
  normalizeAllowedModelItems,
} from './allowedModelView'

export const ALLOWED_MODEL_COLLAPSE_AT = 5

export function AllowedModelChips({ items, policyState }: { items: AllowedModelItem[]; policyState?: string }) {
  if (policyState === 'invalid') {
    return <span className="badge badge-yellow border border-yellow-300">정책 데이터 확인 필요 · 원본 허용 목록이 올바르지 않습니다</span>
  }
  if (items.length === 0) {
    return policyState === 'unrestricted'
      ? <span className="text-xs text-gray-400">제한 없음 · 모든 모델 허용</span>
      : <span className="badge badge-yellow border border-yellow-300">허용 모델 정보를 확인할 수 없음</span>
  }
  const chip = (item: AllowedModelItem, compact: boolean) => {
    const suffix = ALLOWED_MODEL_STATE_LABEL_KO[item.state]
    const muted = item.state === 'unknown' || item.state === 'retired' || item.state === 'unavailable' || item.state === 'restricted'
    return (
      <Link key={item.id} to={allowedModelDestination(item)} title={`${item.id}${suffix ? ` · ${suffix}` : ''}`}
        className={`badge ${compact ? 'text-[10px] px-1.5 py-0.5' : 'text-xs'} ${muted ? 'badge-gray border-dashed' : 'badge-blue'}`}>
        {item.label || classLabel(item.id)}{suffix ? ` (${suffix})` : ''}
      </Link>
    )
  }
  const shown = items.slice(0, ALLOWED_MODEL_COLLAPSE_AT)
  const rest = items.length - shown.length
  return (
    <div className="inline-flex flex-wrap gap-1 items-center">
      {shown.map(item => chip(item, false))}
      {rest > 0 && (
        <details className="inline-block">
          <summary className="text-xs text-blue-600 cursor-pointer select-none list-none">외 {rest}개</summary>
          <div className="inline-flex flex-wrap gap-1 items-center ml-1">
            {items.slice(ALLOWED_MODEL_COLLAPSE_AT).map(item => chip(item, true))}
          </div>
        </details>
      )}
    </div>
  )
}
