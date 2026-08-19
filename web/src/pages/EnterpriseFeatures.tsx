import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'
import { useAuth } from '../hooks/useAuth'
import { api } from '../api'
import { formatShortTime } from '../utils/format'
import {
  FEATURE_CATALOG, ADMIN_ROLES,
  type Scope, type HarnessInfo, type RolloutRecord, type Governance,
} from '../enterpriseCatalog'
import { parseGovernance, evaluateHarnesses, headEpoch } from '../governanceTrace'
import { validateChange, buildPreview, applyChange, buildRollback } from '../enterpriseChangeset'

const CATEGORY_INFO: Record<string, { icon: string; name: string; nameEn: string }> = {
  governance: { icon: '⚖️', name: '거버넌스', nameEn: 'Governance' },
  security: { icon: '🛡', name: '보안', nameEn: 'Security' },
  compliance: { icon: '📋', name: '컴플라이언스', nameEn: 'Compliance' },
  identity: { icon: '🔑', name: '신원', nameEn: 'Identity' },
  audit: { icon: '☰', name: '감사', nameEn: 'Audit' },
}

type Feature = {
  id: string
  feature_key: string
  feature_name: string
  feature_name_ko: string
  category: string
  prd_ref: string
  enabled: boolean
  enforced: boolean
  status: string
  last_reported_at: string
  violation_count: number
  config: string
}

type Violation = {
  id: string
  harness_id: string
  session_id: string
  feature_key: string
  severity: string
  description: string
  description_ko: string
  resolved: boolean
  occurred_at: string
}

type ChangeDraft = {
  feature: Feature
  target: { enabled: boolean; enforced: boolean }
  scope: Scope
  reason: string
  approved: boolean
  expectedEpoch: number
  rollbackOf?: number
}

function featureLabel(f: Feature) {
  return f.feature_name_ko || f.feature_name
}

function scopeSummary(scope: Scope, totalHarnesses: number): string {
  if (scope.type === 'selected') return `지정 하네스 ${scope.harness_ids.length}대`
  const n = Math.max(totalHarnesses - scope.exceptions.length, 0)
  return `조직 전체 ${n}대${scope.exceptions.length > 0 ? ` (예외 ${scope.exceptions.length}대)` : ''}`
}

// Labeled semantic switch — exposes feature + state to assistive tech.
function FeatureSwitch({ label, checked, disabled, ariaLabel, onToggle }: {
  label: string
  checked: boolean
  disabled?: boolean
  ariaLabel: string
  onToggle: () => void
}) {
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={ariaLabel}
      disabled={disabled} onClick={onToggle}
      className={`flex items-center gap-1.5 text-xs ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}>
      <span className="text-gray-500">{label}</span>
      <span className={`relative inline-flex h-4 w-7 items-center rounded-full transition-colors ${checked ? 'bg-patty-600' : 'bg-gray-300'}`}>
        <span className={`inline-block h-2.5 w-2.5 rounded-full bg-white transition-transform ${checked ? 'translate-x-4' : 'translate-x-1'}`} />
      </span>
      <span className="sr-only">{checked ? '켜짐' : '꺼짐'}</span>
    </button>
  )
}

export default function EnterpriseFeatures() {
  const { role, email } = useAuth()
  const [features, setFeatures] = useState<Feature[]>([])
  const [violations, setViolations] = useState<Violation[]>([])
  const [harnesses, setHarnesses] = useState<HarnessInfo[]>([])
  const [tab, setTab] = useState<'features' | 'violations'>('features')
  const [loading, setLoading] = useState(true)
  const [detail, setDetail] = useState<Feature | null>(null)
  const [draft, setDraft] = useState<ChangeDraft | null>(null)

  const isAdmin = ADMIN_ROLES.includes(role)

  // One governance parse per feature config, shared by the cards, the
  // detail modal, and the change flow (epoch derives from this parse).
  const govByConfig = useMemo(() => {
    const m = new Map<string, Governance>()
    for (const f of features) m.set(f.config, parseGovernance(f.config))
    return m
  }, [features])
  const govOf = (config: string): Governance => govByConfig.get(config) ?? parseGovernance(config)

  const load = () => {
    const h = authHeaders()
    Promise.all([
      fetch('/api/enterprise/features', { headers: h }).then(r => r.json()).catch(() => []),
      fetch('/api/enterprise/violations', { headers: h }).then(r => r.json()).catch(() => []),
      api.listHarnesses().catch(() => []),
    ]).then(([f, v, hs]) => {
      setFeatures(Array.isArray(f) ? f : [])
      setViolations(Array.isArray(v) ? v : [])
      setHarnesses(Array.isArray(hs) ? hs : [])
      setLoading(false)
    })
  }
  useEffect(() => { load() }, [])

  const seed = async () => {
    await fetch('/api/enterprise/features/seed', { method: 'POST', headers: authHeaders() })
    showToast('20개 기능 등록됨', 'success')
    load()
  }

  const beginChange = (f: Feature, field: 'enabled' | 'enforced') => {
    setDraft({
      feature: f,
      target: { enabled: f.enabled, enforced: f.enforced, [field]: !f[field] },
      scope: govOf(f.config).scope,
      reason: '',
      approved: false,
      expectedEpoch: headEpoch(govOf(f.config)),
    })
  }

  const beginRollback = (f: Feature, record: RolloutRecord) => {
    setDraft({
      feature: f,
      target: { ...record.from },
      scope: record.scope,
      reason: '',
      approved: false,
      expectedEpoch: headEpoch(govOf(f.config)),
      rollbackOf: record.epoch,
    })
  }

  const confirmChange = async () => {
    if (!draft) return
    const f = draft.feature
    const entry = FEATURE_CATALOG[f.feature_key]
    if (!entry) return
    const evals = evaluateHarnesses(entry, harnesses, draft.scope, Date.now())
    const common = {
      feature: f, scope: draft.scope, reason: draft.reason, actor: email || role,
      now: new Date().toISOString(), evals, expectedEpoch: draft.expectedEpoch,
    }
    const result = draft.rollbackOf !== undefined
      ? buildRollback({ ...common, epoch: draft.rollbackOf })
      : applyChange({ ...common, target: draft.target })
    if (result.error || !result.config) {
      showToast(result.error || '변경에 실패했습니다', 'error')
      return
    }
    try {
      await api.updateEnterpriseFeature(f.id, {
        enabled: draft.target.enabled, enforced: draft.target.enforced,
        config: result.config, reason: draft.reason, expected_epoch: draft.expectedEpoch,
      })
    } catch (err) {
      const msg = err instanceof Error ? err.message : ''
      if (msg.includes('epoch_conflict')) {
        showToast('다른 변경이 먼저 적용되었습니다. 최신 상태를 불러온 후 다시 시도하세요.', 'error')
        load()
      } else if (msg.includes('patty_mandatory_weakening_forbidden')) {
        showToast('패티 필수 기능은 테넌트 관리자가 비활성화하거나 강제를 해제할 수 없습니다.', 'error')
      } else {
        showToast('변경 저장에 실패했습니다', 'error')
      }
      return
    }
    showToast(draft.rollbackOf !== undefined ? `에포크 ${draft.rollbackOf} 롤백이 기록되었습니다` : `에포크 ${result.record?.epoch} 롤아웃이 기록되었습니다`, 'success')
    setDraft(null)
    load()
  }

  // PAT-1516: violation resolution is evidence-backed (PAT-1516). The
// disposition, reason, and (for risk_accepted) expiry are required
// so the resolution is documented, auditable, and bounded.
const resolveViolation = async (id: string) => {
  const disposition = window.prompt('결론 (fixed | false_positive | risk_accepted | duplicate | suppressed):', 'fixed')
  if (!disposition) return
  if (!['fixed', 'false_positive', 'risk_accepted', 'duplicate', 'suppressed'].includes(disposition)) {
    showToast('유효하지 않은 결론', 'error'); return
  }
  const reason = window.prompt('결론 사유 (필수 — 감사 로그):', '')
  if (!reason || !reason.trim()) return
  const payload: any = { disposition, disposition_reason: reason.trim(), evidence: [], owner_id: '' }
  if (disposition === 'risk_accepted') {
    const until = window.prompt('위험 수락 만료 (ISO8601, 예: 2026-12-31T00:00:00Z):', new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString())
    if (!until) return
    payload.expires_at = until
  }
  try {
    const resp = await fetch(`/api/enterprise/violations/${id}`, { method: 'PUT', headers: authHeaders(), body: JSON.stringify(payload) })
    if (!resp.ok) { showToast((await resp.json()).error || '실패', 'error'); return }
    showToast(`위반 결론 기록됨 (${disposition})`, 'success')
    load()
  } catch (e: any) { showToast(e?.message || '실패', 'error') }
}

  // Harness evals are computed once per real change input (feature/scope/
  // harnesses) — not per reason-textarea keystroke — and shared between
  // validation and the impact preview.
  const draftEntry = draft ? FEATURE_CATALOG[draft.feature.feature_key] : undefined
  const draftEvals = useMemo(() => {
    if (!draft?.feature || !draft?.scope || !draftEntry) return []
    return evaluateHarnesses(draftEntry, harnesses, draft.scope, Date.now())
  }, [draft?.feature, draft?.scope, draftEntry, harnesses])

  const validation = useMemo(() => {
    if (!draft?.feature || !draft?.target || !draft?.scope) return null
    return validateChange({
      feature: draft.feature, features, harnesses,
      target: draft.target, scope: draft.scope, role, now: Date.now(), evals: draftEvals,
    })
  }, [draft?.feature, draft?.target, draft?.scope, features, harnesses, role, draftEvals])

  if (loading) return <div className="text-gray-500">로딩 중...</div>

  const stats = {
    total: features.length,
    enabled: features.filter(f => f.enabled).length,
    enforced: features.filter(f => f.enforced).length,
    violations: violations.filter(v => !v.resolved).length,
  }

  const draftWeakening = draft ? (draft.feature.enabled && !draft.target.enabled) || (draft.feature.enforced && !draft.target.enforced) : false
  const draftNeedsApproval = !!(draft && draftEntry?.mandatory && draftWeakening)
  const draftConfirmDisabled = !draft || !validation || validation.blockers.length > 0 || !draft.reason.trim() || (draftNeedsApproval && !draft.approved)

  return (
    <div>
      <div className="flex justify-between items-center mb-4">
        <div>
          <h1 className="text-2xl font-bold">엔터프라이즈 하네스 기능 <span className="text-gray-400 text-lg font-normal">Enterprise Harness Features</span></h1>
          <p className="text-xs text-gray-400 mt-1">기업/정부 전용 하네스 기능 · PRD §33 Korean Enterprise Differentiators · 공용 에디션 미제공</p>
        </div>
        {features.length === 0 && <button onClick={seed} className="btn-primary text-sm">20개 기본 기능 등록</button>}
      </div>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-3 mb-6">
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-blue-600">{stats.total}</div><div className="text-xs text-gray-500">전체 기능</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-green-600">{stats.enabled}</div><div className="text-xs text-gray-500">활성화</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-purple-600">{stats.enforced}</div><div className="text-xs text-gray-500">강제 적용</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-red-600">{stats.violations}</div><div className="text-xs text-gray-500">위반</div></div>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'features', label: '기능 목록', en: 'Features' },
          { id: 'violations', label: '위반 사항', en: 'Violations', count: stats.violations },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && t.count > 0 && <span className="badge-red text-[10px] ml-1">{t.count}</span>}
          </button>
        ))}
      </div>

      {/* Features Tab */}
      {tab === 'features' && (
        <div className="space-y-4">
          {Object.entries(CATEGORY_INFO).map(([catId, catInfo]) => {
            const catFeatures = features.filter(f => f.category === catId)
            if (catFeatures.length === 0) return null
            return (
              <div key={catId} className="card">
                <div className="flex items-center gap-2 mb-3">
                  <span className="text-xl">{catInfo.icon}</span>
                  <h3 className="text-sm font-semibold">{catInfo.name} <span className="text-gray-400 font-normal">{catInfo.nameEn}</span></h3>
                  <span className="badge-gray ml-auto">{catFeatures.length}개</span>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  {catFeatures.map(f => {
                    const entry = FEATURE_CATALOG[f.feature_key]
                    const gov = govOf(f.config)
                    const epoch = headEpoch(gov)
                    return (
                      <div key={f.id} className={`p-3 rounded-lg border ${f.enabled ? 'border-gray-200' : 'border-gray-100 bg-gray-50 opacity-60'}`}>
                        <div className="flex items-start justify-between mb-1">
                          <div className="flex-1">
                            <button onClick={() => setDetail(f)} className="text-sm font-medium text-left hover:text-patty-600 hover:underline">
                              {featureLabel(f)}
                            </button>
                            <div className="text-xs text-gray-400">{f.feature_name}</div>
                          </div>
                          <div className="flex items-center gap-1">
                            {entry?.mandatory
                              ? <span className="badge-red text-[10px]" title="패티 필수 기능 — 테넌트 관리자는 약화할 수 없습니다">패티 필수</span>
                              : <span className="badge-gray text-[10px]" title="테넌트 관리자가 변경할 수 있는 기능">테넌트 설정</span>}
                            {f.status === 'planned' && <span className="badge-gray text-[10px]" title="아직 시행되지 않음">계획</span>}
                            {f.enforced && <span className="badge-red text-[10px]">의무</span>}
                            <Link to="/audit" className="text-[10px] text-gray-400 hover:underline">{f.prd_ref}</Link>
                          </div>
                        </div>
                        <div className="text-[11px] text-gray-400 mt-1">
                          적용 범위: {scopeSummary(gov.scope, harnesses.length)} · 에포크 {epoch}
                        </div>
                        <div className="flex items-center justify-between mt-2">
                          <div className="flex items-center gap-2 text-xs">
                            {f.violation_count > 0 && <span className="text-red-500">⚠ {f.violation_count} 위반</span>}
                            {f.last_reported_at && <span className="text-gray-400">마지막 보고: {formatShortTime(f.last_reported_at)}</span>}
                          </div>
                          <div className="flex items-center gap-3">
                            <FeatureSwitch label="강제" checked={f.enforced} disabled={!isAdmin}
                              ariaLabel={`${featureLabel(f)} 강제 적용 — 현재 ${f.enforced ? '켜짐' : '꺼짐'}`}
                              onToggle={() => beginChange(f, 'enforced')} />
                            <FeatureSwitch label="활성화" checked={f.enabled} disabled={!isAdmin}
                              ariaLabel={`${featureLabel(f)} 활성화 — 현재 ${f.enabled ? '켜짐' : '꺼짐'}`}
                              onToggle={() => beginChange(f, 'enabled')} />
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            )
          })}
          {features.length === 0 && (
            <div className="card text-center py-12">
              <EmptyState icon="🏢" title="등록된 엔터프라이즈 기능이 없습니다" message="20개 기본 기능 등록 버튼으로 시작하세요" />
              <button onClick={seed} className="btn-primary text-sm mt-2">20개 기본 기능 등록</button>
            </div>
          )}
        </div>
      )}

      {/* Violations Tab */}
      {tab === 'violations' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">엔터프라이즈 정책 위반 · Enterprise Policy Violations</h3>
          {violations.length === 0 ? (
            <p className="text-gray-400 text-center py-8">위반 사항이 없습니다 ✅</p>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3">기능</th><th className="pb-3">심각도</th><th className="pb-3">설명</th>
                <th className="pb-3">하네스</th><th className="pb-3">시간</th><th className="pb-3"></th>
              </tr></thead>
              <tbody>
                {violations.map(v => (
                  <tr key={v.id} className="border-b border-gray-100 last:border-0">
                    <td className="py-3 text-xs font-mono">{v.feature_key}</td>
                    <td className="py-3"><span className={v.severity === 'critical' ? 'badge-red' : v.severity === 'high' ? 'badge-yellow' : 'badge-gray'}>{v.severity}</span></td>
                    <td className="py-3 text-sm">{v.description_ko || v.description}</td>
                    <td className="py-3 text-xs font-mono"><Link to="/harnesses" className="text-blue-600 hover:underline">{v.harness_id?.slice(0, 15)}</Link></td>
                    <td className="py-3 text-xs text-gray-400">{v.occurred_at?.slice(0, 19)}</td>
                    <td className="py-3"><button onClick={() => resolveViolation(v.id)} className="text-xs text-green-600 hover:underline">해결</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Feature Detail Modal */}
      <Modal open={!!detail} onClose={() => setDetail(null)} size="lg"
        title={detail ? featureLabel(detail) : ''}
        subtitle={detail ? `${detail.feature_name} · ${detail.feature_key} · ${detail.prd_ref}` : undefined}>
        {detail && (() => {
          const entry = FEATURE_CATALOG[detail.feature_key]
          const gov = govOf(detail.config)
          const detailViolations = violations.filter(v => v.feature_key === detail.feature_key)
          return (
            <div className="space-y-4 text-sm">
              <div className="flex items-center gap-2">
                {entry?.mandatory
                  ? <span className="badge-red text-[10px]">패티 필수</span>
                  : <span className="badge-gray text-[10px]">테넌트 설정 가능</span>}
                <span className="badge-gray text-[10px]">{detail.enabled ? '활성화됨' : '비활성'}</span>
                <span className="badge-gray text-[10px]">{detail.enforced ? '강제 적용' : '강제 아님'}</span>
                {detail.status === 'planned' && <span className="badge-gray text-[10px]">계획</span>}
              </div>

              {entry ? (
                <div className="space-y-2">
                  <div>
                    <div className="text-xs font-semibold text-gray-500 mb-0.5">목적</div>
                    <p>{entry.purposeKo}</p>
                  </div>
                  <div>
                    <div className="text-xs font-semibold text-gray-500 mb-0.5">보안/거버넌스 근거</div>
                    <p className="text-gray-600">{entry.rationaleKo}</p>
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <div className="text-xs font-semibold text-gray-500 mb-0.5">요구 하네스 버전</div>
                      <p className="font-mono text-xs">v{entry.minHarnessVersion}+</p>
                    </div>
                    <div>
                      <div className="text-xs font-semibold text-gray-500 mb-0.5">적용 범위 · 상속</div>
                      <p className="text-xs">
                        {scopeSummary(gov.scope, harnesses.length)} · {gov.scope.type === 'org' ? '조직 정책 상속(모든 하네스)' : '조직 정책 재정의(지정 하네스)'}
                      </p>
                    </div>
                  </div>
                  <div>
                    <div className="text-xs font-semibold text-gray-500 mb-0.5">의존 기능</div>
                    {entry.dependencies.length === 0 ? (
                      <p className="text-xs text-gray-400">없음</p>
                    ) : (
                      <ul className="text-xs space-y-0.5">
                        {entry.dependencies.map(depKey => {
                          const dep = features.find(x => x.feature_key === depKey)
                          const depEntry = FEATURE_CATALOG[depKey]
                          const ok = dep?.enabled && dep.status !== 'planned'
                          return (
                            <li key={depKey} className={ok ? 'text-gray-600' : 'text-red-600'}>
                              {depEntry?.purposeKo ? `${depKey} — ${ok ? '활성' : '비활성/계획'}` : depKey}
                              {!ok && ' (먼저 활성화 필요)'}
                            </li>
                          )
                        })}
                      </ul>
                    )}
                  </div>
                </div>
              ) : (
                <p className="text-xs text-red-600">카탈로그에 등록되지 않은 기능 키입니다: {detail.feature_key}</p>
              )}

              <div className="grid grid-cols-3 gap-3 text-xs">
                <div className="card py-2 px-3"><div className="text-gray-500">영향 하네스</div><div className="font-semibold">{scopeSummary(gov.scope, harnesses.length)}</div></div>
                <div className="card py-2 px-3"><div className="text-gray-500">미해결 위반</div><div className="font-semibold text-red-600">{detailViolations.length}건 (누적 {detail.violation_count})</div></div>
                <div className="card py-2 px-3"><div className="text-gray-500">마지막 보고</div><div className="font-semibold">{detail.last_reported_at ? formatShortTime(detail.last_reported_at) : '없음'}</div></div>
              </div>

              <div>
                <div className="text-xs font-semibold text-gray-500 mb-1">롤아웃 이력 · Rollout History</div>
                {gov.rollouts.length === 0 ? (
                  <p className="text-xs text-gray-400">기록된 롤아웃이 없습니다. 변경은 범위·사유 확인 후 에포크로 기록됩니다.</p>
                ) : (
                  <div className="space-y-2">
                    {[...gov.rollouts].sort((a, b) => b.epoch - a.epoch).map(r => (
                      <div key={r.epoch} className="border border-gray-200 rounded-lg p-3">
                        <div className="flex items-center justify-between">
                          <div className="text-xs font-medium">
                            에포크 {r.epoch} {r.kind === 'rollback' && <span className="badge-yellow text-[10px] ml-1">롤백{r.rollback_of ? ` (대상 ${r.rollback_of})` : ''}</span>}
                          </div>
                          {isAdmin && r.kind === 'change' && (
                            <button onClick={() => { const f = detail; setDetail(null); beginRollback(f, r) }}
                              className="text-xs text-red-600 hover:underline">이 상태로 롤백</button>
                          )}
                        </div>
                        <div className="text-xs text-gray-500 mt-1">
                          {r.at?.slice(0, 19)} · {r.by} · 범위: {r.scope.type === 'org' ? `조직 전체${r.scope.exceptions.length > 0 ? ` (예외 ${r.scope.exceptions.length}대)` : ''}` : `지정 ${r.scope.harness_ids.length}대`}
                        </div>
                        <div className="text-xs text-gray-600 mt-1">사유: {r.reason}</div>
                        <div className="text-xs mt-1">
                          활성화 {r.from.enabled ? '켜짐' : '꺼짐'} → {r.to.enabled ? '켜짐' : '꺼짐'} · 강제 {r.from.enforced ? '켜짐' : '꺼짐'} → {r.to.enforced ? '켜짐' : '꺼짐'}
                        </div>
                        {r.results.length > 0 && (
                          <table className="w-full text-xs mt-2">
                            <thead><tr className="text-left text-gray-400 border-b border-gray-100">
                              <th className="pb-1">하네스</th><th className="pb-1">버전</th><th className="pb-1">결과</th>
                            </tr></thead>
                            <tbody>
                              {r.results.map(e => (
                                <tr key={e.harness_id} className="border-b border-gray-50 last:border-0">
                                  <td className="py-1 font-mono">{e.name}</td>
                                  <td className="py-1 font-mono">{e.version}</td>
                                  <td className={`py-1 ${e.result === '적용 가능' ? 'text-green-600' : e.result === '오프라인 대기' ? 'text-gray-500' : 'text-red-600'}`}>{e.result}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )
        })()}
      </Modal>

      {/* Change / Rollback Modal — scope, validation, impact preview, reason, approval */}
      <Modal open={!!draft} onClose={() => setDraft(null)} size="lg"
        title={draft ? `${draft.rollbackOf !== undefined ? '롤백' : '기능 변경'}: ${featureLabel(draft.feature)}` : ''}
        subtitle={draft ? `현재 에포크 ${draft.expectedEpoch} · 변경은 확인 후에만 적용됩니다` : undefined}
        footer={draft && (
          <ModalFooter onCancel={() => setDraft(null)} onConfirm={confirmChange}
            confirmLabel={draft.rollbackOf !== undefined ? '롤백 확정' : '변경 확정'}
            danger={draftWeakening || draft.rollbackOf !== undefined}
            disabled={draftConfirmDisabled} />
        )}>
        {draft && validation && (
          <div className="space-y-4 text-sm">
            <div>
              <div className="text-xs font-semibold text-gray-500 mb-1">변경 내용 · Diff</div>
              <ul className="text-xs space-y-0.5 list-disc list-inside">
                {buildPreview(draft.feature, draft.target, draft.scope, draftEvals).map((line, i) => <li key={i}>{line}</li>)}
              </ul>
            </div>

            <div>
              <div className="text-xs font-semibold text-gray-500 mb-1">적용 범위 선택</div>
              <div className="flex gap-3 text-xs mb-2">
                <label className="flex items-center gap-1 cursor-pointer">
                  <input type="radio" name="ef-change-scope" checked={draft.scope.type === 'org'}
                    onChange={() => setDraft({ ...draft, scope: { ...draft.scope, type: 'org' } })} />
                  조직 전체
                </label>
                <label className="flex items-center gap-1 cursor-pointer">
                  <input type="radio" name="ef-change-scope" checked={draft.scope.type === 'selected'}
                    onChange={() => setDraft({ ...draft, scope: { ...draft.scope, type: 'selected' } })} />
                  하네스 지정
                </label>
              </div>
              {draft.scope.type === 'selected' && (
                <div className="max-h-32 overflow-y-auto border border-gray-200 rounded p-2 space-y-1">
                  {harnesses.length === 0 && <p className="text-xs text-gray-400">등록된 하네스가 없습니다.</p>}
                  {harnesses.map(h => (
                    <label key={h.harness_id} className="flex items-center gap-2 text-xs cursor-pointer">
                      <input type="checkbox" checked={draft.scope.harness_ids.includes(h.harness_id)}
                        onChange={e => setDraft({
                          ...draft,
                          scope: {
                            ...draft.scope,
                            harness_ids: e.target.checked
                              ? [...draft.scope.harness_ids, h.harness_id]
                              : draft.scope.harness_ids.filter(id => id !== h.harness_id),
                          },
                        })} />
                      <span className="font-mono">{h.name || h.harness_id}</span>
                      <span className="text-gray-400">v{h.binary_version || '?'} · {h.status}</span>
                    </label>
                  ))}
                </div>
              )}
              {draft.scope.type === 'org' && harnesses.length > 0 && (
                <details className="text-xs">
                  <summary className="cursor-pointer text-gray-500">예외 하네스 지정 ({draft.scope.exceptions.length}대)</summary>
                  <div className="max-h-32 overflow-y-auto border border-gray-200 rounded p-2 mt-1 space-y-1">
                    {harnesses.map(h => (
                      <label key={h.harness_id} className="flex items-center gap-2 cursor-pointer">
                        <input type="checkbox" checked={draft.scope.exceptions.includes(h.harness_id)}
                          onChange={e => setDraft({
                            ...draft,
                            scope: {
                              ...draft.scope,
                              exceptions: e.target.checked
                                ? [...draft.scope.exceptions, h.harness_id]
                                : draft.scope.exceptions.filter(id => id !== h.harness_id),
                            },
                          })} />
                        <span className="font-mono">{h.name || h.harness_id}</span>
                      </label>
                    ))}
                  </div>
                </details>
              )}
            </div>

            {validation.blockers.length > 0 && (
              <div className="rounded-lg bg-red-50 border border-red-200 p-3">
                <div className="text-xs font-semibold text-red-700 mb-1">적용 불가</div>
                <ul className="text-xs text-red-600 space-y-0.5 list-disc list-inside">
                  {validation.blockers.map((b, i) => <li key={i}>{b}</li>)}
                </ul>
              </div>
            )}
            {validation.warnings.length > 0 && (
              <div className="rounded-lg bg-yellow-50 border border-yellow-200 p-3">
                <div className="text-xs font-semibold text-yellow-700 mb-1">주의</div>
                <ul className="text-xs text-yellow-700 space-y-0.5 list-disc list-inside">
                  {validation.warnings.map((w, i) => <li key={i}>{w}</li>)}
                </ul>
              </div>
            )}

            <div>
              <label htmlFor="change-reason" className="text-xs font-semibold text-gray-500 block mb-1">
                {draft.rollbackOf !== undefined ? '롤백 사유' : '변경 사유'} (필수, 감사 로그에 기록)
              </label>
              <textarea id="change-reason" value={draft.reason} onChange={e => setDraft({ ...draft, reason: e.target.value })}
                className="w-full border border-gray-300 rounded-lg p-2 text-xs" rows={2}
                placeholder="예: 분기 감사 대비 변경 동결 적용" />
            </div>

            {draftNeedsApproval && (
              <label className="flex items-start gap-2 text-xs cursor-pointer rounded-lg border border-red-200 bg-red-50 p-3">
                <input type="checkbox" className="mt-0.5" checked={draft.approved}
                  onChange={e => setDraft({ ...draft, approved: e.target.checked })} />
                <span>패티 필수 기능 약화 — 정책 변경 승인 절차를 완료했음을 확인합니다.</span>
              </label>
            )}
          </div>
        )}
      </Modal>
    </div>
  )
}

function authHeaders(): Record<string, string> {
  const token = sessionStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
