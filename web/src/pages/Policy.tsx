import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { EntitySelect } from '../components/EntitySelect'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { formatRelative } from '../utils/format'
import { useConfirm } from '../components/useConfirm'
import { showToast } from '../components/Toast'

const DOMAIN_INFO: Record<string, { name: string; nameEn: string; icon: string; desc: string }> = {
  models: { name: '모델 접근 정책', nameEn: 'Model Access', icon: '◆', desc: '조직/부서/프로젝트별 허용 모델 제어' },
  tools: { name: '도구 권한 정책', nameEn: 'Tool Permissions', icon: '🔧', desc: '하네스가 사용할 수 있는 도구와 승인 규칙' },
  data: { name: '데이터 보호 정책', nameEn: 'Data Protection', icon: '🛡', desc: '민감 정보, 개인정보, 비밀번호 보호' },
  scm: { name: 'Git/SCM 정책', nameEn: 'Git/SCM Governance', icon: '🌿', desc: '브랜치 보호, 커밋 규칙, PR 승인' },
  network: { name: '네트워크 정책', nameEn: 'Network Access', icon: '🌐', desc: '외부 통신 대상 제한' },
  session: { name: '세션 정책', nameEn: 'Session Controls', icon: '⏱', desc: '세션 시간 제한, 동시성, 자동 종료' },
}

const LAYERS = ['Patty 필수', '프로필', '조직', '계열사', '부서', '프로젝트', '저장소', '브랜치', '세션']

type Rule = any

export default function Policy() {
  const confirm = useConfirm()
  const [rules, setRules] = useState<Rule[]>([])
  const [epochs, setEpochs] = useState<any[]>([])
  const [templates, setTemplates] = useState<any[]>([])
  const [packs, setPacks] = useState<any[]>([])
  const [exceptions, setExceptions] = useState<any[]>([])
  const [tab, setTab] = useState<'active' | 'templates' | 'epochs' | 'packs' | 'exceptions'>('active')
  const [domainFilter, setDomainFilter] = useState('')
  const [search, setSearch] = useState('')
  const [showBuilder, setShowBuilder] = useState(false)
  const [builderDomain, setBuilderDomain] = useState('')
  const [builderTemplate, setBuilderTemplate] = useState<any>(null)
  const [builderScope, setBuilderScope] = useState('org')
  const [builderScopeId, setBuilderScopeId] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const { favorites, isFavorite, toggle } = useFavorites('policy_rules')
  // Hierarchy + effective policy (B1)
  const [activeLayer, setActiveLayer] = useState<number | null>(null)
  const [effective, setEffective] = useState<any>(null)
  const [effProject, setEffProject] = useState('')
  const [effRepo, setEffRepo] = useState('')
  // Epoch diff + acks (B3/C2)
  const [diffModal, setDiffModal] = useState<{ a: string; b: string } | null>(null)
  const [diff, setDiff] = useState<any>(null)
  const [acksModal, setAcksModal] = useState<any>(null)
  const [acks, setAcks] = useState<any[]>([])
  // Simulation (C3)
  const [simRuleIds, setSimRuleIds] = useState<Set<string>>(new Set())
  const [simResult, setSimResult] = useState<any>(null)
  // Pack + exception modals
  const [packModal, setPackModal] = useState(false)
  const [packForm, setPackForm] = useState({ name: '', name_ko: '', version: '1', profile: 'enterprise', rule_ids: [] as string[] })
  const [assignPack, setAssignPack] = useState<any>(null)
  const [assignForm, setAssignForm] = useState({ scope: 'org', scope_id: '' })
  const [exceptionModal, setExceptionModal] = useState(false)
  const [exceptionForm, setExceptionForm] = useState({ scope: 'project', scope_id: '', scopeName: '', reason: '', rule_ids: [] as string[] })
  const [templateEdit, setTemplateEdit] = useState<any>(null)
  const [templateForm, setTemplateForm] = useState({ name: '', nameEn: '', desc: '', config: '{}', version: '1' })

  const reloadRules = () => api.listPolicyRules().then(data => setRules(Array.isArray(data) ? data : []))
  const reloadAll = () => {
    api.listEpochs().then(d => setEpochs(Array.isArray(d) ? d : []))
    reloadRules()
    api.listPolicyTemplates().then(d => setTemplates(Array.isArray(d) ? d : [])).catch(() => {})
    api.listPolicyPacks().then(d => setPacks(Array.isArray(d) ? d : [])).catch(() => {})
    api.listPolicyExceptions().then(d => setExceptions(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(() => { reloadAll() }, [])

  const templatesByDomain = useMemo(() => {
    const map: Record<string, any[]> = {}
    for (const t of templates) {
      (map[t.domain] = map[t.domain] || []).push(t)
    }
    return map
  }, [templates])

  const filteredRules = rules.filter(r => {
    if (domainFilter && r.domain !== domainFilter) return false
    if (search) {
      const q = search.toLowerCase()
      const hay = `${r.name} ${r.nameEn} ${r.desc} ${r.domain} ${r.scopeName} ${JSON.stringify(r.config || {})}`.toLowerCase()
      if (!hay.includes(q)) return false
    }
    return true
  })

  const activeEpoch = epochs.find(e => e.status === 'active')
  const draftCount = rules.filter(r => r.status === 'draft').length
  const enforcedCount = rules.filter(r => r.status === 'approved' && r.enabled).length

  // Build a rule from a template (dedupe by template_id + scope — UX15).
  const addRuleFromTemplate = (template: any, scope = 'org', scopeId = '', scopeName = '전체 조직') => {
    const dup = rules.find(r => r.template_id === template.template_id && r.scope === scope && r.scopeName === scopeName)
    if (dup) { showToast('이미 같은 범위에 동일 템플릿이 있습니다 · Dedupe', 'error'); return }
    api.createPolicyRule({
      domain: template.domain, template_id: template.template_id,
      name: template.name, nameEn: template.nameEn, desc: template.desc,
      scope, scopeName, enabled: true, config: JSON.parse(template.config || '{}'),
    }).then((res: any) => {
      if (res?.conflicts?.length) showToast(`⚠ ${res.conflicts.length}개 중첩 규칙 감지 — 같은 범위의 다른 규칙과 충돌할 수 있습니다`, 'error')
      reloadAll()
      showToast('초안으로 저장됨 — 승인이 필요합니다', 'info')
    }).catch(() => {})
    setShowBuilder(false)
    setBuilderTemplate(null)
  }

  const approveRule = async (id: string) => {
    try {
      const res: any = await api.approvePolicyRule(id)
      showToast(`승인됨 · 에포크 #${res?.epoch?.epoch_number} 발행`, 'success')
      reloadAll()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const rejectRule = async (id: string) => {
    if (!await confirm({ title: '거부', message: '이 초안을 거부하고 삭제하시겠습니까?', danger: true })) return
    try { await api.rejectPolicyRule(id); showToast('거부됨', 'info'); reloadAll() } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const toggleRule = (r: Rule) => {
    if (r.status !== 'approved') { showToast('초안은 승인 후에만 토글할 수 있습니다', 'error'); return }
    api.createPolicyRule({ ...r, enabled: !r.enabled }).then((res: any) => {
      showToast(!r.enabled ? '활성화됨 · 에포크 재발행' : '비활성화됨 · 에포크 재발행', 'info')
      reloadAll()
    }).catch(() => {})
  }

  const deleteRule = async (r: Rule) => {
    if (!await confirm({ title: '확인', message: `'${r.name}' 정책을 삭제하시겠습니까?${r.status === 'approved' ? ' 활성 에포크가 재발행됩니다.' : ''}`, danger: true })) return
    api.deletePolicyRule(r.id).then(() => { reloadAll(); showToast('정책 삭제됨', 'info') }).catch(() => {})
  }

  const bulkToggle = async (enabled: boolean) => {
    const ids = [...selected].filter(id => rules.find(r => r.id === id && r.status === 'approved'))
    if (ids.length === 0) { showToast('승인된 규칙을 선택하세요', 'error'); return }
    try { await api.bulkPolicyRules(ids, enabled); showToast(`${ids.length}개 규칙 ${enabled ? '활성화' : '비활성화'} · 에포크 재발행`, 'success'); setSelected(new Set()); reloadAll() } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const loadEffective = async () => {
    try {
      const res: any = await api.getEffectivePolicy({ project_id: effProject || undefined, repo_id: effRepo || undefined })
      setEffective(res)
    } catch { showToast('실패했습니다 · action failed', 'error') }
  }
  useEffect(() => { if (activeLayer !== null) loadEffective() }, [activeLayer, effProject, effRepo])

  const openDiff = async (a: string, b: string) => {
    if (!a || !b) { showToast('비교할 두 에포크를 선택하세요', 'error'); return }
    try { setDiff(await api.getEpochDiff(a, b)); setDiffModal({ a, b }) } catch (err: any) { showToast(err.message, 'error') }
  }

  const openAcks = async (epoch: any) => {
    setAcksModal(epoch)
    try { setAcks(await api.listEpochAcks(epoch.epoch_id)) } catch { setAcks([]) }
  }

  const requireAck = async (epoch: any) => {
    if (!await confirm({ title: '확인 캠페인 시작', message: `에포크 #${epoch.epoch_number}에 사용자 확인을 요구하시겠습니까? 미확인 사용자의 새 세션이 차단됩니다. (§33.6)`, danger: true })) return
    try { await api.requireEpochAck(epoch.epoch_id); showToast('확인 요구 설정됨', 'success'); reloadAll() } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const runSimulation = async () => {
    const ids = [...simRuleIds]
    if (ids.length === 0) { showToast('시뮬레이션할 규칙을 선택하세요', 'error'); return }
    try { setSimResult(await api.simulatePolicy(ids)) } catch (err: any) { showToast(err.message, 'error') }
  }

  const createPack = async () => {
    if (!packForm.name) { showToast('팩 이름을 입력하세요', 'error'); return }
    try {
      await api.createPolicyPack(packForm)
      showToast('팩 생성됨', 'success')
      setPackModal(false)
      setPackForm({ name: '', name_ko: '', version: '1', profile: 'enterprise', rule_ids: [] })
      reloadAll()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const submitAssign = async () => {
    if (!assignPack) return
    try {
      await api.assignPolicyPack(assignPack.id, assignForm.scope, assignForm.scope_id)
      showToast('팩 할당됨', 'success')
      setAssignPack(null)
      reloadAll()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const exportPack = async (pack: any) => {
    try {
      const doc: any = await api.exportPolicyPack(pack.id)
      const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${pack.name}-v${pack.version}.json`
      a.click()
      URL.revokeObjectURL(url)
    } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const importPack = async (file: File) => {
    try {
      const doc = JSON.parse(await file.text())
      await api.importPolicyPack(doc)
      showToast('팩 가져옴', 'success')
      reloadAll()
    } catch (err: any) { showToast('가져오기 실패: ' + err.message, 'error') }
  }

  const submitException = async () => {
    if (!exceptionForm.reason || exceptionForm.rule_ids.length === 0) { showToast('사유와 규칙을 선택하세요', 'error'); return }
    try {
      await api.createPolicyException({ ...exceptionForm, requested_by: 'admin' })
      showToast('예외 요청됨 — 승인 대기', 'success')
      setExceptionModal(false)
      setExceptionForm({ scope: 'project', scope_id: '', scopeName: '', reason: '', rule_ids: [] })
      reloadAll()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const decideException = async (ex: any, approve: boolean) => {
    try {
      await api.decidePolicyException(ex.id, approve, 'admin')
      showToast(approve ? '예외 승인됨' : '예외 거부됨', 'info')
      reloadAll()
    } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const saveTemplateEdit = async () => {
    if (!templateEdit) return
    try {
      await api.savePolicyTemplate({ ...templateEdit, ...templateForm, config: JSON.parse(templateForm.config || '{}') })
      showToast('템플릿 저장됨', 'success')
      setTemplateEdit(null)
      reloadAll()
    } catch (err: any) { showToast('저장 실패: ' + err.message, 'error') }
  }

  const openTemplateEdit = (t: any) => {
    setTemplateEdit(t)
    setTemplateForm({ name: t.name, nameEn: t.nameEn, desc: t.desc, config: t.config || '{}', version: t.version })
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">거버넌스 정책 <span className="text-gray-400 text-lg font-normal">Governance Policy</span></h1>
      <p className="text-xs text-gray-400 mb-6">
        서버 저장 · 승인 워크플로 · 에포크 발행 · {enforcedCount}개 시행 중
        {draftCount > 0 && <span className="text-yellow-600 ml-2">· {draftCount}개 승인 대기</span>}
      </p>

      {/* Interactive policy hierarchy (B1/UX11) — click a layer to see effective rules */}
      <div className="card mb-6 py-3 px-4">
        <div className="flex items-center justify-center gap-2 text-xs text-gray-500 flex-wrap">
          {LAYERS.map((layer, i) => (
            <span key={layer} className="flex items-center gap-2">
              <button
                onClick={() => setActiveLayer(activeLayer === i ? null : i)}
                className={`px-2 py-1 rounded transition-colors ${activeLayer === i ? 'bg-blue-600 text-white' : i === 0 ? 'bg-red-50 text-red-600 hover:bg-red-100' : i < 3 ? 'bg-blue-50 text-blue-600 hover:bg-blue-100' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}
              >
                {layer}
              </button>
              {i < 8 && <span className="text-gray-300">→</span>}
            </span>
          ))}
        </div>
        <p className="text-center text-[10px] text-gray-400 mt-2">하위 계층은 상위 정책을 강화할 수 있음 · 약화는 예외 승인 필요 — 계층 클릭 → 유효 정책 확인</p>

        {activeLayer !== null && (
          <div className="mt-3 pt-3 border-t border-gray-100 expand-enter">
            <div className="flex items-center gap-2 mb-2 flex-wrap">
              <span className="text-xs font-semibold text-gray-600">{LAYERS[activeLayer]} 계층의 유효 정책</span>
              <EntitySelect entity="project" value={effProject} onChange={setEffProject} noneLabel="프로젝트: 전체" />
              <EntitySelect entity="repository" value={effRepo} onChange={setEffRepo} noneLabel="저장소: 전체" />
            </div>
            {effective ? (
              <div className="text-xs space-y-1">
                {effective.allowed_models && (
                  <div>허용 모델: <span className="font-mono">{effective.allowed_models.join(', ') || '(없음 — 전체 차단)'}</span></div>
                )}
                {Object.keys(effective).filter(k => k !== 'rules' && k !== 'allowed_models').map(k => (
                  <div key={k}>{DOMAIN_INFO[k]?.nameEn || k}: <span className="font-mono">{JSON.stringify(effective[k]).slice(0, 160)}</span></div>
                ))}
                <div className="text-gray-400 mt-1">적용 규칙: {effective.rules?.length || 0}개</div>
              </div>
            ) : <p className="text-xs text-gray-400">로딩 중...</p>}
          </div>
        )}
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200 flex-wrap">
        {[
          { id: 'active', label: '활성 정책', en: 'Rules', count: rules.length },
          { id: 'templates', label: '템플릿', en: 'Templates' },
          { id: 'epochs', label: '에포크 이력', en: 'Epochs' },
          { id: 'packs', label: '정책 팩', en: 'Packs' },
          { id: 'exceptions', label: '예외 마켓', en: 'Exceptions' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && <span className="text-xs text-gray-400">({t.count})</span>}
          </button>
        ))}
      </div>

      {/* ACTIVE RULES */}
      {tab === 'active' && (
        <div>
          <div className="flex justify-between items-center mb-4 flex-wrap gap-2">
            <div className="flex items-center gap-2 flex-wrap">
              <input className="input max-w-[220px]" placeholder="정책 내용 검색..." value={search} onChange={e => setSearch(e.target.value)} />
              <select className="input max-w-[160px] text-xs" value={domainFilter} onChange={e => setDomainFilter(e.target.value)}>
                <option value="">영역: 전체</option>
                {Object.entries(DOMAIN_INFO).map(([k, v]) => <option key={k} value={k}>{v.nameEn}</option>)}
              </select>
              {selected.size > 0 && (
                <>
                  <button onClick={() => bulkToggle(true)} className="btn-sm btn-secondary">선택 활성화</button>
                  <button onClick={() => bulkToggle(false)} className="btn-sm btn-secondary">선택 비활성화</button>
                  <button onClick={() => setSelected(new Set())} className="btn-sm btn-secondary">취소</button>
                </>
              )}
            </div>
            <button onClick={() => { setShowBuilder(!showBuilder); setBuilderDomain(''); setBuilderTemplate(null) }} className="btn-primary text-sm">+ 정책 추가</button>
          </div>

          {/* Builder */}
          {showBuilder && (
            <div className="card mb-6 expand-enter">
              <div className="flex justify-between items-center mb-3">
                <h3 className="text-sm font-semibold">새 정책 만들기</h3>
                <button onClick={() => { setShowBuilder(false); setBuilderDomain(''); setBuilderTemplate(null); setBuilderScope('org'); setBuilderScopeId('') }} className="text-xs text-gray-400 hover:text-gray-600">초기화 · Reset</button>
              </div>
              <div className="space-y-4">
                <div>
                  <label className="label">정책 영역 · Domain</label>
                  <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                    {Object.entries(DOMAIN_INFO).map(([id, d]) => (
                      <button key={id} onClick={() => { setBuilderDomain(id); setBuilderTemplate(null) }}
                        className={`p-3 rounded-lg text-left text-sm border transition-all ${builderDomain === id ? 'border-blue-400 bg-blue-50' : 'border-gray-200 hover:bg-gray-50'}`}>
                        <span className="text-lg mr-1">{d.icon}</span>
                        <span className="font-medium">{d.name}</span>
                        <span className="text-xs text-gray-400 block">{d.nameEn}</span>
                      </button>
                    ))}
                  </div>
                </div>

                {builderDomain && (
                  <div>
                    <label className="label">템플릿 선택 · Template (서버 카탈로그)</label>
                    <div className="space-y-2">
                      {(templatesByDomain[builderDomain] || []).map(t => (
                        <button key={t.template_id} onClick={() => setBuilderTemplate(t)}
                          className={`w-full p-3 rounded-lg text-left border transition-all ${builderTemplate?.template_id === t.template_id ? 'border-blue-400 bg-blue-50' : 'border-gray-200 hover:bg-gray-50'}`}>
                          <div className="flex items-center justify-between">
                            <div>
                              <span className="text-sm font-medium">{t.name}</span>
                              <span className="text-xs text-gray-400 ml-2">{t.nameEn} v{t.version}</span>
                            </div>
                            <button onClick={e => { e.stopPropagation(); openTemplateEdit(t) }} className="text-xs text-gray-400 hover:text-blue-600">편집</button>
                          </div>
                          <p className="text-xs text-gray-500 mt-1">{t.desc}</p>
                          <pre className="text-[10px] text-gray-400 mt-1 bg-gray-50 rounded p-1 overflow-x-auto">{String(t.config).slice(0, 200)}</pre>
                        </button>
                      ))}
                    </div>
                  </div>
                )}

                {builderTemplate && (
                  <>
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="label">적용 범위 · Scope</label>
                        <select className="input" value={builderScope} onChange={e => { setBuilderScope(e.target.value); setBuilderScopeId('') }}>
                          <option value="org">전체 조직</option>
                          <option value="project">특정 프로젝트</option>
                          <option value="repo">특정 저장소</option>
                        </select>
                      </div>
                      <div>
                        <label className="label">범위 대상</label>
                        {builderScope === 'project' && <EntitySelect entity="project" value={builderScopeId} onChange={setBuilderScopeId} />}
                        {builderScope === 'repo' && <EntitySelect entity="repository" value={builderScopeId} onChange={setBuilderScopeId} />}
                        {builderScope === 'org' && <input className="input" value="전체 조직" disabled />}
                      </div>
                    </div>
                    <button onClick={() => addRuleFromTemplate(builderTemplate, builderScope, builderScopeId, builderScope === 'org' ? '전체 조직' : builderScopeId)} className="btn-primary text-sm">초안 저장 · Save Draft</button>
                    <p className="text-[10px] text-gray-400">초안은 승인 전까지 시행되지 않습니다. 승인 시 새 에포크가 발행됩니다.</p>
                  </>
                )}
              </div>
            </div>
          )}

          {/* Rules list */}
          {filteredRules.length === 0 ? (
            <div className="card text-center py-12">
              <EmptyState icon="⚖" title="정책이 없습니다" message="템플릿 탭에서 정책을 선택하거나 + 정책 추가로 시작하세요" action={{ label: '+ 정책 추가', onClick: () => setShowBuilder(true) }} />
            </div>
          ) : (
            <div className="space-y-2">
              {filteredRules.map(r => {
                const info = DOMAIN_INFO[r.domain] || { nameEn: r.domain, icon: '·' }
                const enforced = r.status === 'approved' && r.enabled
                return (
                  <div key={r.id} className={`card flex items-center gap-3 py-3 px-4 ${r.status === 'draft' ? 'border-l-4 border-l-yellow-400' : 'border-l-4 border-l-transparent'}`}>
                    <input type="checkbox" checked={selected.has(r.id)} onChange={() => { const n = new Set(selected); if (n.has(r.id)) n.delete(r.id); else n.add(r.id); setSelected(n) }} />
                    <FavoriteStar entity="policy_rules" id={r.id} onToggle={() => toggle(r.id)} />
                    <span className="text-xl">{info.icon}</span>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-sm font-medium">{r.name}</span>
                        <span className="text-xs text-gray-400">{r.nameEn}</span>
                        {r.status === 'draft' && <span className="badge-yellow text-[10px]">승인 대기 · Draft</span>}
                        {enforced && <span className="badge-green text-[10px]">시행 중 · Enforced</span>}
                        {r.status === 'approved' && !r.enabled && <span className="badge-gray text-[10px]">비활성 · Off</span>}
                      </div>
                      <p className="text-xs text-gray-500 truncate">{r.desc}</p>
                      <div className="text-[10px] text-gray-400 mt-0.5">
                        범위: {r.scopeName || '전체 조직'} · 영역: {info.nameEn}
                        {activeEpoch && <span className="ml-2">· 에포크 #{activeEpoch.epoch_number}</span>}
                      </div>
                    </div>
                    {r.status === 'draft' ? (
                      <div className="flex gap-2 flex-shrink-0">
                        <button onClick={() => approveRule(r.id)} className="btn-sm btn-primary">승인</button>
                        <button onClick={() => rejectRule(r.id)} className="btn-sm btn-secondary">거부</button>
                      </div>
                    ) : (
                      <button onClick={() => toggleRule(r)}
                        className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors flex-shrink-0 ${r.enabled ? 'bg-patty-600' : 'bg-gray-300'}`}>
                        <span className={`inline-block h-3 w-3 rounded-full bg-white transition-transform ${r.enabled ? 'translate-x-5' : 'translate-x-1'}`} />
                      </button>
                    )}
                    <button onClick={() => deleteRule(r)} className="text-xs text-red-600 hover:underline flex-shrink-0">삭제</button>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* TEMPLATES */}
      {tab === 'templates' && (
        <div>
          <p className="text-xs text-gray-400 mb-4">서버 템플릿 카탈로그 · 편집/버전 관리 가능 · 클릭하여 활성화</p>
          {Object.entries(DOMAIN_INFO).map(([domainId, info]) => (
            <div key={domainId} className="mb-6">
              <div className="flex items-center gap-2 mb-3">
                <span className="text-xl">{info.icon}</span>
                <h3 className="text-sm font-semibold">{info.name} <span className="text-gray-400 font-normal">{info.nameEn}</span></h3>
                <span className="text-xs text-gray-400 ml-auto">{info.desc}</span>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                {(templatesByDomain[domainId] || []).map(t => {
                  const isActive = rules.some(r => r.template_id === t.template_id && r.status === 'approved' && r.enabled)
                  return (
                    <div key={t.template_id} className={`card border-l-4 ${isActive ? 'border-l-green-500' : 'border-l-gray-300'}`}>
                      <div className="flex items-start justify-between mb-2">
                        <div>
                          <h4 className="text-sm font-medium">{t.name}</h4>
                          <p className="text-xs text-gray-400">{t.nameEn} · v{t.version}</p>
                        </div>
                        {isActive && <span className="badge-green text-[10px]">활성</span>}
                      </div>
                      <p className="text-xs text-gray-500 mb-3">{t.desc}</p>
                      <div className="flex gap-2">
                        <button onClick={() => addRuleFromTemplate(t)} className="text-xs text-blue-600 hover:underline">{isActive ? '다시 추가' : '+ 활성화 (초안)'}</button>
                        <button onClick={() => openTemplateEdit(t)} className="text-xs text-gray-400 hover:underline ml-auto">편집</button>
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* EPOCHS */}
      {tab === 'epochs' && (
        <div className="card">
          <div className="flex justify-between items-center mb-3 flex-wrap gap-2">
            <h3 className="text-sm font-semibold">정책 에포크 이력 · Epoch History ({epochs.length})</h3>
            <div className="flex gap-2 flex-wrap">
              <button onClick={() => setSimRuleIds(new Set(rules.filter(r => r.status === 'approved' && r.enabled).map(r => r.id)))} className="btn-sm btn-secondary">📊 시뮬레이션 준비</button>
            </div>
          </div>
          <p className="text-xs text-gray-400 mb-4">규칙 승인/변경 시 새 에포크가 발행됩니다. 하네스는 에포크를 참조하여 현재 정책을 확인합니다. {activeEpoch?.requires_ack && <span className="text-yellow-600">· 현재 에포크는 사용자 확인(ack)이 필요합니다</span>}</p>
          {epochs.length === 0 ? (
            <p className="text-gray-400 text-center py-8">에포크 이력이 없습니다 — 첫 규칙을 승인하면 발행됩니다</p>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                <th className="pb-3">#</th><th className="pb-3">에포크 ID</th><th className="pb-3">전환 모드</th><th className="pb-3">허용 모델</th><th className="pb-3">Ack</th><th className="pb-3">상태</th><th className="pb-3">생성일</th><th className="pb-3">작업</th>
              </tr></thead>
              <tbody>
                {epochs.map((e, i) => (
                  <tr key={e.epoch_id} className="border-b border-gray-100 last:border-0 hover:bg-gray-50">
                    <td className="py-3 text-xs font-semibold">{e.epoch_number}</td>
                    <td className="py-3 font-mono text-xs">{e.epoch_id?.slice(0, 24)}</td>
                    <td className="py-3 text-xs"><span className="badge-gray">{e.transition_mode || 'immediate'}</span></td>
                    <td className="py-3 text-xs text-gray-500 max-w-[220px] truncate">{Array.isArray(e.allowed_models) ? e.allowed_models.join(', ') : e.allowed_models || '-'}</td>
                    <td className="py-3 text-xs">{e.requires_ack ? <span className="badge-yellow">필요</span> : <span className="text-gray-400">-</span>}</td>
                    <td className="py-3 text-xs">{e.status === 'active' ? <span className="badge-green">활성</span> : <span className="badge-gray">{e.status}</span>}</td>
                    <td className="py-3 text-xs text-gray-400">{formatRelative(e.created_at || e.effective_at)}</td>
                    <td className="py-3 text-xs">
                      <div className="flex gap-2">
                        {i < epochs.length - 1 && (
                          <button onClick={() => openDiff(e.epoch_id, epochs[i + 1].epoch_id)} className="text-blue-600 hover:underline">diff</button>
                        )}
                        <button onClick={() => openAcks(e)} className="text-blue-600 hover:underline">ack 현황</button>
                        {e.status === 'active' && !e.requires_ack && (
                          <button onClick={() => requireAck(e)} className="text-yellow-600 hover:underline">ack 요구</button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {/* Simulation panel (C3) */}
          {simRuleIds.size > 0 && (
            <div className="mt-4 pt-4 border-t border-gray-100 expand-enter">
              <h4 className="text-sm font-semibold mb-2">정책 시뮬레이션 · Simulation (§15.5)</h4>
              <p className="text-xs text-gray-400 mb-2">선택된 규칙을 과거 세션에 적용해 차단 영향을 예측합니다.</p>
              <div className="flex items-center gap-2 flex-wrap mb-3">
                {rules.map(r => (
                  <label key={r.id} className="flex items-center gap-1 text-xs">
                    <input type="checkbox" checked={simRuleIds.has(r.id)} onChange={() => { const n = new Set(simRuleIds); if (n.has(r.id)) n.delete(r.id); else n.add(r.id); setSimRuleIds(n) }} />
                    {r.name}
                  </label>
                ))}
                <button onClick={runSimulation} className="btn-sm btn-primary ml-auto">실행</button>
              </div>
              {simResult && (
                <div className="text-xs space-y-1 bg-gray-50 rounded p-3">
                  <div>허용 유지: <span className="text-green-600 font-semibold">{simResult.would_allow}</span> · 차단 예상: <span className="text-red-600 font-semibold">{simResult.would_block}</span> · 승인 필요: <span className="text-yellow-600 font-semibold">{simResult.would_require_approval}</span></div>
                  <div>영향 사용자: {simResult.affected_users?.length ?? 0}명 · 영향 저장소: {simResult.affected_repos?.length ?? 0}개</div>
                  <div>개발자 마찰: {simResult.developer_friction || '-'} · 오탐 추정: {simResult.false_positive_estimate ?? 0} · 예외 필요: {simResult.exceptions_needed ?? 0}</div>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* PACKS */}
      {tab === 'packs' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <p className="text-xs text-gray-400">규칙을 버전 팩으로 묶어 조직/프로젝트에 할당 (§41)</p>
            <div className="flex gap-2">
              <label className="btn-sm btn-secondary cursor-pointer">📥 팩 가져오기<input type="file" accept="application/json" className="hidden" onChange={e => { const f = e.target.files?.[0]; if (f) importPack(f) }} /></label>
              <button onClick={() => { setPackModal(true); setPackForm({ name: '', name_ko: '', version: '1', profile: 'enterprise', rule_ids: [] }) }} className="btn-primary text-sm">+ 팩 생성</button>
            </div>
          </div>
          {packs.length === 0 ? (
            <div className="card text-center py-12">
              <EmptyState icon="📦" title="정책 팩이 없습니다" message="승인된 규칙들을 팩으로 묶어 프로젝트에 할당하세요" action={{ label: '+ 팩 생성', onClick: () => setPackModal(true) }} />
            </div>
          ) : (
            <div className="space-y-2">
              {packs.map(p => (
                <div key={p.id} className="card flex items-center gap-3 py-3 px-4">
                  <span className="text-xl">📦</span>
                  <div className="flex-1">
                    <div className="text-sm font-medium">{p.name} {p.name_ko && <span className="text-xs text-gray-400">({p.name_ko})</span>} <span className="badge-blue text-[10px]">v{p.version}</span></div>
                    <div className="text-[10px] text-gray-400 font-mono">{(p.digest || '').slice(0, 28)} · {p.profile}</div>
                  </div>
                  <button onClick={() => setAssignPack(p)} className="btn-sm btn-secondary">할당</button>
                  <button onClick={() => exportPack(p)} className="btn-sm btn-secondary">내보내기</button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* EXCEPTIONS */}
      {tab === 'exceptions' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <p className="text-xs text-gray-400">예외 마켓플레이스 · §33.8 — 범위별 정책 예외를 신청하고 승인합니다</p>
            <button onClick={() => { setExceptionModal(true); setExceptionForm({ scope: 'project', scope_id: '', scopeName: '', reason: '', rule_ids: [] }) }} className="btn-primary text-sm">+ 예외 신청</button>
          </div>
          {exceptions.length === 0 ? (
            <div className="card text-center py-12">
              <EmptyState icon="🔓" title="예외 요청이 없습니다" message="정책을 완화해야 하는 범위가 있다면 예외를 신청하세요" />
            </div>
          ) : (
            <div className="space-y-2">
              {exceptions.map(ex => (
                <div key={ex.id} className={`card flex items-center gap-3 py-3 px-4 ${ex.status === 'pending' ? 'border-l-4 border-l-yellow-400' : ''}`}>
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{ex.scopeName || ex.scope_id || ex.scope}</span>
                      <span className={ex.status === 'approved' ? 'badge-green' : ex.status === 'denied' ? 'badge-red' : 'badge-yellow'}>{ex.status}</span>
                    </div>
                    <p className="text-xs text-gray-500 mt-0.5">{ex.reason}</p>
                    <div className="text-[10px] text-gray-400">규칙: {JSON.parse(ex.rule_ids || '[]').length}개 · 신청: {formatRelative(ex.created_at)}</div>
                  </div>
                  {ex.status === 'pending' && (
                    <div className="flex gap-2">
                      <button onClick={() => decideException(ex, true)} className="btn-sm btn-primary">승인</button>
                      <button onClick={() => decideException(ex, false)} className="btn-sm btn-secondary">거부</button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Epoch diff modal */}
      <Modal open={!!diffModal} title="에포크 비교 · Epoch Diff" subtitle={`${diffModal?.a?.slice(0, 16)} ↔ ${diffModal?.b?.slice(0, 16)}`} onClose={() => setDiffModal(null)} size="lg"
        footer={<ModalFooter onCancel={() => setDiffModal(null)} onConfirm={() => setDiffModal(null)} confirmLabel="닫기" />}>
        {diff ? (
          <div className="text-xs space-y-3">
            {(diff.domains?.allowed_models?.changed || diff.domains?.domain_policies?.changed || diff.domains?.requires_ack?.changed) ? (
              <>
                {diff.domains.allowed_models?.changed && (
                  <div className="bg-gray-50 rounded p-3">
                    <div className="font-semibold mb-1">허용 모델 변화</div>
                    {diff.domains.allowed_models.added?.length > 0 && <div className="text-green-600">+ 추가: {diff.domains.allowed_models.added.join(', ')}</div>}
                    {diff.domains.allowed_models.removed?.length > 0 && <div className="text-red-600">- 제거: {diff.domains.allowed_models.removed.join(', ')}</div>}
                  </div>
                )}
                {diff.domains.domain_policies?.changed && (
                  <div className="bg-gray-50 rounded p-3">
                    <div className="font-semibold mb-1">도메인 정책 변화</div>
                    <div className="grid grid-cols-2 gap-2">
                      <div><div className="text-gray-400">이전</div><pre className="text-[10px] overflow-x-auto">{String(diff.domains.domain_policies.before).slice(0, 400)}</pre></div>
                      <div><div className="text-gray-400">이후</div><pre className="text-[10px] overflow-x-auto">{String(diff.domains.domain_policies.after).slice(0, 400)}</pre></div>
                    </div>
                  </div>
                )}
                {diff.domains.requires_ack?.changed && <div>Ack 요구: {String(diff.domains.requires_ack.before)} → {String(diff.domains.requires_ack.after)}</div>}
              </>
            ) : <p className="text-gray-400">두 에포크 간 차이가 없습니다</p>}
          </div>
        ) : <p className="text-xs text-gray-400">로딩 중...</p>}
      </Modal>

      {/* Ack campaign modal */}
      <Modal open={!!acksModal} title="확인 캠페인 현황 · Acknowledgements" subtitle={`에포크 #${acksModal?.epoch_number}`} onClose={() => setAcksModal(null)} size="lg"
        footer={<ModalFooter onCancel={() => setAcksModal(null)} onConfirm={() => setAcksModal(null)} confirmLabel="닫기" />}>
        {acks.length === 0 ? <p className="text-xs text-gray-400">사용자 없음</p> : (
          <div className="space-y-1 max-h-72 overflow-y-auto">
            {acks.map(a => (
              <div key={a.user_id} className="flex items-center gap-3 text-xs p-2 bg-gray-50 rounded">
                <Link to={`/users/${a.user_id}`} className="text-blue-600 hover:underline font-medium">{a.name_ko || a.name}</Link>
                <span className="text-gray-400">{a.email}</span>
                <span className={a.acked ? 'badge-green ml-auto' : 'badge-yellow ml-auto'}>{a.acked ? '확인됨' : '미확인'}</span>
                {a.acked_at && <span className="text-gray-400">{formatRelative(a.acked_at)}</span>}
              </div>
            ))}
          </div>
        )}
      </Modal>

      {/* Pack create modal */}
      <Modal open={packModal} title="정책 팩 생성 · Create Pack" onClose={() => setPackModal(false)} size="md"
        footer={<ModalFooter onCancel={() => setPackModal(false)} onConfirm={createPack} confirmLabel="생성" disabled={!packForm.name} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">이름 · Name</label><input className="input" value={packForm.name} onChange={e => setPackForm({ ...packForm, name: e.target.value })} placeholder="enterprise-base" /></div>
            <div><label className="label">한글명</label><input className="input" value={packForm.name_ko} onChange={e => setPackForm({ ...packForm, name_ko: e.target.value })} /></div>
            <div><label className="label">버전</label><input className="input" value={packForm.version} onChange={e => setPackForm({ ...packForm, version: e.target.value })} /></div>
            <div><label className="label">프로필</label><select className="input" value={packForm.profile} onChange={e => setPackForm({ ...packForm, profile: e.target.value })}>
              <option value="enterprise">enterprise</option><option value="government">government</option>
            </select></div>
          </div>
          <div>
            <label className="label">포함 규칙 (비우면 승인된 전체)</label>
            <div className="space-y-1 max-h-40 overflow-y-auto border border-gray-200 rounded p-2">
              {rules.filter(r => r.status === 'approved').map(r => (
                <label key={r.id} className="flex items-center gap-2 text-xs cursor-pointer">
                  <input type="checkbox" checked={packForm.rule_ids.includes(r.id)} onChange={() => { const ids = [...packForm.rule_ids]; const i = ids.indexOf(r.id); if (i >= 0) ids.splice(i, 1); else ids.push(r.id); setPackForm({ ...packForm, rule_ids: ids }) }} />
                  {r.name} <span className="text-gray-400">({DOMAIN_INFO[r.domain]?.nameEn})</span>
                </label>
              ))}
            </div>
          </div>
        </div>
      </Modal>

      {/* Pack assign modal */}
      <Modal open={!!assignPack} title="팩 할당 · Assign Pack" subtitle={assignPack?.name} onClose={() => setAssignPack(null)} size="sm"
        footer={<ModalFooter onCancel={() => setAssignPack(null)} onConfirm={submitAssign} confirmLabel="할당" />}>
        <div className="space-y-3">
          <div><label className="label">범위 · Scope</label>
            <select className="input" value={assignForm.scope} onChange={e => setAssignForm({ scope: e.target.value, scope_id: '' })}>
              <option value="org">전체 조직</option>
              <option value="project">특정 프로젝트</option>
            </select>
          </div>
          {assignForm.scope === 'project' && (
            <div><label className="label">프로젝트</label><EntitySelect entity="project" value={assignForm.scope_id} onChange={v => setAssignForm({ ...assignForm, scope_id: v })} /></div>
          )}
        </div>
      </Modal>

      {/* Exception modal */}
      <Modal open={exceptionModal} title="예외 신청 · Request Exception" onClose={() => setExceptionModal(false)} size="md"
        footer={<ModalFooter onCancel={() => setExceptionModal(false)} onConfirm={submitException} confirmLabel="신청" disabled={!exceptionForm.reason || exceptionForm.rule_ids.length === 0} />}>
        <div className="space-y-3">
          <div><label className="label">범위 · Scope</label>
            <select className="input" value={exceptionForm.scope} onChange={e => setExceptionForm({ ...exceptionForm, scope: e.target.value, scope_id: '' })}>
              <option value="project">프로젝트</option>
              <option value="repo">저장소</option>
              <option value="user">사용자</option>
            </select>
          </div>
          <div><label className="label">대상</label>
            {exceptionForm.scope === 'project' && <EntitySelect entity="project" value={exceptionForm.scope_id} onChange={v => setExceptionForm({ ...exceptionForm, scope_id: v, scopeName: v })} />}
            {exceptionForm.scope === 'repo' && <EntitySelect entity="repository" value={exceptionForm.scope_id} onChange={v => setExceptionForm({ ...exceptionForm, scope_id: v, scopeName: v })} />}
            {exceptionForm.scope === 'user' && <EntitySelect entity="user" value={exceptionForm.scope_id} onChange={v => setExceptionForm({ ...exceptionForm, scope_id: v, scopeName: v })} />}
          </div>
          <div><label className="label">사유 · Reason</label><textarea className="input" rows={2} value={exceptionForm.reason} onChange={e => setExceptionForm({ ...exceptionForm, reason: e.target.value })} placeholder="예: 레거시 마이그레이션 기간 동안 필요" /></div>
          <div><label className="label">제외할 규칙</label>
            <div className="space-y-1 max-h-40 overflow-y-auto border border-gray-200 rounded p-2">
              {rules.filter(r => r.status === 'approved' && r.enabled).map(r => (
                <label key={r.id} className="flex items-center gap-2 text-xs cursor-pointer">
                  <input type="checkbox" checked={exceptionForm.rule_ids.includes(r.id)} onChange={() => { const ids = [...exceptionForm.rule_ids]; const i = ids.indexOf(r.id); if (i >= 0) ids.splice(i, 1); else ids.push(r.id); setExceptionForm({ ...exceptionForm, rule_ids: ids }) }} />
                  {r.name}
                </label>
              ))}
            </div>
          </div>
        </div>
      </Modal>

      {/* Template edit modal */}
      <Modal open={!!templateEdit} title="템플릿 편집 · Edit Template" subtitle={`${templateEdit?.template_id} v${templateEdit?.version}`} onClose={() => setTemplateEdit(null)} size="md"
        footer={<ModalFooter onCancel={() => setTemplateEdit(null)} onConfirm={saveTemplateEdit} confirmLabel="저장" />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">이름</label><input className="input" value={templateForm.name} onChange={e => setTemplateForm({ ...templateForm, name: e.target.value })} /></div>
            <div><label className="label">영문명</label><input className="input" value={templateForm.nameEn} onChange={e => setTemplateForm({ ...templateForm, nameEn: e.target.value })} /></div>
            <div><label className="label">버전</label><input className="input" value={templateForm.version} onChange={e => setTemplateForm({ ...templateForm, version: e.target.value })} /></div>
            <div><label className="label">설명</label><input className="input" value={templateForm.desc} onChange={e => setTemplateForm({ ...templateForm, desc: e.target.value })} /></div>
          </div>
          <div><label className="label">설정 JSON</label><textarea className="input font-mono text-xs" rows={8} value={templateForm.config} onChange={e => setTemplateForm({ ...templateForm, config: e.target.value })} /></div>
        </div>
      </Modal>
    </div>
  )
}
