import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { formatRelative } from '../utils/format'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'
import { exportCSV } from '../utils/csv'

// Pattern presets — plain-language options, no regex visible to user
const PATTERN_PRESETS = {
  pii: [
    { id: 'kr-rrn', label: '주민등록번호 (Korean Resident Registration)', desc: '6자리-7자리 형식', value: '\\d{6}-\\d{7}' },
    { id: 'kr-business', label: '사업자등록번호 (Business Registration)', desc: '3자리-2자리-5자리 형식', value: '\\d{3}-\\d{2}-\\d{5}' },
    { id: 'kr-phone', label: '한국 전화번호 (Korean Phone)', desc: '0XX-XXXX-XXXX 형식', value: '0\\d{1,2}-\\d{3,4}-\\d{4}' },
    { id: 'kr-account', label: '은행 계좌번호 (Bank Account)', desc: '은행 계좌 번호 패턴', value: '\\d{3}-\\d{6,8}-\\d{3}' },
  ],
  secret: [
    { id: 'aws-key', label: 'AWS 접근 키 (AWS Access Key)', desc: 'AKIA로 시작하는 20자리 키', value: 'AKIA[A-Z0-9]{16}' },
    { id: 'jwt', label: 'JWT 토큰 (JSON Web Token)', desc: 'eyJ로 시작하는 토큰', value: 'eyJ[a-zA-Z0-9_-]+' },
    { id: 'private-key', label: '개인키 (Private Key)', desc: 'PEM 형식 개인키', value: '-----BEGIN.*PRIVATE KEY' },
    { id: 'github-pat', label: 'GitHub 토큰 (GitHub PAT)', desc: 'ghp_/ghs_ 등으로 시작', value: 'gh[pousr]_[A-Za-z0-9]{36}' },
    { id: 'generic-api-key', label: '일반 API 키 (Generic API Key)', desc: '32자리 이상의 영숫자 키', value: '[a-zA-Z0-9]{32,}' },
  ],
  injection: [
    { id: 'ignore-instructions', label: '명령어 무시 (Ignore Previous Instructions)', desc: '"이전 지시 무시" 등의 재정의 시도', value: 'ignore.*previous.*instructions' },
    { id: 'jailbreak', label: '탈옥 시도 (Jailbreak)', desc: 'DAN, 개발자 모드 등 보안 해제 시도', value: '(jailbreak|DAN|developer.mode|system.prompt)' },
    { id: 'bypass-guard', label: '보안 장치 우회 (Bypass Safety)', desc: '필터/가드 무시 시도', value: '(bypass.*filter|ignore.*safety|disable.*guard)' },
    { id: 'reveal-credentials', label: '자격증명 노출 (Credential Exposure)', desc: '시스템 프롬프트/API 키 출력 시도', value: '(show.*system.*prompt|reveal.*api.*key|print.*env)' },
    { id: 'indirect-injection', label: '간접 인젝션 (Indirect via Code)', desc: '코드/주석에 숨겨진 인젝션', value: '(<!--.*ignore.*-->|{{.*system.*}}|eval.*prompt)' },
  ],
  custom: [
    { id: 'custom', label: '커스텀 정규식 (Custom Regex)', desc: '관리자 정의 정규식 규칙 — 실시간 스캔 적용', value: '' },
  ],
}

const CATEGORY_INFO: Record<string, any> = {
  custom: { ko: '커스텀', en: 'Custom Rules', icon: '⚙️', desc: '관리자 정의 정규식 — 즉시 스캔 적용' },
  pii: { ko: '개인정보', en: 'PII Detection', icon: '🆔', desc: '한국 개인정보(주민번호, 사업자번호 등) 감지' },
  secret: { ko: '비밀정보', en: 'Secret Scanning', icon: '🔑', desc: 'API 키, 토큰, 개인키 등 민감 정보 감지' },
  injection: { ko: '프롬프트 인젝션', en: 'Prompt Injection', icon: '🧪', desc: '명령어 재정의, 탈옥, 제어 우회 시도' },
  behavior: { ko: '행동 분석', en: 'Behavioral Analysis', icon: '📊', desc: '사용량 패턴, 봇 감지, 비정상 행동' },
  code: { ko: '코드 보안', en: 'Code Security', icon: '📦', desc: '취약한 의존성, 금지 라이선스, 암호화' },
  infra: { ko: '인프라 보안', en: 'Infrastructure', icon: '🏗️', desc: '샌드박스, 엔드포인트, 프로토콜 공격' },
}

const SEVERITY_INFO: Record<string, any> = {
  critical: { ko: '치명적', color: 'badge-red', desc: '즉시 차단 필요' },
  high: { ko: '높음', color: 'badge-red', desc: '빠른 대응 필요' },
  medium: { ko: '중간', color: 'badge-yellow', desc: '검토 후 대응' },
  low: { ko: '낮음', color: 'badge-blue', desc: '기록 및 모니터링' },
}

const ACTION_INFO: Record<string, any> = {
  block: { ko: '차단', color: 'badge-red', desc: '요청 즉시 거부' },
  mask: { ko: '마스킹', color: 'badge-yellow', desc: '민감 정보 마스킹 후 허용' },
  throttle: { ko: '속도 제한', color: 'badge-blue', desc: '요청 빈도 제한' },
  review: { ko: '검토 요청', color: 'badge-yellow', desc: '관리자 검토 대기' },
  alert: { ko: '알림만', color: 'badge-gray', desc: '기록 및 알림, 허용' },
  stepup: { ko: '재인증', color: 'badge-blue', desc: '추가 인증 요구' },
}

type Rule = {
  id: string
  name: string
  nameEn: string
  category: string
  severity: string
  action: string
  presetId: string
  pattern: string
  enabled: boolean
}

// Default rules built from presets
function buildDefaultRules(): Rule[] {
  const rules: Rule[] = []
  Object.entries(PATTERN_PRESETS).forEach(([cat, presets]) => {
    presets.forEach(p => {
      const sev = cat === 'pii' || cat === 'secret' ? 'critical' : cat === 'injection' || cat === 'infra' ? 'critical' : cat === 'behavior' ? 'medium' : 'high'
      const act = cat === 'pii' ? 'mask' : 'block'
      rules.push({
        id: `${cat}-${p.id}`,
        name: p.label.split('(')[0].trim(),
        nameEn: p.label.match(/\(([^)]+)\)/)?.[1] || p.id,
        category: cat,
        severity: sev,
        action: cat === 'pii' && p.id !== 'kr-rrn' && p.id !== 'kr-account' ? 'mask' : act,
        presetId: p.id,
        pattern: p.value,
        enabled: true,
      })
    })
  })
  return rules
}

type Finding = {
  id: string; finding_type: string; severity: string; title: string;
  title_ko?: string; status: string; occurred_at: string; session_id?: string;
}

export default function Security() {
  const confirm = useConfirm()
  const [tab, setTab] = useState<'dashboard' | 'rules' | 'findings' | 'scanner'>('dashboard')
  const [rules, setRules] = useState<Rule[]>(buildDefaultRules())

  // Merge persisted DLP rules from API into the catalog
  useEffect(() => {
    api.securityRules().then((persisted: any[]) => {
      if (!persisted || persisted.length === 0) return
      setRules(prev => {
        const map = new Map(prev.map(r => [r.id, r]))
        for (const p of persisted) {
          const key = p.rule_id || p.id
          if (map.has(key)) {
            const ex = map.get(key)!
            map.set(key, { ...ex, enabled: p.enabled ?? ex.enabled, action: p.action || ex.action })
          }
        }
        return Array.from(map.values())
      })
    }).catch(() => {})
  }, [])
  const [scanText, setScanText] = useState('')
  const [scanResult, setScanResult] = useState<any>(null)
  const [findings, setFindings] = useState<Finding[]>([])
  const [stats, setStats] = useState({ critical: 0, high: 0, medium: 0, open: 0, total: 0 })
  const [showBuilder, setShowBuilder] = useState(false)
  const [findingDetail, setFindingDetail] = useState<any>(null)
  const [editingRule, setEditingRule] = useState<Rule | null>(null)
  const [findingFilter, setFindingFilter] = useState('all')

  useEffect(() => {
    fetch('/api/analytics/security', { headers: authHeaders() })
      .then(r => r.json()).then(data => {
        const s = data || {}
        setStats({
          critical: s.critical_count || 0, high: s.high_count || 0,
          medium: (s.total_findings || 0) - (s.critical_count || 0) - (s.high_count || 0),
          open: s.open_count || 0, total: s.total_findings || 0,
        })
      }).catch(() => {})
    fetch('/api/security/findings', { headers: authHeaders() })
      .then(r => r.json()).then(data => setFindings(Array.isArray(data) ? data : []))
      .catch(() => {})
  }, [])

  const viewFindingDetail = async (id: string) => {
    try {
      const res = await fetch(`/api/security/findings/${id}`, { headers: authHeaders() })
      if (res.ok) { setFindingDetail(await res.json()) }
    } catch {}
  }

  const updateFindingStatus = async (id: string, status: string) => {
    try {
      await fetch(`/api/security/findings/${id}`, {
        method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ status }),
      })
      setFindingDetail(null)
      // Refresh findings
      fetch('/api/security/findings', { headers: authHeaders() })
        .then(r => r.json()).then(data => setFindings(Array.isArray(data) ? data : []))
    } catch {}
  }

  const runScan = async () => {
    if (!scanText) return
    try { setScanResult(await api.securityCheck(scanText)) } catch (e: any) { setScanResult({ error: e.message }) }
  }

  const saveRule = (rule: Rule) => {
    if (editingRule) {
      setRules(rs => rs.map(r => r.id === rule.id ? rule : r))
    } else {
      setRules(rs => [...rs, { ...rule, id: `${rule.category}-${Date.now()}` }])
    }
    // Persist to backend (custom rules carry their regex pattern —
    // the engine compiles + scans it live).
    fetch('/api/security/policy', {
      method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ rule_id: rule.id, enabled: rule.enabled, severity: rule.severity, action: rule.action, pattern: rule.pattern || '' }),
    }).catch(() => {})
    setShowBuilder(false)
    setEditingRule(null)
  }

  const deleteRule = async (id: string) => {
    if (!await confirm({ title: '확인', message: '이 규칙을 삭제하시겠습니까?', danger: true })) return
    setRules(rs => rs.filter(r => r.id !== id))
  }

  const toggleRule = (id: string) => {
    setRules(rs => rs.map(r => r.id === id ? { ...r, enabled: !r.enabled } : r))
    const rule = rules.find(r => r.id === id)
    if (rule) {
      fetch('/api/security/policy', {
        method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ rule_id: id, enabled: !rule.enabled }),
      }).catch(() => {})
    }
  }

  const sevBadge = (s: string) => SEVERITY_INFO[s]?.color || 'badge-gray'
  const actionBadge = (a: string) => ACTION_INFO[a]?.color || 'badge-gray'
  const statusBadge = (s: string) => s === 'open' ? 'badge-red' : s === 'investigating' ? 'badge-yellow' : 'badge-green'

  const filteredFindings = findings.filter(f => {
    if (findingFilter === 'all') return true
    if (findingFilter === 'open') return f.status === 'open'
    return f.severity === findingFilter
  })
  const postureScore = stats.total === 0 ? 100 : Math.max(0, 100 - stats.critical * 25 - stats.high * 10 - stats.open * 5)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">보안 운영 센터 <span className="text-gray-400 text-lg font-normal">Security Operations Center</span></h1>
      <p className="text-xs text-gray-400 mb-6">AI 코딩 위협 탐지 및 대응 · Threat Detection & Response</p>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'dashboard', label: '보안 현황', en: 'Dashboard' },
          { id: 'rules', label: '보안 규칙', en: 'Rules' },
          { id: 'findings', label: '보안 발견', en: 'Findings' },
          { id: 'scanner', label: '보안 검사', en: 'Scanner' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} <span className="text-xs text-gray-400">{t.en}</span>
          </button>
        ))}
      </div>

      {/* DASHBOARD */}
      {tab === 'dashboard' && (
        <div>
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="card"><div className="text-center">
              <div className={`text-5xl font-bold ${postureScore >= 80 ? 'text-green-600' : postureScore >= 50 ? 'text-yellow-600' : 'text-red-600'}`}>{postureScore}</div>
              <div className="text-sm text-gray-500 mt-1">보안 점수</div></div></div>
            <div className="card col-span-3">
              <h3 className="text-sm font-medium text-gray-700 mb-3">위협 현황</h3>
              <div className="grid grid-cols-4 gap-4">
                <div className="text-center"><div className="text-2xl font-bold text-red-600">{stats.critical}</div><div className="text-xs text-gray-500">치명적</div></div>
                <div className="text-center"><div className="text-2xl font-bold text-orange-600">{stats.high}</div><div className="text-xs text-gray-500">높음</div></div>
                <div className="text-center"><div className="text-2xl font-bold text-yellow-600">{stats.medium}</div><div className="text-xs text-gray-500">중간</div></div>
                <div className="text-center"><div className="text-2xl font-bold text-blue-600">{stats.open}</div><div className="text-xs text-gray-500">미해결</div></div>
              </div>
            </div>
          </div>

          {/* Category cards */}
          <div className="grid grid-cols-3 gap-3 mb-6">
            {Object.entries(CATEGORY_INFO).map(([id, info]) => {
              const catRules = rules.filter(r => r.category === id && r.enabled)
              return (
                <div key={id} className="card">
                  <div className="flex items-start gap-3">
                    <span className="text-2xl">{info.icon}</span>
                    <div className="flex-1">
                      <h4 className="text-sm font-semibold">{info.ko}</h4>
                      <p className="text-xs text-gray-400">{info.en}</p>
                      <p className="text-xs text-gray-500 mt-1">{info.desc}</p>
                    </div>
                    <span className="badge-gray">{catRules.length} 규칙</span>
                  </div>
                </div>
              )
            })}
          </div>

          {/* Emergency */}
          <div className="card">
            <h3 className="text-sm font-semibold mb-3">인시던트 대응</h3>
            <button className="btn-danger w-full text-sm" onClick={async () => {
              if (!await confirm({ title: '확인', message: '전체 조직을 잠금하시겠습니까? 모든 AI 세션이 중지됩니다.', danger: true })) return
              try { await fetch('/api/security/lockdown', { method: 'POST', headers: authHeaders() }); showToast('긴급 잠금 활성화', 'success') } catch { showToast('실패', 'error') }
            }}>⚠ 긴급 조직 잠금 · Emergency Lockdown</button>
          </div>
        </div>
      )}

      {/* RULES — full CRUD with builder */}
      {tab === 'rules' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <div>
              <h3 className="text-lg font-semibold">보안 규칙 관리 · Security Rules</h3>
              <p className="text-xs text-gray-400">{rules.filter(r => r.enabled).length}개 활성 / {rules.length}개 전체 · 규칙을 클릭하여 편집/삭제</p>
            </div>
            <button onClick={() => { setEditingRule(null); setShowBuilder(true) }} className="btn-primary text-sm">+ 새 규칙 만들기</button>
          </div>

          {/* Rule Builder Modal */}
          {showBuilder && (
            <RuleBuilder
              rule={editingRule}
              onSave={saveRule}
              onCancel={() => { setShowBuilder(false); setEditingRule(null) }}
            />
          )}

          {/* Rules grouped by category */}
          {Object.entries(CATEGORY_INFO).map(([catId, catInfo]) => {
            const catRules = rules.filter(r => r.category === catId)
            if (catRules.length === 0) return null
            return (
              <div key={catId} className="card mb-4">
                <div className="flex items-center gap-2 mb-3">
                  <span className="text-xl">{catInfo.icon}</span>
                  <h4 className="text-sm font-semibold">{catInfo.ko} <span className="text-gray-400 font-normal">{catInfo.en}</span></h4>
                  <span className="text-xs text-gray-400 ml-auto">{catInfo.desc}</span>
                </div>
                <table className="w-full overflow-x-auto block">
                  <thead>
                    <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                      <th className="pb-2">규칙 이름</th>
                      <th className="pb-2">설명</th>
                      <th className="pb-2">심각도</th>
                      <th className="pb-2">조치</th>
                      <th className="pb-2">활성</th>
                      <th className="pb-2"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {catRules.map(r => {
                      const preset = PATTERN_PRESETS[r.category as keyof typeof PATTERN_PRESETS]?.find(p => p.id === r.presetId)
                      return (
                        <tr key={r.id} className="border-b border-gray-50 last:border-0 hover:bg-blue-50/20">
                          <td className="py-2.5">
                            <div className="text-sm font-medium">{r.name}</div>
                            <div className="text-xs text-gray-400">{r.nameEn}</div>
                          </td>
                          <td className="py-2.5 text-xs text-gray-500 max-w-xs">{preset?.desc || '-'}</td>
                          <td className="py-2.5"><span className={sevBadge(r.severity)}>{SEVERITY_INFO[r.severity]?.ko}</span></td>
                          <td className="py-2.5"><span className={actionBadge(r.action)}>{ACTION_INFO[r.action]?.ko}</span></td>
                          <td className="py-2.5">
                            <button onClick={() => toggleRule(r.id)}
                              className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${r.enabled ? 'bg-patty-600' : 'bg-gray-300'}`}>
                              <span className={`inline-block h-3 w-3 rounded-full bg-white transition-transform ${r.enabled ? 'translate-x-5' : 'translate-x-1'}`} />
                            </button>
                          </td>
                          <td className="py-2.5">
                            <div className="flex gap-2">
                              <button onClick={() => { setEditingRule(r); setShowBuilder(true) }} className="text-xs text-blue-600 hover:underline">편집</button>
                              <button onClick={() => deleteRule(r.id)} className="text-xs text-red-600 hover:underline">삭제</button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )
          })}
        </div>
      )}

      {/* FINDINGS */}
      {tab === 'findings' && (
        <div className="card">
          <div className="flex justify-between items-center mb-4">
            <h3 className="text-lg font-semibold">보안 발견 목록 · Findings</h3>
            <div className="flex gap-2 items-center">
              <select className="input w-auto text-xs" value={findingFilter} onChange={e => setFindingFilter(e.target.value)}>
                <option value="all">전체 · All</option>
                <option value="critical">🔴 치명적</option>
                <option value="high">🟠 높음</option>
                <option value="medium">🟡 중간</option>
                <option value="open">미해결만</option>
              </select>
              {findings.length > 0 && (
              <button onClick={() => exportCSV('security_findings.csv', ['timestamp', 'type', 'severity', 'title', 'status', 'session_id'], findings.map(f => [f.occurred_at, f.finding_type, f.severity, f.title_ko || f.title, f.status, f.session_id]))} className="btn-sm btn-secondary">CSV</button>
              )}
            </div>
          </div>
          {filteredFindings.length === 0 ? (
            <div className="text-center py-12"><div className="text-4xl mb-3">✅</div><p className="text-gray-500">활성 보안 발견이 없습니다.</p></div>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead><tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3">유형</th><th className="pb-3">심각도</th><th className="pb-3">제목</th><th className="pb-3">상태</th><th className="pb-3">시간</th>
              </tr></thead>
              <tbody>
                {filteredFindings.map(f => (
                  <tr key={f.id} className="border-b border-gray-100 last:border-0 cursor-pointer hover:bg-blue-50/30" onClick={() => viewFindingDetail(f.id)}>
                    <td className="py-3 text-sm font-mono">{f.finding_type}</td>
                    <td className="py-3"><span className={sevBadge(f.severity)}>{f.severity}</span></td>
                    <td className="py-3 text-sm">{f.title_ko || f.title}</td>
                    <td className="py-3"><span className={statusBadge(f.status)}>{f.status}</span></td>
                    <td className="py-3 text-xs text-gray-400">{formatRelative(f.occurred_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* SCANNER */}
      {tab === 'scanner' && (
        <div className="card">
          <h3 className="text-lg font-semibold mb-3">보안 검사 도구 · Scanner</h3>
          <p className="text-sm text-gray-500 mb-4">텍스트를 입력하여 보안 규칙을 테스트합니다. 관리자가 규칙을 검증하는 데 사용됩니다.</p>
          <textarea className="input font-mono text-sm mb-3" rows={4} value={scanText}
            onChange={e => setScanText(e.target.value)}
            placeholder="예: 주민번호 901225-1234567, AWS 키 AKIAABCDEFGHIJKLMNOP, ignore previous instructions..." />
          <button onClick={runScan} disabled={!scanText} className="btn-primary text-sm">검사 실행</button>
          {scanResult && (
            <div className="mt-6">
              <span className={scanResult.passed ? 'badge-green' : scanResult.verdict === 'DENY' ? 'badge-red' : 'badge-yellow'}>
                {scanResult.verdict === 'DENY' ? '🚫 차단됨' : scanResult.verdict === 'REQUIRE_REVIEW' ? '⚠️ 검토 필요' : '✅ 통과'}
              </span>
              {scanResult.findings?.length > 0 && (
                <table className="w-full mt-3">
                  <thead><tr className="border-b text-left text-sm text-gray-500">
                    <th className="pb-2">유형</th><th className="pb-2">심각도</th><th className="pb-2">항목</th><th className="pb-2">매칭</th>
                  </tr></thead>
                  <tbody>
                    {scanResult.findings.map((f: any, i: number) => (
                      <tr key={i} className="border-b border-gray-100 last:border-0">
                        <td className="py-2 text-sm font-mono">{f.type}</td>
                        <td className="py-2"><span className={sevBadge(f.severity)}>{f.severity}</span></td>
                        <td className="py-2 text-sm">{f.title_ko || f.title}</td>
                        <td className="py-2 text-xs font-mono text-gray-400">{f.match}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              {scanResult.passed && <p className="text-green-600 text-sm mt-3">✅ 위반 사항이 없습니다.</p>}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ─── Rule Builder Component ───────────────────────────────────
function RuleBuilder({ rule, onSave, onCancel }: { rule: Rule | null; onSave: (r: Rule) => void; onCancel: () => void }) {
  const [category, setCategory] = useState(rule?.category || 'pii')
  const [presetId, setPresetId] = useState(rule?.presetId || '')
  const [name, setName] = useState(rule?.name || '')
  const [severity, setSeverity] = useState(rule?.severity || 'high')
  const [action, setAction] = useState(rule?.action || 'block')
  const [enabled, setEnabled] = useState(rule?.enabled ?? true)

  const presets = PATTERN_PRESETS[category as keyof typeof PATTERN_PRESETS] || []
  const selectedPreset = presets.find(p => p.id === presetId)

  const handlePresetChange = (id: string) => {
    setPresetId(id)
    const p = presets.find(x => x.id === id)
    if (p) setName(p.label.split('(')[0].trim())
  }

  const handleSave = () => {
    if (!presetId || !name) { showToast('패턴과 이름을 선택하세요', 'error'); return }
    const p = presets.find(x => x.id === presetId)
    onSave({
      id: rule?.id || `${category}-${Date.now()}`,
      name, nameEn: p?.label.match(/\(([^)]+)\)/)?.[1] || name,
      category, severity, action, presetId,
      pattern: p?.value || '',
      enabled,
    })
  }

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50" onClick={onCancel}>
      <div className="bg-white rounded-xl shadow-xl max-w-lg w-full mx-4" onClick={e => e.stopPropagation()}>
        <div className="p-5 border-b border-gray-100 flex items-center justify-between">
          <h3 className="font-semibold">{rule ? '규칙 편집' : '새 보안 규칙'}</h3>
          <button onClick={onCancel} className="text-gray-400 hover:text-gray-600">✕</button>
        </div>
        <div className="p-5 space-y-4">
          {/* Category */}
          <div>
            <label className="label">위협 카테고리 · Threat Category</label>
            <select className="input" value={category} onChange={e => { setCategory(e.target.value); setPresetId('') }}>
              {Object.entries(CATEGORY_INFO).map(([id, info]) => (
                <option key={id} value={id}>{info.icon} {info.ko} ({info.en})</option>
              ))}
            </select>
            <p className="text-xs text-gray-400 mt-1">{CATEGORY_INFO[category]?.desc}</p>
          </div>

          {/* Pattern Preset */}
          <div>
            <label className="label">탐지 대상 · What to Detect</label>
            <select className="input" value={presetId} onChange={e => handlePresetChange(e.target.value)}>
              <option value="">선택하세요 · Select...</option>
              {presets.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}
            </select>
            {selectedPreset && <p className="text-xs text-blue-600 mt-1">📋 {selectedPreset.desc}</p>}
          </div>

          {/* Name */}
          <div>
            <label className="label">규칙 이름 · Rule Name</label>
            <input className="input" value={name} onChange={e => setName(e.target.value)} />
          </div>

          {/* Severity + Action */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">심각도 · Severity</label>
              <select className="input" value={severity} onChange={e => setSeverity(e.target.value)}>
                {Object.entries(SEVERITY_INFO).map(([id, info]) => (
                  <option key={id} value={id}>{info.ko} — {info.desc}</option>
                ))}
              </select>
            </div>
            <div>
              <label className="label">조치 · Action</label>
              <select className="input" value={action} onChange={e => setAction(e.target.value)}>
                {Object.entries(ACTION_INFO).map(([id, info]) => (
                  <option key={id} value={id}>{info.ko} — {info.desc}</option>
                ))}
              </select>
            </div>
          </div>

          {/* Enabled */}
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} className="w-4 h-4" />
            규칙 활성화 · Enable this rule
          </label>
        </div>
        <div className="p-5 border-t border-gray-100 flex gap-2">
          <button onClick={handleSave} className="btn-primary text-sm flex-1">{rule ? '수정 저장' : '규칙 생성'}</button>
          <button onClick={onCancel} className="btn-secondary text-sm">취소</button>
        </div>
      </div>
    </div>
  )
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
