export type AllowedModelState = 'single' | 'many' | 'retired' | 'unavailable' | 'unknown' | 'restricted'
export type AllowedModelEntityKind = 'model' | 'class'

export const DEFAULT_ALLOWED_MODELS = ['patty-code-standard'] as const

export const ALLOWED_MODEL_STATE_LABEL_KO: Record<AllowedModelState, string> = {
  single: '',
  many: '여러 모델',
  retired: '사용 중단',
  unavailable: '현재 사용할 수 없음',
  unknown: '등록되지 않음',
  restricted: '접근 권한 없음',
}

export interface AllowedModelItem {
  id: string
  label: string
  state: AllowedModelState
  entity_kind?: AllowedModelEntityKind
  catalog_model_id?: string
  package_id?: string
}

const ALLOWED_STATES = new Set<AllowedModelState>(['single', 'many', 'retired', 'unavailable', 'unknown', 'restricted'])

const CLASS_LABEL_KO: Record<string, string> = {
  code: '코드 생성',
  chat: '대화',
  reasoning: '추론',
  text: '텍스트',
  vision: '비전',
  embed: '임베딩',
  audio: '오디오',
  'enterprise-code': '엔터프라이즈 코드',
}

export function classLabel(modelClass: string): string {
  return CLASS_LABEL_KO[modelClass] || modelClass
}

export function modelClassOptions() {
  return Object.entries(CLASS_LABEL_KO).map(([value, label]) => ({ value, label }))
}

export function allowedModelDestination(item: AllowedModelItem): string {
  if (item.package_id) return `/models/${encodeURIComponent(item.package_id)}`
  if (item.catalog_model_id || item.entity_kind === 'model') return `/models?catalog=${encodeURIComponent(item.catalog_model_id || item.id)}`
  return `/models?class=${encodeURIComponent(item.id)}`
}

export function normalizeAllowedModelItems(value: unknown): AllowedModelItem[] {
  if (!Array.isArray(value)) return []
  const items: AllowedModelItem[] = []
  for (const candidate of value) {
    if (!candidate || typeof candidate !== 'object') continue
    const row = candidate as Record<string, unknown>
    if (typeof row.id !== 'string' || !row.id || typeof row.label !== 'string' || typeof row.state !== 'string' || !ALLOWED_STATES.has(row.state as AllowedModelState)) continue
    const isCatalogModel = row.entity_kind === 'model' || (typeof row.catalog_model_id === 'string' && Boolean(row.catalog_model_id))
    const item: AllowedModelItem = { id: row.id, label: row.label && (row.label !== row.id || isCatalogModel) ? row.label : classLabel(row.id), state: row.state as AllowedModelState }
    if (row.entity_kind === 'model' || row.entity_kind === 'class') item.entity_kind = row.entity_kind
    if (typeof row.catalog_model_id === 'string' && row.catalog_model_id) item.catalog_model_id = row.catalog_model_id
    if (typeof row.package_id === 'string' && row.package_id) item.package_id = row.package_id
    items.push(item)
  }
  return items
}

export function allowedModelPolicySummary(items: AllowedModelItem[], policyState: string): string {
  if (policyState === 'invalid') return '정책 데이터 확인 필요'
  if (policyState === 'unrestricted') return '제한 없음 · 모든 모델 허용'
  if (items.length === 0) return '허용 모델 정보를 확인할 수 없음'
  return items.map(item => item.label || classLabel(item.id)).join(', ')
}

export function filterCatalogModels<T extends Record<string, any>>(models: T[], catalogID: string, modelClass: string): T[] {
  return models.filter(model => {
    if (catalogID && model.catalog_model_id !== catalogID) return false
    if (modelClass && model.family !== modelClass && model.entitlement?.class !== modelClass) return false
    return true
  })
}

export function modelPackageState(modelPackage: Record<string, unknown>): string {
  return typeof modelPackage.state === 'string' && modelPackage.state ? modelPackage.state : 'unknown'
}
