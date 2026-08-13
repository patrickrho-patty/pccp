import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'

// Policy domains per PRD §13 — each has real, usable templates
const POLICY_DOMAINS = [
  {
    id: 'models',
    name: '모델 접근 정책',
    nameEn: 'Model Access',
    icon: '◆',
    desc: '조직/부서/프로젝트별 허용 모델 제어',
    prdRef: '§9, §10A',
    templates: [
      { id: 'restrict-models', name: '모델 제한', nameEn: 'Restrict Models', desc: '특정 모델만 허용 (예: Standard만)', config: { allowed_models: ['patty-code-standard'], denied_models: [] } },
      { id: 'production-models', name: '프로덕션 전용', nameEn: 'Production Only', desc: '프로덕션 환경에서 검증된 모델만', config: { allowed_models: ['patty-code-standard'], denied_models: ['patty-code-fast'] } },
      { id: 'no-vision', name: '비전 모델 차단', nameEn: 'No Vision Models', desc: '이미지 입력 지원 모델 비활성화', config: { deny_capabilities: ['image'] } },
    ]
  },
  {
    id: 'tools',
    name: '도구 권한 정책',
    nameEn: 'Tool Permissions',
    icon: '🔧',
    desc: '하네스가 사용할 수 있는 도구와 승인 규칙',
    prdRef: '§17',
    templates: [
      { id: 'strict-tools', name: '엄격 모드', nameEn: 'Strict Mode', desc: '모든 쓰기/실행 도구 승인 필요', config: { require_approval_for: ['file.write', 'file.delete', 'shell.execute'], auto_approve: ['file.read', 'search.code'] } },
      { id: 'read-only', name: '읽기 전용', nameEn: 'Read Only', desc: '쓰기/실행 완전 차단, 읽기/검색만', config: { block_all: ['file.write', 'file.delete', 'shell.execute', 'git.push', 'package.install'], allow: ['file.read', 'search.code', 'test.run'] } },
      { id: 'no-network', name: '네트워크 차단', nameEn: 'No Network', desc: '외부 HTTP 요청 차단', config: { block_all: ['network.http'], reason: '보안: 외부 데이터 유출 방지' } },
      { id: 'no-mcp', name: 'MCP 서버 제한', nameEn: 'Restrict MCP', desc: '승인되지 않은 MCP 서버 차단', config: { require_mcp_allowlist: true } },
    ]
  },
  {
    id: 'data',
    name: '데이터 보호 정책',
    nameEn: 'Data Protection',
    icon: '🛡',
    desc: '민감 정보, 개인정보, 비밀번호 보호',
    prdRef: '§16',
    templates: [
      { id: 'kr-pii', name: '한국 PII 보호', nameEn: 'Korean PII Protection', desc: '주민번호, 사업자번호 자동 마스킹', config: { dlp_rules: ['pii-kr-rrn', 'pii-kr-business', 'pii-kr-phone'], action: 'mask' } },
      { id: 'secrets-block', name: '비밀정보 차단', nameEn: 'Block Secrets', desc: 'API 키, 토큰, 개인키 모델 입력 차단', config: { dlp_rules: ['secret-aws', 'secret-jwt', 'secret-private-key', 'secret-github'], action: 'block' } },
      { id: 'context-firewall', name: '컨텍스트 방화벽', nameEn: 'Context Firewall', desc: '외부 컨텐츠에서 인젝션 감지', config: { scan_context: true, block_injection: true } },
    ]
  },
  {
    id: 'scm',
    name: 'Git/SCM 정책',
    nameEn: 'Git/SCM Governance',
    icon: '🌿',
    desc: '브랜치 보호, 커밋 규칙, PR 승인',
    prdRef: '§18',
    templates: [
      { id: 'protected-main', name: '메인 브랜치 보호', nameEn: 'Protected Main', desc: 'main/release/prod 직접 푸시 금지', config: { protected_branches: ['main', 'release', 'prod'], require_pr: true } },
      { id: 'require-approval', name: 'AI 변경 승인', nameEn: 'AI Change Approval', desc: 'AI가 생성한 모든 커밋은 인간 승인 필요', config: { require_human_review: true, block_ai_direct_push: true } },
      { id: 'no-force-push', name: '강제 푸시 금지', nameEn: 'No Force Push', desc: '모든 브랜치 force push 차단', config: { block_force_push: true } },
    ]
  },
  {
    id: 'network',
    name: '네트워크 정책',
    nameEn: 'Network Access',
    icon: '🌐',
    desc: '외부 통신 대상 제한',
    prdRef: '§17.4',
    templates: [
      { id: 'allowlist', name: '접속 허용 목록', nameEn: 'Allowlist', desc: '지정된 도메인만 접속 허용', config: { mode: 'allowlist', allowed: ['npmjs.org', 'pypi.org', 'github.com'] } },
      { id: 'block-exfil', name: '데이터 유출 방지', nameEn: 'Anti-Exfiltration', desc: '대용량 업로드 차단', config: { max_upload_mb: 10, block_unknown: true } },
      { id: 'vpn-only', name: 'VPN 전용', nameEn: 'VPN Only', desc: 'VPN 네트워크 대역에서만 접속', config: { require_vpn: true, allowed_zones: ['corp-vpn'] } },
    ]
  },
  {
    id: 'session',
    name: '세션 정책',
    nameEn: 'Session Controls',
    icon: '⏱',
    desc: '세션 시간 제한, 동시성, 자동 종료',
    prdRef: '§14',
    templates: [
      { id: 'max-duration', name: '최대 세션 시간', nameEn: 'Max Duration', desc: '세션 최대 4시간 후 자동 종료', config: { max_duration_minutes: 240 } },
      { id: 'idle-timeout', name: '유휴 종료', nameEn: 'Idle Timeout', desc: '30분 미사용 시 자동 종료', config: { idle_timeout_minutes: 30 } },
      { id: 'auto-evidence', name: '자동 증거 수집', nameEn: 'Auto Evidence', desc: '세션 종료 시 자동으로 증거 번들 생성', config: { auto_evidence: true, retain_days: 90 } },
    ]
  },
]

type PolicyRule = {
  id: string
  domain: string
  template_id: string
  name: string
  nameEn: string
  desc: string
  scope: string // org, project, repo, team
  scopeName: string
  enabled: boolean
  config: any
  createdAt: string
}

export default function Policy() {
  const [rules, setRules] = useState<PolicyRule[]>([])
  const [epochs, setEpochs] = useState<any[]>([])
  const [tab, setTab] = useState<'active' | 'templates' | 'epochs'>('active')
  const [showBuilder, setShowBuilder] = useState(false)
  const [builderDomain, setBuilderDomain] = useState<string>('')
  const [builderTemplate, setBuilderTemplate] = useState<any>(null)
  const [builderScope, setBuilderScope] = useState('org')
  const [builderScopeName, setBuilderScopeName] = useState('전체 조직')

  const reloadRules = () => api.listPolicyRules().then(data => setRules(Array.isArray(data) ? data : []))

  useEffect(() => {
    api.listEpochs().then(data => setEpochs(Array.isArray(data) ? data : data || []))
    reloadRules()
  }, [])

  const addRule = (rule: PolicyRule) => {
    api.createPolicyRule(rule).then(() => { reloadRules(); showToast('정책 추가됨', 'success') }).catch(() => {})
    setShowBuilder(false)
    setBuilderTemplate(null)
  }

  const toggleRule = (id: string) => {
    const r = rules.find(x => x.id === id)
    if (r) api.createPolicyRule({ ...r, enabled: !r.enabled }).then(reloadRules).catch(() => {})
  }

  const deleteRule = (id: string) => {
    if (!confirm('이 정책을 삭제하시겠습니까?')) return
    api.deletePolicyRule(id).then(() => { reloadRules(); showToast('정책 삭제됨', 'info') }).catch(() => {})
  }

  const domainIcon: Record<string,string> = {}
  POLICY_DOMAINS.forEach(d => domainIcon[d.id] = d.icon)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">거버넌스 정책 <span className="text-gray-400 text-lg font-normal">Governance Policy</span></h1>
      <p className="text-xs text-gray-400 mb-6">조직 정책 계층: Patty 기본 → 프로필 → 조직 → 부서 → 프로젝트 → 저장소 · PRD §13.1</p>

      {/* Policy hierarchy visualization */}
      <div className="card mb-6 py-3 px-4">
        <div className="flex items-center justify-center gap-2 text-xs text-gray-500 flex-wrap">
          {['Patty 필수', '프로필', '조직', '계열사', '부서', '프로젝트', '저장소', '브랜치', '세션'].map((layer, i) => (
            <span key={layer} className="flex items-center gap-2">
              <span className={`px-2 py-1 rounded ${i === 0 ? 'bg-red-50 text-red-600' : i < 3 ? 'bg-blue-50 text-blue-600' : 'bg-gray-100 text-gray-600'}`}>{layer}</span>
              {i < 8 && <span className="text-gray-300">→</span>}
            </span>
          ))}
        </div>
        <p className="text-center text-[10px] text-gray-400 mt-2">하위 계층은 상위 정책을 강화할 수 있음 · 약화는 예외 승인 필요</p>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'active', label: '활성 정책', en: 'Active Rules', count: rules.filter(r => r.enabled).length },
          { id: 'templates', label: '정책 템플릿', en: 'Templates' },
          { id: 'epochs', label: '에포크 이력', en: 'Epoch History' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && <span className="text-xs text-gray-400">({t.count})</span>}
          </button>
        ))}
      </div>

      {/* Active Rules Tab */}
      {tab === 'active' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <p className="text-xs text-gray-400">{rules.length}개 정책 ({rules.filter(r => r.enabled).length}개 활성)</p>
            <button onClick={() => { setShowBuilder(!showBuilder); setBuilderDomain(''); setBuilderTemplate(null) }} className="btn-primary text-sm">+ 정책 추가</button>
          </div>

          {/* Builder */}
          {showBuilder && (
            <div className="card mb-6">
              <h3 className="text-sm font-semibold mb-3">새 정책 만들기</h3>
              <div className="space-y-4">
                <div>
                  <label className="label">정책 영역 · Domain</label>
                  <div className="grid grid-cols-3 gap-2">
                    {POLICY_DOMAINS.map(d => (
                      <button key={d.id} onClick={() => { setBuilderDomain(d.id); setBuilderTemplate(null) }}
                        className={`p-3 rounded-lg text-left text-sm border transition-all ${builderDomain === d.id ? 'border-blue-400 bg-blue-50' : 'border-gray-200 hover:bg-gray-50'}`}>
                        <span className="text-lg mr-1">{d.icon}</span>
                        <span className="font-medium">{d.name}</span>
                        <span className="text-xs text-gray-400 block">{d.nameEn}</span>
                      </button>
                    ))}
                  </div>
                </div>

                {builderDomain && (
                  <div>
                    <label className="label">템플릿 선택 · Template</label>
                    <div className="space-y-2">
                      {POLICY_DOMAINS.find(d => d.id === builderDomain)?.templates.map(t => (
                        <button key={t.id} onClick={() => setBuilderTemplate(t)}
                          className={`w-full p-3 rounded-lg text-left border transition-all ${builderTemplate?.id === t.id ? 'border-blue-400 bg-blue-50' : 'border-gray-200 hover:bg-gray-50'}`}>
                          <div className="flex items-center justify-between">
                            <div>
                              <span className="text-sm font-medium">{t.name}</span>
                              <span className="text-xs text-gray-400 ml-2">{t.nameEn}</span>
                            </div>
                          </div>
                          <p className="text-xs text-gray-500 mt-1">{t.desc}</p>
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
                        <select className="input" value={builderScope} onChange={e => setBuilderScope(e.target.value)}>
                          <option value="org">전체 조직</option>
                          <option value="project">특정 프로젝트</option>
                          <option value="repo">특정 저장소</option>
                          <option value="team">특정 팀</option>
                        </select>
                      </div>
                      <div>
                        <label className="label">범위 이름</label>
                        <input className="input" value={builderScopeName} onChange={e => setBuilderScopeName(e.target.value)} />
                      </div>
                    </div>
                    <button onClick={() => addRule({
                      id: `rule-${Date.now()}`,
                      domain: builderDomain,
                      template_id: builderTemplate.id,
                      name: builderTemplate.name,
                      nameEn: builderTemplate.nameEn,
                      desc: builderTemplate.desc,
                      scope: builderScope,
                      scopeName: builderScopeName,
                      enabled: true,
                      config: builderTemplate.config,
                      createdAt: new Date().toISOString(),
                    })} className="btn-primary text-sm">정책 활성화</button>
                  </>
                )}
              </div>
            </div>
          )}

          {/* Active rules list */}
          {rules.length === 0 ? (
            <div className="card text-center py-12">
              <p className="text-gray-400 mb-2">활성화된 정책이 없습니다</p>
              <p className="text-xs text-gray-400">정책 템플릿 탭에서 원하는 정책을 선택하세요</p>
            </div>
          ) : (
            <div className="space-y-2">
              {rules.map(r => (
                <div key={r.id} className="card flex items-center gap-4 py-3 px-4">
                  <span className="text-xl">{domainIcon[r.domain]}</span>
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{r.name}</span>
                      <span className="text-xs text-gray-400">{r.nameEn}</span>
                    </div>
                    <p className="text-xs text-gray-500">{r.desc}</p>
                    <div className="text-[10px] text-gray-400 mt-0.5">
                      범위: {r.scopeName} · 영역: {POLICY_DOMAINS.find(d => d.id === r.domain)?.name}
                    </div>
                  </div>
                  <button onClick={() => toggleRule(r.id)}
                    className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${r.enabled ? 'bg-patty-600' : 'bg-gray-300'}`}>
                    <span className={`inline-block h-3 w-3 rounded-full bg-white transition-transform ${r.enabled ? 'translate-x-5' : 'translate-x-1'}`} />
                  </button>
                  <button onClick={() => deleteRule(r.id)} className="text-xs text-red-600 hover:underline">삭제</button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Templates Tab — browse all available policies */}
      {tab === 'templates' && (
        <div>
          <p className="text-xs text-gray-400 mb-4">사용 가능한 정책 템플릿 · 클릭하여 활성화</p>
          {POLICY_DOMAINS.map(domain => (
            <div key={domain.id} className="mb-6">
              <div className="flex items-center gap-2 mb-3">
                <span className="text-xl">{domain.icon}</span>
                <h3 className="text-sm font-semibold">{domain.name} <span className="text-gray-400 font-normal">{domain.nameEn}</span></h3>
                <span className="text-xs text-gray-400 ml-auto">{domain.prdRef} · {domain.desc}</span>
              </div>
              <div className="grid grid-cols-3 gap-3">
                {domain.templates.map(t => {
                  const isActive = rules.some(r => r.template_id === t.id && r.enabled)
                  return (
                    <div key={t.id} className={`card border-l-4 ${isActive ? 'border-l-green-500' : 'border-l-gray-300'}`}>
                      <div className="flex items-start justify-between mb-2">
                        <div>
                          <h4 className="text-sm font-medium">{t.name}</h4>
                          <p className="text-xs text-gray-400">{t.nameEn}</p>
                        </div>
                        {isActive && <span className="badge-green text-[10px]">활성</span>}
                      </div>
                      <p className="text-xs text-gray-500 mb-3">{t.desc}</p>
                      <button
                        onClick={() => addRule({
                          id: `rule-${Date.now()}`, domain: domain.id, template_id: t.id,
                          name: t.name, nameEn: t.nameEn, desc: t.desc,
                          scope: 'org', scopeName: '전체 조직', enabled: true, config: t.config,
                          createdAt: new Date().toISOString(),
                        })}
                        className="text-xs text-blue-600 hover:underline">
                        {isActive ? '다시 추가' : '+ 활성화'}
                      </button>
                    </div>
                  )
                })}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Epochs Tab */}
      {tab === 'epochs' && (
        <div className="card">
          <h3 className="text-sm font-semibold mb-3">정책 에포크 이력 · Policy Epoch History</h3>
          <p className="text-xs text-gray-400 mb-4">정책 변경 시마다 새 에포크가 생성됩니다. 하네스는 에포크를 참조하여 현재 정책을 확인합니다.</p>
          {epochs.length === 0 ? (
            <p className="text-gray-400 text-center py-8">에포크 이력이 없습니다</p>
          ) : (
            <table className="w-full">
              <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                <th className="pb-3">에포크 ID</th><th className="pb-3">전환 모드</th><th className="pb-3">허용 모델</th><th className="pb-3">생성일</th>
              </tr></thead>
              <tbody>
                {epochs.map((e, i) => (
                  <tr key={i} className="border-b border-gray-100 last:border-0">
                    <td className="py-3 font-mono text-xs">{e.epoch_id?.slice(0, 30) || e.id?.slice(0, 30) || '-'}</td>
                    <td className="py-3 text-xs"><span className="badge-gray">{e.transition_mode || '-'}</span></td>
                    <td className="py-3 text-xs text-gray-500">{Array.isArray(e.allowed_models) ? e.allowed_models.join(', ') : e.allowed_models || '-'}</td>
                    <td className="py-3 text-xs text-gray-400">{e.created_at?.slice(0, 19) || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}
