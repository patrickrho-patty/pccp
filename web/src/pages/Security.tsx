import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { StatCard } from '../components/StatCard'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { formatRelative } from '../utils/format'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'
import { exportCSV } from '../utils/csv'

const CATEGORY_INFO: Record<string, { ko: string; en: string; icon: string; desc: string }> = {
  pii: { ko: '개인정보', en: 'PII Detection', icon: '🆔', desc: '한국 개인정보(주민번호, 사업자번호 등) 감지' },
  secret: { ko: '비밀정보', en: 'Secret Scanning', icon: '🔑', desc: 'API 키, 토큰, 개인키 등 민감 정보 감지' },
  injection: { ko: '프롬프트 인젝션', en: 'Prompt Injection', icon: '🧪', desc: '명령어 재정의, 탈옥, 제어 우회 시도' },
  behavior: { ko: '행동 분석', en: 'Behavioral Analysis', icon: '📊', desc: '사용량 패턴, 봇 감지, 비정상 행동' },
  code: { ko: '코드 보안', en: 'Code Security', icon: '📦', desc: '취약한 의존성, 금지 라이선스, 암호화' },
  infra: { ko: '인프라 보안', en: 'Infrastructure', icon: '🏗️', desc: '샌드박스, 엔드포인트, 프로토콜 공격' },
}

const SEVERITY_INFO: Record<string, { ko: string; color: string; desc: string }> = {
  critical: { ko: '치명적', color: 'badge-red', desc: '즉시 차단 필요' },
  high: { ko: '높음', color: 'badge-red', desc: '빠른 대응 필요' },
  medium: { ko: '중간', color: 'badge-yellow', desc: '검토 후 대응' },
  low: { ko: '낮음', color: 'badge-blue', desc: '기록 및 모니터링' },
}

const RULE_PRESETS: Record<string, { id: string; label: string; value: string }[]> = {
  pii: [
    { id: 'kr-rrn', label: '주민등록번호', value: '\\d{6}-\\d{7}' },
    { id: 'kr-business', label: '사업자등록번호', value: '\\d{3}-\\d{2}-\\d{5}' },
    { id: 'kr-phone', label: '한국 전화번호', value: '0\\d{1,2}-\\d{3,4}-\\d{4}' },
    { id: 'kr-account', label: '은행 계좌번호', value: '\\d{3}-\\d{6,8}-\\d{3}' },
  ],
  secret: [
    { id: 'aws-key', label: 'AWS 접근 키', value: 'AKIA[A-Z0-9]{16}' },
    { id: 'jwt', label: 'JWT 토큰', value: 'eyJ[a-zA-Z0-9_-]+' },
    { id: 'private-key', label: '개인키', value: '-----BEGIN.*PRIVATE KEY' },
    { id: 'github-pat', label: 'GitHub 토큰', value: 'gh[pousr]_[A-Za-z0-9]{36}' },
  ],
  injection: [
    { id: 'ignore-instructions', label: '명령어 무시', value: 'ignore.*previous.*instructions' },
    { id: 'jailbreak', label: '탈옥 시도', value: '(jailbreak|DAN|developer.mode|system.prompt)' },
  ],
}

type Rule = { id: string; name: string; nameEn: string; category: string; severity: string; action: string; pattern: string; enabled: boolean }

function buildDefaultRules(): Rule[] {
  const rules: Rule[] = []
  Object.entries(RULE_PRESETS).forEach(([cat, presets]) => {
    presets.forEach(p => {
      rules.push({
        id: `${cat}-${p.id}`, name: p.label, nameEn: p.id, category: cat,
        severity: cat === 'secret' ? 'critical' : cat === 'injection' ? 'critical' : 'high',
        action: cat === 'pii' ? 'mask' : 'block',
        pattern: p.value, enabled: true,
      })
    })
  })
  return rules
}

type Finding = { id: string; finding_type: string; severity: string; title: string; title_ko?: string; status: string; occurred_at: string; session_id?: string; direction?: string; suppressed?: boolean }

export default function Security() {
  const confirm = useConfirm()
  const [tab, setTab] = useState<'dashboard' | 'rules' | 'findings' | 'scanner' | 'incidents' | 'alerts'>('dashboard')
  const [rules, setRules] = useState<Rule[]>(buildDefaultRules())
  const [favorites, setFavorites] = useState<Set<string>>(new Set())
  const [scanText, setScanText] = useState('')
  const [scanResult, setScanResult] = useState<any>(null)
  const [findings, setFindings] = useState<Finding[]>([])
  const [findingTotal, setFindingTotal] = useState(0)
  const [stats, setStats] = useState({ critical: 0, high: 0, medium: 0, open: 0, total: 0 })
  const [showBuilder, setShowBuilder] = useState(false)
  const [findingDetail, setFindingDetail] = useState<any>(null)
  const [editingRule, setEditingRule] = useState<Rule | null>(null)
  const [ruleBefore, setRuleBefore] = useState<Rule | null>(null)
  const [findingFilters, setFindingFilters] = useState({ severity: '', status: '', type: '', from: '', to: '' })
  const [selectedFindings, setSelectedFindings] = useState<Set<string>>(new Set())
  const [suppressTarget, setSuppressTarget] = useState<Finding | null>(null)
  const [suppressForm, setSuppressForm] = useState({ reason: '', days: 30 })
  const [lockdownModal, setLockdownModal] = useState(false)
  const [lockdownForm, setLockdownForm] = useState({ scope: 'org', project_id: '', reason: '' })
  const [lockdownImpact, setLockdownImpact] = useState<any>(null)
  const [incidents, setIncidents] = useState<any[]>([])
  const [incidentModal, setIncidentModal] = useState(false)
  const [incidentForm, setIncidentForm] = useState({ title: '', title_ko: '', severity: 'high', category: 'credential_leak', finding_ids: [] as string[] })
  const [alerts, setAlerts] = useState<any[]>([])
  const [alertModal, setAlertModal] = useState(false)
  const [alertForm, setAlertForm] = useState({ name: '', type: 'webhook', target: '', severities: ['critical', 'high'] })
  const [lexicon, setLexicon] = useState<any>(null)
  const [lexiconForm, setLexiconForm] = useState('')
  const [sessionPick, setSessionPick] = useState('')
  const [sessions, setSessions] = useState<any[]>([])
  const [projects, setProjects] = useState<any[]>([])
  const [testerText, setTesterText] = useState('')
  const [testerRule, setTesterRule] = useState<string>('')
  const [testerMatches, setTesterMatches] = useState<string[]>([])

  const loadRules = () => {
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
  }

  const loadFindings = () => {
    const params: Record<string, string> = {}
    if (findingFilters.severity) params.severity = findingFilters.severity
    if (findingFilters.status) params.status = findingFilters.status
    if (findingFilters.type) params.finding_type = findingFilters.type
    if (findingFilters.from) params.from = findingFilters.from
    if (findingFilters.to) params.to = findingFilters.to
    api.securityFindings(params).then((d: any) => {
      const list = Array.isArray(d) ? d : (d?.data || [])
      setFindings(list)
      setFindingTotal(typeof d?.total === 'number' ? d.total : list.length)
    }).catch(() => {})
  }

  const loadStats = () => {
    fetch('/api/analytics/security', { headers: authHeaders() })
      .then(r => r.json()).then(data => {
        const s = data || {}
        setStats({
          critical: s.critical_count || 0, high: s.high_count || 0,
          medium: (s.total_findings || 0) - (s.critical_count || 0) - (s.high_count || 0),
          open: s.open_count || 0, total: s.total_findings || 0,
        })
      }).catch(() => {})
  }

  const loadIncidents = () => api.listIncidents().then(d => setIncidents(Array.isArray(d) ? d : [])).catch(() => {})
  const loadAlerts = () => api.securityAlerts().then(d => setAlerts(Array.isArray(d) ? d : [])).catch(() => {})
  const loadLexicon = () => api.securityLexicon().then(d => { setLexicon(d); setLexiconForm(JSON.stringify(d?.patterns || {}, null, 2)) }).catch(() => {})

  useEffect(() => {
    // Drill-down deep links (00 A5): /security?tab=findings&severity=...
    const params = new URLSearchParams(window.location.search)
    const urlTab = params.get('tab')
    if (urlTab) setTab(urlTab as any)
    const urlSeverity = params.get('severity')
    const urlStatus = params.get('status')
    if (urlSeverity || urlStatus) setFindingFilters(f => ({ ...f, severity: urlSeverity || '', status: urlStatus || '' }))
  }, [])

  useEffect(() => {
    loadRules()
    loadFindings()
    loadStats()
    loadIncidents()
    loadAlerts()
    loadLexicon()
    api.listSessions().then((d: any[]) => setSessions(Array.isArray(d) ? d : [])).catch(() => {})
    api.listProjects().then((d: any[]) => setProjects(Array.isArray(d) ? d : [])).catch(() => {})
  }, [findingFilters])

  const toggleFavorite = (id: string) => {
    setFavorites(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }
  const sortedRules = useMemo(() =>
    [...rules].sort((a, b) => Number(favorites.has(b.id)) - Number(favorites.has(a.id))),
    [rules, favorites])

  const viewFindingDetail = async (id: string) => {
    try { setFindingDetail(await api.securityFindingDetail(id)) } catch {}
  }

  const updateFindingStatus = async (id: string, status: string) => {
    try {
      await api.updateSecurityFinding(id, { status })
      setFindingDetail(null)
      loadFindings(); loadStats()
    } catch {}
  }

  const bulkFindings = async (status: string) => {
    const ids = [...selectedFindings]
    if (ids.length === 0) { showToast('발견을 선택하세요', 'error'); return }
    try {
      await api.bulkSecurityFindings(ids, status)
      showToast(`${ids.length}건 ${status} 처리`, 'success')
      setSelectedFindings(new Set())
      loadFindings(); loadStats()
    } catch {}
  }

  const submitSuppress = async () => {
    if (!suppressTarget || !suppressForm.reason.trim()) { showToast('사유를 입력하세요', 'error'); return }
    try {
      await api.suppressFinding(suppressTarget.id, suppressForm)
      showToast('억제됨 · 만료 시 자동 재오픈', 'success')
      setSuppressTarget(null)
      loadFindings(); loadStats()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const runScan = async () => {
    if (!scanText) return
    try { setScanResult(await api.securityCheck(scanText)) } catch (e: any) { setScanResult({ error: e.message }) }
  }

  const runSessionScan = async () => {
    if (!sessionPick) { showToast('세션을 선택하세요', 'error'); return }
    try {
      const res: any = await api.scanSession(sessionPick)
      showToast(`${res.exchanges_scanned}개 교환 스캔 · ${res.total_findings}건 발견`, res.total_findings > 0 ? 'error' : 'success')
      loadFindings(); loadStats()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const runTester = () => {
    if (!testerRule || !testerText) return
    try {
      const re = new RegExp(testerRule, 'gi')
      const matches = testerText.match(re) || []
      setTesterMatches(matches)
    } catch { setTesterMatches([]); showToast('정규식 오류', 'error') }
  }

  const saveRule = (rule: Rule) => {
    setRules(rs => rs.map(r => r.id === rule.id ? rule : r))
    fetch('/api/security/policy', {
      method: 'PUT', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ rule_id: rule.id, enabled: rule.enabled, severity: rule.severity, action: rule.action }),
    }).catch(() => {})
    setShowBuilder(false)
    setEditingRule(null)
    setRuleBefore(null)
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

  const openLockdown = async () => {
    setLockdownModal(true)
    try { setLockdownImpact(await api.lockdownImpact(lockdownForm.scope, lockdownForm.project_id || undefined)) } catch { setLockdownImpact(null) }
  }

  const runLockdown = async () => {
    if (!await confirm({ title: '⚠ 긴급 잠금 최종 확인', message: `${lockdownForm.scope === 'project' ? '선택한 프로젝트' : '전체 조직'}의 모든 활성 AI 세션이 종료됩니다. 실행하시겠습니까?`, danger: true })) return
    try {
      const res: any = await api.securityLockdown({ ...lockdownForm })
      showToast(`잠금 활성화 · ${res.affected_harnesses ?? 'N'}개 하네스 통지${res.relay_propagated === false ? ' (릴레이 채널 미연결)' : ''}`, 'success')
      setLockdownModal(false)
      loadStats()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const createIncident = async () => {
    if (!incidentForm.title) { showToast('제목을 입력하세요', 'error'); return }
    try {
      await api.createIncident({
        title: incidentForm.title, title_ko: incidentForm.title_ko || incidentForm.title,
        severity: incidentForm.severity, category: incidentForm.category,
        finding_ids: incidentForm.finding_ids,
        description: `SOC 콘솔에서 생성 · ${incidentForm.finding_ids.length}개 발견 그룹화`,
      })
      showToast('인시던트 생성됨', 'success')
      setIncidentModal(false)
      setIncidentForm({ title: '', title_ko: '', severity: 'high', category: 'credential_leak', finding_ids: [] })
      loadIncidents()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const containIncident = async (inc: any, mode: string) => {
    if (!await confirm({ title: '봉쇄', message: `인시던트를 ${mode} 모드로 봉쇄하시겠습니까?`, danger: true })) return
    try {
      await api.containIncident({ incident_id: inc.id, mode, organization_id: inc.organization_id, performed_by: 'admin', reason: 'SOC containment' })
      showToast('봉쇄 실행됨', 'success')
      loadIncidents()
    } catch {}
  }

  const resolveIncident = async (inc: any) => {
    if (!await confirm({ title: '해결', message: '인시던트를 해결 처리하시겠습니까?', danger: false })) return
    try { await api.resolveIncident(inc.id, 'resolved via console'); showToast('해결됨', 'success'); loadIncidents() } catch {}
  }

  const submitAlert = async () => {
    if (!alertForm.name || !alertForm.target) { showToast('이름과 대상 URL을 입력하세요', 'error'); return }
    try {
      await api.createSecurityAlert(alertForm)
      showToast('알림 라우트 추가됨', 'success')
      setAlertModal(false)
      setAlertForm({ name: '', type: 'webhook', target: '', severities: ['critical', 'high'] })
      loadAlerts()
    } catch (err: any) { showToast(err.message, 'error') }
  }

  const publishLexicon = async () => {
    try {
      const patterns = JSON.parse(lexiconForm)
      await api.updateSecurityLexicon({ version: String(Date.now()), patterns })
      showToast('렉시콘 버전 발행됨', 'success')
      loadLexicon()
    } catch (err: any) { showToast('발행 실패: ' + err.message, 'error') }
  }

  const sevBadge = (s: string) => SEVERITY_INFO[s]?.color || 'badge-gray'
  const statusBadge = (s: string) => s === 'open' ? 'badge-red' : s === 'investigating' ? 'badge-yellow' : s === 'suppressed' ? 'badge-gray' : 'badge-green'
  const postureScore = stats.total === 0 ? 100 : Math.max(0, 100 - stats.critical * 25 - stats.high * 10 - stats.open * 5)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">보안 운영 센터 <span className="text-gray-400 text-lg font-normal">Security Operations Center</span></h1>
      <p className="text-xs text-gray-400 mb-4">AI 코딩 위협 탐지 및 대응 · Threat Detection & Response</p>

      {/* Tabs guidance (UX2) */}
      <div className="card mb-4 py-2 px-4 flex items-center gap-3 text-xs text-gray-500 flex-wrap">
        <span className="font-semibold text-gray-700">시작하기 · Start here:</span>
        <span>1️⃣ 현황 대시보드 → 2️⃣ 규칙 튜닝 → 3️⃣ 발견 처리 → 4️⃣ 스캐너 검증</span>
        <span className="ml-auto text-gray-400">탐지는 릴레이 실경로에서 실행됩니다 (§16)</span>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200 flex-wrap">
        {[
          { id: 'dashboard', label: '보안 현황', en: 'Dashboard' },
          { id: 'rules', label: '보안 규칙', en: 'Rules' },
          { id: 'findings', label: '보안 발견', en: 'Findings', count: stats.total },
          { id: 'scanner', label: '보안 검사', en: 'Scanner' },
          { id: 'incidents', label: '인시던트', en: 'Incidents' },
          { id: 'alerts', label: '알림/렉시콘', en: 'Alerts & Lexicon' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} {t.count !== undefined && <span className="text-xs text-gray-400">({t.count})</span>}
          </button>
        ))}
      </div>

      {/* DASHBOARD */}
      {tab === 'dashboard' && (
        <div>
          <div className="grid grid-cols-2 md:grid-cols-5 gap-3 stat-grid mb-6">
            <StatCard label="보안 점수" value={postureScore} accent={postureScore >= 80 ? 'green' : postureScore >= 50 ? 'yellow' : 'red'} />
            <StatCard label="치명적" value={stats.critical} accent="red" to="/security" query="?tab=findings&severity=critical" sub="클릭 → 필터된 발견" />
            <StatCard label="높음" value={stats.high} accent="orange" to="/security" query="?tab=findings&severity=high" />
            <StatCard label="중간" value={stats.medium} accent="yellow" to="/security" query="?tab=findings&severity=medium" />
            <StatCard label="미해결" value={stats.open} accent="blue" to="/security" query="?tab=findings&status=open" />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mb-6">
            {Object.entries(CATEGORY_INFO).map(([id, info]) => {
              const catRules = rules.filter(r => r.category === id && r.enabled)
              return (
                <button key={id} onClick={() => setTab('rules')} className="card text-left hover:border-gray-300 transition-colors">
                  <div className="flex items-start gap-3">
                    <span className="text-2xl">{info.icon}</span>
                    <div className="flex-1">
                      <h4 className="text-sm font-semibold">{info.ko}</h4>
                      <p className="text-xs text-gray-400">{info.en}</p>
                      <p className="text-xs text-gray-500 mt-1">{info.desc}</p>
                    </div>
                    <span className="badge-gray">{catRules.length} 규칙</span>
                  </div>
                </button>
              )
            })}
          </div>

          <div className="card">
            <h3 className="text-sm font-semibold mb-2">인시던트 대응 · Response</h3>
            <p className="text-xs text-gray-400 mb-3">잠금은 DB 세션 종료 + 릴레이 실경로 차단(세션 상태 게이트) + 하네스 위험도 상향을 함께 수행합니다.</p>
            <button className="btn-danger w-full text-sm" onClick={openLockdown}>⚠ 긴급 조직 잠금 · Emergency Lockdown</button>
          </div>
        </div>
      )}

      {/* RULES */}
      {tab === 'rules' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <div>
              <h3 className="text-lg font-semibold">보안 규칙 관리 · Security Rules</h3>
              <p className="text-xs text-gray-400">{rules.filter(r => r.enabled).length}개 활성 / {rules.length}개 전체 · 토글은 서버에 저장되어 탐지에 반영됩니다 · ★ 고정 우선</p>
            </div>
            <button onClick={() => { setEditingRule(null); setRuleBefore(null); setShowBuilder(true) }} className="btn-primary text-sm">+ 새 규칙 만들기</button>
          </div>

          {showBuilder && <RuleBuilder rule={editingRule} before={ruleBefore} onSave={saveRule} onCancel={() => { setShowBuilder(false); setEditingRule(null); setRuleBefore(null) }} />}

          {/* Rule tester (UX3) */}
          <div className="card mb-4">
            <h4 className="text-sm font-semibold mb-2">🧪 규칙 테스터 · Pattern Tester</h4>
            <div className="flex gap-2 mb-2">
              <select className="input max-w-[260px] text-xs" value={testerRule} onChange={e => setTesterRule(e.target.value)}>
                <option value="">규칙 선택...</option>
                {rules.map(r => <option key={r.id} value={r.pattern}>{r.name} ({r.id})</option>)}
              </select>
              <button onClick={runTester} className="btn-sm btn-primary">테스트</button>
            </div>
            <textarea className="input font-mono text-xs" rows={2} value={testerText} onChange={e => setTesterText(e.target.value)} placeholder="테스트할 텍스트 입력..." />
            {testerMatches.length > 0 && (
              <div className="mt-2 text-xs">
                <span className="text-green-600">✅ {testerMatches.length}개 매치:</span>{' '}
                <span className="font-mono">{testerMatches.join(', ')}</span>
              </div>
            )}
          </div>

          {Object.entries(CATEGORY_INFO).map(([catId, catInfo]) => {
            const catRules = sortedRules.filter(r => r.category === catId)
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
                      <th className="pb-2">★</th><th className="pb-2">규칙 이름</th><th className="pb-2">심각도</th><th className="pb-2">조치</th><th className="pb-2">활성</th><th className="pb-2"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {catRules.map(r => (
                      <tr key={r.id} className="border-b border-gray-50 last:border-0 hover:bg-blue-50/20">
                        <td className="py-2.5"><button onClick={() => toggleFavorite(r.id)} className={`text-sm ${favorites.has(r.id) ? 'text-yellow-500' : 'text-gray-300 hover:text-yellow-400'}`}>{favorites.has(r.id) ? '★' : '☆'}</button></td>
                        <td className="py-2.5"><div className="text-sm font-medium">{r.name}</div><div className="text-xs text-gray-400 font-mono">{r.pattern}</div></td>
                        <td className="py-2.5"><span className={sevBadge(r.severity)}>{SEVERITY_INFO[r.severity]?.ko}</span></td>
                        <td className="py-2.5 text-xs">{r.action}</td>
                        <td className="py-2.5">
                          <button onClick={() => toggleRule(r.id)} className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${r.enabled ? 'bg-patty-600' : 'bg-gray-300'}`}>
                            <span className={`inline-block h-3 w-3 rounded-full bg-white transition-transform ${r.enabled ? 'translate-x-5' : 'translate-x-1'}`} />
                          </button>
                        </td>
                        <td className="py-2.5">
                          <button onClick={() => { setEditingRule(r); setRuleBefore({ ...r }); setShowBuilder(true) }} className="text-xs text-blue-600 hover:underline">편집 (diff)</button>
                        </td>
                      </tr>
                    ))}
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
          <div className="flex justify-between items-center mb-4 flex-wrap gap-2">
            <h3 className="text-lg font-semibold">보안 발견 목록 · Findings ({findingTotal})</h3>
            <div className="flex gap-2 items-center flex-wrap">
              <button onClick={() => setIncidentModal(true)} className="btn-sm btn-primary">+ 인시던트 생성</button>
              <button onClick={() => exportCSV(`security_findings_${new Date().toISOString().slice(0,10)}.csv`, ['timestamp', 'type', 'severity', 'title', 'status', 'direction', 'session_id'], findings.map(f => [f.occurred_at, f.finding_type, f.severity, f.title_ko || f.title, f.status, f.direction || 'request', f.session_id || '']))} className="btn-sm btn-secondary">CSV</button>
            </div>
          </div>

          {/* Server-side filters (UX5) */}
          <div className="flex gap-2 mb-4 flex-wrap">
            <select className="input max-w-[130px] text-xs" value={findingFilters.severity} onChange={e => setFindingFilters({ ...findingFilters, severity: e.target.value })}>
              <option value="">심각도: 전체</option>
              <option value="critical">치명적</option><option value="high">높음</option><option value="medium">중간</option><option value="low">낮음</option>
            </select>
            <select className="input max-w-[140px] text-xs" value={findingFilters.status} onChange={e => setFindingFilters({ ...findingFilters, status: e.target.value })}>
              <option value="">상태: 전체</option>
              <option value="open">미해결</option><option value="investigating">조사 중</option><option value="resolved">해결</option><option value="suppressed">억제</option>
            </select>
            <select className="input max-w-[150px] text-xs" value={findingFilters.type} onChange={e => setFindingFilters({ ...findingFilters, type: e.target.value })}>
              <option value="">유형: 전체</option>
              {[...new Set(findings.map(f => f.finding_type))].map(t => <option key={t} value={t}>{t}</option>)}
            </select>
            <input type="date" className="input max-w-[150px] text-xs" value={findingFilters.from} onChange={e => setFindingFilters({ ...findingFilters, from: e.target.value })} />
            <span className="text-xs text-gray-400">~</span>
            <input type="date" className="input max-w-[150px] text-xs" value={findingFilters.to} onChange={e => setFindingFilters({ ...findingFilters, to: e.target.value })} />
          </div>

          {selectedFindings.size > 0 && (
            <div className="flex items-center gap-2 mb-3 p-2 bg-blue-50 rounded-lg">
              <span className="text-xs text-blue-700">{selectedFindings.size}개 선택됨</span>
              <button onClick={() => bulkFindings('resolved')} className="btn-sm btn-secondary">일괄 해결</button>
              <button onClick={() => bulkFindings('false_positive')} className="btn-sm btn-secondary">일괄 오탐 처리</button>
              <button onClick={() => setSelectedFindings(new Set())} className="btn-sm btn-secondary">취소</button>
            </div>
          )}

          {findings.length === 0 ? (
            <div className="text-center py-12">
              <EmptyState icon="🛡" title="발견이 없습니다" message="발견은 릴레이가 실세션 트래픽을 검사할 때 생성됩니다 — 스캐너 탭에서 수동 검사로 규칙을 검증해 보세요" action={{ label: '스캐너로 이동', onClick: () => setTab('scanner') }} />
            </div>
          ) : (
            <table className="w-full overflow-x-auto block">
              <thead><tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                <th className="pb-3 w-8"><input type="checkbox" onChange={e => { if (e.target.checked) setSelectedFindings(new Set(findings.map(f => f.id))); else setSelectedFindings(new Set()) }} /></th>
                <th className="pb-3">유형</th><th className="pb-3">심각도</th><th className="pb-3">제목</th><th className="pb-3">방향</th><th className="pb-3">상태</th><th className="pb-3">시간</th>
              </tr></thead>
              <tbody>
                {findings.map(f => (
                  <tr key={f.id} className="border-b border-gray-100 last:border-0 cursor-pointer hover:bg-blue-50/30" onClick={() => viewFindingDetail(f.id)}>
                    <td className="py-3" onClick={e => e.stopPropagation()}><input type="checkbox" checked={selectedFindings.has(f.id)} onChange={() => { const n = new Set(selectedFindings); if (n.has(f.id)) n.delete(f.id); else n.add(f.id); setSelectedFindings(n) }} /></td>
                    <td className="py-3 text-sm font-mono">{f.finding_type}</td>
                    <td className="py-3"><span className={sevBadge(f.severity)}>{f.severity}</span></td>
                    <td className="py-3 text-sm">
                      <Link to={`/findings/${f.id}`} className="text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>{f.title_ko || f.title}</Link>
                      {f.session_id && <div className="text-[10px] text-gray-400">세션: <Link to={`/sessions/${f.session_id}`} className="hover:underline" onClick={e => e.stopPropagation()}>{f.session_id.slice(0, 16)}</Link></div>}
                    </td>
                    <td className="py-3 text-xs">{f.direction === 'response' ? '응답' : '요청'}</td>
                    <td className="py-3"><span className={statusBadge(f.status)}>{f.status}</span>{f.suppressed && <span className="text-[10px] text-gray-400 block">억제됨</span>}</td>
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
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="card">
            <h3 className="text-lg font-semibold mb-3">보안 검사 도구 · Scanner</h3>
            <p className="text-sm text-gray-500 mb-4">텍스트를 입력하여 보안 규칙을 테스트합니다.</p>
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

          <div className="card">
            <h3 className="text-lg font-semibold mb-3">세션 전체 스캔 · Scan a Session (UX8)</h3>
            <p className="text-sm text-gray-500 mb-4">기록된 세션의 모든 요청/응답 교환을 재검사합니다.</p>
            <select className="input mb-3" value={sessionPick} onChange={e => setSessionPick(e.target.value)}>
              <option value="">세션 선택...</option>
              {sessions.map(s => <option key={s.id} value={s.session_id || s.id}>{s.title || '제목 없음'} ({s.session_id?.slice(0, 12)})</option>)}
            </select>
            <button onClick={runSessionScan} disabled={!sessionPick} className="btn-primary text-sm">세션 스캔 실행</button>
            <p className="text-[10px] text-gray-400 mt-3">응답 방향 스캔은 출력 유출(exfiltration) 탐지에 사용됩니다 (§16.5).</p>
          </div>
        </div>
      )}

      {/* INCIDENTS */}
      {tab === 'incidents' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <p className="text-xs text-gray-400">인시던트 수명주기: open → investigating → contained → resolved (§15.2-15.4)</p>
            <button onClick={() => setIncidentModal(true)} className="btn-primary text-sm">+ 인시던트 생성</button>
          </div>
          {incidents.length === 0 ? (
            <div className="card text-center py-12">
              <EmptyState icon="🚨" title="인시던트가 없습니다" message="발견들을 그룹화해 인시던트를 만들면 봉쇄/해결 워크플로를 탈 수 있습니다" action={{ label: '+ 인시던트 생성', onClick: () => setIncidentModal(true) }} />
            </div>
          ) : (
            <div className="space-y-2">
              {incidents.map(inc => (
                <div key={inc.id} className={`card flex items-center gap-3 py-3 px-4 ${inc.status === 'open' ? 'border-l-4 border-l-red-500' : ''}`}>
                  <span className="text-xl">🚨</span>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-medium">{inc.title_ko || inc.title}</span>
                      <span className={sevBadge(inc.severity)}>{inc.severity}</span>
                      <span className={inc.status === 'open' ? 'badge-red' : inc.status === 'contained' ? 'badge-yellow' : 'badge-green'}>{inc.status}</span>
                    </div>
                    <p className="text-xs text-gray-500 truncate">{inc.description}</p>
                    <div className="text-[10px] text-gray-400">발견 {inc.finding_ids?.length || 0}개 · {formatRelative(inc.first_seen_at)}</div>
                  </div>
                  <div className="flex gap-2 flex-shrink-0 flex-wrap">
                    {inc.status === 'open' && <button onClick={() => containIncident(inc, 'session')} className="btn-sm btn-secondary">세션 봉쇄</button>}
                    {inc.status === 'open' && <button onClick={() => containIncident(inc, 'harness')} className="btn-sm btn-secondary">하네스 봉쇄</button>}
                    {inc.status === 'open' && <button onClick={() => containIncident(inc, 'org_lockdown')} className="btn-sm btn-danger">조직 잠금</button>}
                    {(inc.status === 'open' || inc.status === 'contained') && <button onClick={() => resolveIncident(inc)} className="btn-sm btn-primary">해결</button>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ALERTS + LEXICON */}
      {tab === 'alerts' && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <div className="flex justify-between items-center mb-3">
              <h3 className="text-lg font-semibold">알림 라우팅 · Alert Routing (§10C.14)</h3>
              <button onClick={() => setAlertModal(true)} className="btn-primary text-sm">+ 라우트 추가</button>
            </div>
            {alerts.length === 0 ? (
              <div className="card text-center py-10">
                <EmptyState icon="📣" title="알림 라우트가 없습니다" message="Slack/웹훅/SIEM 대상으로 발견을 실시간 전달하세요" action={{ label: '+ 라우트 추가', onClick: () => setAlertModal(true) }} />
              </div>
            ) : (
              <div className="space-y-2">
                {alerts.map(a => (
                  <div key={a.id} className="card flex items-center gap-3 py-3 px-4">
                    <span>{a.type === 'slack' ? '💬' : a.type === 'siem' ? '📡' : '🪝'}</span>
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-medium">{a.name} <span className="text-xs text-gray-400">{a.type}</span></div>
                      <div className="text-[10px] text-gray-400 font-mono truncate">{a.target}</div>
                      <div className="text-[10px] text-gray-400">심각도: {JSON.parse(a.severities || '[]').length ? JSON.parse(a.severities).join(', ') : '전체'}</div>
                    </div>
                    <button onClick={async () => { await api.deleteSecurityAlert(a.id); showToast('삭제됨', 'info'); loadAlerts() }} className="text-xs text-red-600 hover:underline">삭제</button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="card">
            <h3 className="text-lg font-semibold mb-2">한국 PII 렉시콘 · Lexicon (§16.3)</h3>
            <p className="text-xs text-gray-400 mb-3">
              현재 버전: <span className="font-semibold">{lexicon?.version || 'builtin'}</span> — 조직별 패턴 오버라이드가 탐지기에 반영됩니다. rule_id → 정규식 맵을 편집하세요.
            </p>
            <textarea className="input font-mono text-xs mb-3" rows={10} value={lexiconForm} onChange={e => setLexiconForm(e.target.value)} />
            <div className="flex gap-2">
              <button onClick={publishLexicon} className="btn-primary text-sm">새 버전 발행</button>
              <button onClick={() => setLexiconForm(JSON.stringify({}, null, 2))} className="btn-sm btn-secondary">초기화</button>
            </div>
          </div>
        </div>
      )}

      {/* Finding detail modal */}
      <Modal open={!!findingDetail} title="발견 상세 · Finding Detail" onClose={() => setFindingDetail(null)} size="lg"
        footer={<ModalFooter onCancel={() => setFindingDetail(null)} onConfirm={() => setFindingDetail(null)} confirmLabel="닫기" />}>
        {findingDetail?.finding && (
          <div className="text-sm space-y-3">
            <div className="flex items-center gap-2">
              <span className={sevBadge(findingDetail.finding.severity)}>{findingDetail.finding.severity}</span>
              <span className={statusBadge(findingDetail.finding.status)}>{findingDetail.finding.status}</span>
              {findingDetail.finding.direction && <span className="badge-gray">{findingDetail.finding.direction}</span>}
            </div>
            <h4 className="font-semibold">{findingDetail.finding.title_ko || findingDetail.finding.title}</h4>
            <p className="text-xs text-gray-500">{findingDetail.finding.description}</p>
            {findingDetail.session && (
              <div className="text-xs">
                세션: <Link to={`/sessions/${findingDetail.session.session_id || findingDetail.session.id}`} className="text-blue-600 hover:underline">{findingDetail.session.title || findingDetail.session.session_id}</Link>
                {findingDetail.user && <> · 사용자: <Link to={`/users/${findingDetail.user.id}`} className="text-blue-600 hover:underline">{findingDetail.user.name_ko || findingDetail.user.name}</Link></>}
                {findingDetail.harness && <> · 하네스: <Link to={`/harnesses/${findingDetail.harness.id}`} className="text-blue-600 hover:underline font-mono">{findingDetail.harness.harness_id}</Link></>}
              </div>
            )}
            <div className="flex gap-2 flex-wrap">
              {findingDetail.finding.status === 'open' && <button onClick={() => updateFindingStatus(findingDetail.finding.id, 'investigating')} className="btn-sm btn-primary">조사 시작</button>}
              {findingDetail.finding.status !== 'resolved' && <button onClick={() => updateFindingStatus(findingDetail.finding.id, 'resolved')} className="btn-sm btn-secondary">해결</button>}
              {findingDetail.finding.status !== 'false_positive' && <button onClick={() => updateFindingStatus(findingDetail.finding.id, 'false_positive')} className="btn-sm btn-secondary">오탐 처리</button>}
              {!findingDetail.finding.suppressed && <button onClick={() => { setSuppressTarget(findingDetail.finding); setFindingDetail(null) }} className="btn-sm btn-secondary">억제</button>}
            </div>
          </div>
        )}
      </Modal>

      {/* Suppress modal */}
      <Modal open={!!suppressTarget} title="억제 · Suppress (Accept Risk)" subtitle={suppressTarget?.title_ko || suppressTarget?.title} onClose={() => setSuppressTarget(null)} size="sm"
        footer={<ModalFooter onCancel={() => setSuppressTarget(null)} onConfirm={submitSuppress} confirmLabel="억제 실행" disabled={!suppressForm.reason.trim()} />}>
        <div className="space-y-3">
          <label className="label">사유 · Reason</label>
          <input className="input" value={suppressForm.reason} onChange={e => setSuppressForm({ ...suppressForm, reason: e.target.value })} placeholder="예: 내부 테스트 데이터" />
          <label className="label">기간 · Days</label>
          <select className="input" value={suppressForm.days} onChange={e => setSuppressForm({ ...suppressForm, days: Number(e.target.value) })}>
            <option value={7}>7일</option><option value={30}>30일</option><option value={90}>90일</option><option value={365}>1년</option>
          </select>
        </div>
      </Modal>

      {/* Lockdown modal (2-step + scope + impact) */}
      <Modal open={lockdownModal} title="⚠ 긴급 잠금 · Emergency Lockdown" onClose={() => setLockdownModal(false)} size="md"
        footer={<ModalFooter onCancel={() => setLockdownModal(false)} onConfirm={runLockdown} confirmLabel="⚠ 잠금 실행" danger />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">범위 · Scope</label>
              <select className="input" value={lockdownForm.scope} onChange={async e => { const scope = e.target.value; setLockdownForm({ ...lockdownForm, scope, project_id: '' }); setLockdownImpact(await api.lockdownImpact(scope)) }}>
                <option value="org">전체 조직</option>
                <option value="project">특정 프로젝트</option>
              </select>
            </div>
            {lockdownForm.scope === 'project' && (
              <div><label className="label">프로젝트</label>
                <select className="input" value={lockdownForm.project_id} onChange={async e => { setLockdownForm({ ...lockdownForm, project_id: e.target.value }); setLockdownImpact(await api.lockdownImpact('project', e.target.value)) }}>
                  <option value="">선택...</option>
                  {projects.map(pr => <option key={pr.id} value={pr.id}>{pr.name_ko || pr.name}</option>)}
                </select>
              </div>
            )}
          </div>
          <div><label className="label">사유 · Reason</label><input className="input" value={lockdownForm.reason} onChange={e => setLockdownForm({ ...lockdownForm, reason: e.target.value })} placeholder="예: 자격증명 유출 의심" /></div>
          {lockdownImpact && (
            <div className="bg-red-50 border border-red-200 rounded-lg p-3 text-xs space-y-1">
              <div className="font-semibold text-red-700">영향 미리보기 · Impact:</div>
              <div>· {lockdownImpact.active_sessions}개 활성 세션이 종료됩니다</div>
              <div>· {lockdownImpact.active_harnesses}개 하네스 위험도가 high로 상향됩니다</div>
            </div>
          )}
        </div>
      </Modal>

      {/* Incident create modal */}
      <Modal open={incidentModal} title="인시던트 생성 · Create Incident" onClose={() => setIncidentModal(false)} size="md"
        footer={<ModalFooter onCancel={() => setIncidentModal(false)} onConfirm={createIncident} confirmLabel="생성" disabled={!incidentForm.title} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">제목 · Title</label><input className="input" value={incidentForm.title} onChange={e => setIncidentForm({ ...incidentForm, title: e.target.value })} placeholder="credential leak" /></div>
            <div><label className="label">심각도</label><select className="input" value={incidentForm.severity} onChange={e => setIncidentForm({ ...incidentForm, severity: e.target.value })}>
              <option value="critical">critical</option><option value="high">high</option><option value="medium">medium</option><option value="low">low</option>
            </select></div>
            <div><label className="label">카테고리</label><select className="input" value={incidentForm.category} onChange={e => setIncidentForm({ ...incidentForm, category: e.target.value })}>
              <option value="credential_leak">credential_leak</option><option value="pii_exposure">pii_exposure</option><option value="injection">injection</option><option value="exfiltration">exfiltration</option>
            </select></div>
            <div><label className="label">그룹화할 발견 (선택)</label>
              <div className="max-h-24 overflow-y-auto border border-gray-200 rounded p-2">
                {findings.filter(f => f.status === 'open').slice(0, 20).map(f => (
                  <label key={f.id} className="flex items-center gap-1 text-xs">
                    <input type="checkbox" checked={incidentForm.finding_ids.includes(f.id)} onChange={() => { const ids = [...incidentForm.finding_ids]; const i = ids.indexOf(f.id); if (i >= 0) ids.splice(i, 1); else ids.push(f.id); setIncidentForm({ ...incidentForm, finding_ids: ids }) }} />
                    {f.title_ko || f.title}
                  </label>
                ))}
              </div>
            </div>
          </div>
        </div>
      </Modal>

      {/* Alert create modal */}
      <Modal open={alertModal} title="알림 라우트 추가 · Add Alert Route" onClose={() => setAlertModal(false)} size="sm"
        footer={<ModalFooter onCancel={() => setAlertModal(false)} onConfirm={submitAlert} confirmLabel="추가" disabled={!alertForm.name || !alertForm.target} />}>
        <div className="space-y-3">
          <div><label className="label">이름 · Name</label><input className="input" value={alertForm.name} onChange={e => setAlertForm({ ...alertForm, name: e.target.value })} placeholder="oncall-webhook" /></div>
          <div><label className="label">유형 · Type</label><select className="input" value={alertForm.type} onChange={e => setAlertForm({ ...alertForm, type: e.target.value })}>
            <option value="webhook">웹훅 (온콜)</option><option value="slack">Slack 웹훅</option><option value="siem">SIEM 포워더 (§32.4)</option>
          </select></div>
          <div><label className="label">대상 URL · Target</label><input className="input" value={alertForm.target} onChange={e => setAlertForm({ ...alertForm, target: e.target.value })} placeholder="https://hooks.slack.com/..." /></div>
          <div><label className="label">라우팅 심각도</label>
            <div className="flex gap-2 flex-wrap">
              {['critical', 'high', 'medium', 'low'].map(s => (
                <label key={s} className="text-xs flex items-center gap-1">
                  <input type="checkbox" checked={alertForm.severities.includes(s)} onChange={() => { const list = [...alertForm.severities]; const i = list.indexOf(s); if (i >= 0) list.splice(i, 1); else list.push(s); setAlertForm({ ...alertForm, severities: list }) }} />
                  {s}
                </label>
              ))}
            </div>
          </div>
        </div>
      </Modal>
    </div>
  )
}

// ─── Rule Builder with diff view (UX13) ───────────────────────────
function RuleBuilder({ rule, before, onSave, onCancel }: { rule: Rule | null; before: Rule | null; onSave: (r: Rule) => void; onCancel: () => void }) {
  const [category, setCategory] = useState(rule?.category || 'pii')
  const [presetId, setPresetId] = useState('')
  const [name, setName] = useState(rule?.name || '')
  const [pattern, setPattern] = useState(rule?.pattern || '')
  const [severity, setSeverity] = useState(rule?.severity || 'high')
  const [action, setAction] = useState(rule?.action || 'block')
  const [enabled, setEnabled] = useState(rule?.enabled ?? true)

  const presets = RULE_PRESETS[category] || []

  const handlePresetChange = (id: string) => {
    setPresetId(id)
    const p = presets.find(x => x.id === id)
    if (p) { setPattern(p.value); setName(p.label) }
  }

  const handleSave = () => {
    if (!name || !pattern) { showToast('이름과 패턴을 입력하세요', 'error'); return }
    onSave({ id: rule?.id || `${category}-${Date.now()}`, name, nameEn: rule?.nameEn || name, category, severity, action, pattern, enabled })
  }

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4 animate-fadeIn" onClick={onCancel}>
      <div className="bg-white rounded-xl shadow-xl max-w-lg w-full max-h-[85vh] overflow-y-auto animate-scaleIn" onClick={e => e.stopPropagation()}>
        <div className="p-5 border-b border-gray-100 flex items-center justify-between sticky top-0 bg-white">
          <h3 className="font-semibold">{rule ? '규칙 편집' : '새 보안 규칙'}</h3>
          <button onClick={onCancel} className="text-gray-400 hover:text-gray-600">✕</button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="label">위협 카테고리 · Threat Category</label>
            <select className="input" value={category} onChange={e => { setCategory(e.target.value); setPresetId('') }} disabled={!!rule}>
              {Object.entries(CATEGORY_INFO).map(([id, info]) => <option key={id} value={id}>{info.icon} {info.ko} ({info.en})</option>)}
            </select>
          </div>
          <div>
            <label className="label">프리셋 라이브러리 · Preset Library</label>
            <select className="input" value={presetId} onChange={e => handlePresetChange(e.target.value)}>
              <option value="">선택하세요...</option>
              {presets.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}
            </select>
          </div>
          <div><label className="label">규칙 이름 · Name</label><input className="input" value={name} onChange={e => setName(e.target.value)} /></div>
          <div>
            <label className="label">패턴 · Pattern (정규식)</label>
            <input className="input font-mono text-xs" value={pattern} onChange={e => setPattern(e.target.value)} />
            {before && pattern !== before.pattern && (
              <div className="mt-1 text-[10px] text-gray-500">
                <span className="text-red-500 line-through">{before.pattern}</span>
                <span className="mx-1">→</span>
                <span className="text-green-600">{pattern}</span>
              </div>
            )}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">심각도 · Severity</label>
              <select className="input" value={severity} onChange={e => setSeverity(e.target.value)}>
                {Object.entries(SEVERITY_INFO).map(([id, info]) => <option key={id} value={id}>{info.ko} — {info.desc}</option>)}
              </select>
            </div>
            <div><label className="label">조치 · Action</label>
              <select className="input" value={action} onChange={e => setAction(e.target.value)}>
                <option value="block">차단</option><option value="mask">마스킹</option><option value="review">검토 요청</option>
              </select>
            </div>
          </div>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} className="w-4 h-4" />
            규칙 활성화 · Enable
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

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
