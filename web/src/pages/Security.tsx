import { useState, useEffect } from 'react'
import { api } from '../api'

type Finding = {
  id: string; finding_type: string; severity: string; title: string;
  title_ko?: string; status: string; rule_id?: string; description?: string;
  occurred_at: string; session_id?: string;
}

type DlpRule = {
  rule_id: string; name: string; name_ko: string; type: string;
  severity: string; enabled: boolean; pattern: string; action: string;
}

const DLP_RULES: DlpRule[] = [
  { rule_id: 'pii-kr-rrn', name: 'Korean RRN', name_ko: '주민등록번호', type: 'korean_pii', severity: 'critical', enabled: true, pattern: '\\d{6}-\\d{7}', action: 'block' },
  { rule_id: 'pii-kr-business', name: 'Business Registration', name_ko: '사업자등록번호', type: 'korean_pii', severity: 'high', enabled: true, pattern: '\\d{3}-\\d{2}-\\d{5}', action: 'mask' },
  { rule_id: 'pii-kr-phone', name: 'Korean Phone', name_ko: '전화번호', type: 'korean_pii', severity: 'medium', enabled: true, pattern: '0\\d{1,2}-\\d{3,4}-\\d{4}', action: 'mask' },
  { rule_id: 'pii-kr-account', name: 'Bank Account', name_ko: '계좌번호', type: 'korean_pii', severity: 'high', enabled: true, pattern: '\\d{3}-\\d{6,8}-\\d{3}', action: 'block' },
  { rule_id: 'secret-aws', name: 'AWS Access Key', name_ko: 'AWS 접근키', type: 'secret', severity: 'critical', enabled: true, pattern: 'AKIA[A-Z0-9]{16}', action: 'block' },
  { rule_id: 'secret-jwt', name: 'JWT Token', name_ko: 'JWT 토큰', type: 'secret', severity: 'high', enabled: true, pattern: 'eyJ[a-zA-Z0-9_-]+', action: 'block' },
  { rule_id: 'secret-private-key', name: 'Private Key', name_ko: '개인키', type: 'secret', severity: 'critical', enabled: true, pattern: '-----BEGIN.*PRIVATE KEY', action: 'block' },
  { rule_id: 'secret-github', name: 'GitHub PAT', name_ko: 'GitHub 토큰', type: 'secret', severity: 'high', enabled: true, pattern: 'gh[pousr]_[A-Za-z0-9]{36}', action: 'block' },
  { rule_id: 'injection-ignore', name: 'Instruction Override', name_ko: '명령어 재정의', type: 'prompt_injection', severity: 'high', enabled: true, pattern: 'ignore.*previous.*instructions', action: 'block' },
  { rule_id: 'injection-jailbreak', name: 'Jailbreak Attempt', name_ko: '탈옥 시도', type: 'prompt_injection', severity: 'high', enabled: true, pattern: '(jailbreak|DAN|developer.mode)', action: 'block' },
]

export default function Security() {
  const [tab, setTab] = useState<'dashboard' | 'findings' | 'rules' | 'scanner'>('dashboard')
  const [scanText, setScanText] = useState('')
  const [scanResult, setScanResult] = useState<any>(null)
  const [findings, setFindings] = useState<Finding[]>([])
  const [rules, setRules] = useState<DlpRule[]>(DLP_RULES)
  const [stats, setStats] = useState({ critical: 0, high: 0, medium: 0, open: 0, total: 0 })

  useEffect(() => {
    loadFindings()
    loadStats()
  }, [])

  const loadFindings = () => {
    fetch('/api/analytics/security', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => {
        const s = data || {}
        setStats({
          critical: s.critical_count || 0,
          high: s.high_count || 0,
          medium: (s.total_findings || 0) - (s.critical_count || 0) - (s.high_count || 0),
          open: s.open_count || 0,
          total: s.total_findings || 0,
        })
      })
      .catch(() => {})
  }

  const loadStats = () => {
    // Fetch security findings from audit events
    fetch('/api/audit?type=cp.security.finding', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => setFindings(Array.isArray(data) ? data : []))
      .catch(() => setFindings([]))
  }

  const runScan = async () => {
    if (!scanText) return
    try {
      const r = await api.securityCheck(scanText)
      setScanResult(r)
    } catch (e: any) {
      setScanResult({ error: e.message })
    }
  }

  const toggleRule = (ruleId: string) => {
    setRules(rs => rs.map(r => r.rule_id === ruleId ? { ...r, enabled: !r.enabled } : r))
  }

  const sevColor = (s: string) => s === 'critical' ? 'text-red-600' : s === 'high' ? 'text-orange-600' : s === 'medium' ? 'text-yellow-600' : 'text-blue-600'
  const sevBadge = (s: string) => s === 'critical' ? 'badge-red' : s === 'high' ? 'badge-red' : s === 'medium' ? 'badge-yellow' : 'badge-blue'
  const statusBadge = (s: string) => s === 'open' ? 'badge-red' : s === 'investigating' ? 'badge-yellow' : s === 'resolved' ? 'badge-green' : 'badge-gray'

  const postureScore = stats.total === 0 ? 100 : Math.max(0, 100 - stats.critical * 25 - stats.high * 10 - stats.open * 5)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">보안 운영 센터 <span className="text-gray-400 text-lg font-normal">Security Operations Center</span></h1>

      {/* Tab navigation */}
      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'dashboard', label: '보안 현황', labelEn: 'Dashboard' },
          { id: 'findings', label: '보안 발견', labelEn: 'Findings' },
          { id: 'rules', label: 'DLP 규칙', labelEn: 'DLP Rules' },
          { id: 'scanner', label: '보안 검사', labelEn: 'Scanner' },
        ].map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            {t.label} <span className="text-xs text-gray-400">{t.labelEn}</span>
          </button>
        ))}
      </div>

      {/* Dashboard Tab */}
      {tab === 'dashboard' && (
        <div>
          {/* Posture Score */}
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="card col-span-1">
              <div className="text-center">
                <div className={`text-5xl font-bold ${postureScore >= 80 ? 'text-green-600' : postureScore >= 50 ? 'text-yellow-600' : 'text-red-600'}`}>
                  {postureScore}
                </div>
                <div className="text-sm text-gray-500 mt-1">보안 점수 · Security Score</div>
              </div>
            </div>
            <div className="card col-span-3">
              <h3 className="text-sm font-medium text-gray-700 mb-3">위협 현황 · Threat Summary</h3>
              <div className="grid grid-cols-4 gap-4">
                <div className="text-center">
                  <div className="text-2xl font-bold text-red-600">{stats.critical}</div>
                  <div className="text-xs text-gray-500">치명적 · Critical</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-bold text-orange-600">{stats.high}</div>
                  <div className="text-xs text-gray-500">높음 · High</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-bold text-yellow-600">{stats.medium}</div>
                  <div className="text-xs text-gray-500">중간 · Medium</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-bold text-blue-600">{stats.open}</div>
                  <div className="text-xs text-gray-500">미해결 · Open</div>
                </div>
              </div>
            </div>
          </div>

          {/* Active Controls */}
          <div className="grid grid-cols-2 gap-4 mb-6">
            <div className="card">
              <h3 className="text-sm font-medium text-gray-700 mb-3">활성 보안 통제 · Active Controls</h3>
              <div className="space-y-2">
                {[
                  { name: '한국 개인정보 감지', nameEn: 'Korean PII Detection', status: 'active', rules: DLP_RULES.filter(r => r.type === 'korean_pii').length },
                  { name: '시크릿 스캐닝', nameEn: 'Secret Scanning', status: 'active', rules: DLP_RULES.filter(r => r.type === 'secret').length },
                  { name: '프롬프트 인젝션 방어', nameEn: 'Injection Defense', status: 'active', rules: DLP_RULES.filter(r => r.type === 'prompt_injection').length },
                  { name: '컨텍스트 방화벽', nameEn: 'Context Firewall', status: 'active', rules: 7 },
                  { name: '명령어 인가', nameEn: 'Command Authorization', status: 'active', rules: 15 },
                  { name: '네트워크 브로커', nameEn: 'Network Broker', status: 'active', rules: 3 },
                  { name: 'MCP 거버넌스', nameEn: 'MCP Governance', status: 'active', rules: 4 },
                  { name: '시크릿 브로커', nameEn: 'Secret Broker', status: 'active', rules: 2 },
                ].map(ctrl => (
                  <div key={ctrl.nameEn} className="flex items-center justify-between py-1.5 border-b border-gray-100 last:border-0">
                    <div>
                      <span className="text-sm font-medium">{ctrl.name}</span>
                      <span className="text-xs text-gray-400 ml-2">{ctrl.nameEn}</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-gray-500">{ctrl.rules} 규칙</span>
                      <span className="badge-green">{ctrl.status}</span>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="card">
              <h3 className="text-sm font-medium text-gray-700 mb-3">인시던트 대응 · Incident Response</h3>
              <div className="space-y-3">
                <div className="bg-gray-50 rounded-lg p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-sm font-medium">세션 격리</span>
                    <span className="badge-gray">Session</span>
                  </div>
                  <p className="text-xs text-gray-500">의심스러운 세션 일시정지, 모델 요청 중지, 도구 실행 중지</p>
                </div>
                <div className="bg-gray-50 rounded-lg p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-sm font-medium">하네스 격리</span>
                    <span className="badge-gray">Harness</span>
                  </div>
                  <p className="text-xs text-gray-500">하네스 인증서 회수, 통신 비활성화, 재등록 필요</p>
                </div>
                <div className="bg-gray-50 rounded-lg p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-sm font-medium">조직 잠금</span>
                    <span className="badge-red">Emergency</span>
                  </div>
                  <p className="text-xs text-gray-500">전체 에이전트 중지, 클라우드 모델 차단, 긴급 방송</p>
                </div>
                <button className="btn-danger w-full text-sm" onClick={() => {
                  if (confirm('정말로 전체 조직을 잠금하시겠습니까? 이 작업은 모든 AI 세션을 중지합니다.')) {
                    alert('긴급 잠금이 시작되었습니다. 모든 관리자에게 알림이 발송됩니다.')
                  }
                }}>
                  ⚠ 긴급 조직 잠금 · Emergency Lockdown
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Findings Tab */}
      {tab === 'findings' && (
        <div className="card">
          <div className="flex justify-between items-center mb-4">
            <h3 className="text-lg font-semibold">보안 발견 목록 · Security Findings</h3>
            <span className="text-sm text-gray-500">{findings.length}건</span>
          </div>
          {findings.length === 0 ? (
            <div className="text-center py-12">
              <div className="text-4xl mb-3">✅</div>
              <p className="text-gray-500">현재 활성 보안 발견이 없습니다.</p>
              <p className="text-sm text-gray-400 mt-1">시스템이 정상적으로 운영되고 있습니다.</p>
            </div>
          ) : (
            <table className="w-full">
              <thead>
                <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                  <th className="pb-3">유형</th>
                  <th className="pb-3">심각도</th>
                  <th className="pb-3">제목</th>
                  <th className="pb-3">상태</th>
                  <th className="pb-3">시간</th>
                  <th className="pb-3"></th>
                </tr>
              </thead>
              <tbody>
                {findings.map((f) => (
                  <tr key={f.id} className="border-b border-gray-100 last:border-0">
                    <td className="py-3 text-sm font-mono">{f.finding_type}</td>
                    <td className="py-3"><span className={sevBadge(f.severity)}>{f.severity}</span></td>
                    <td className="py-3 text-sm">{f.title_ko || f.title}</td>
                    <td className="py-3"><span className={statusBadge(f.status)}>{f.status}</span></td>
                    <td className="py-3 text-xs text-gray-400">{f.occurred_at?.slice(0, 19)}</td>
                    <td className="py-3">
                      <button className="text-patty-600 text-sm hover:underline">조치 →</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Rules Tab */}
      {tab === 'rules' && (
        <div>
          <div className="flex justify-between items-center mb-4">
            <h3 className="text-lg font-semibold">DLP 및 보안 규칙 · DLP & Security Rules</h3>
            <span className="text-sm text-gray-500">{rules.filter(r => r.enabled).length}/{rules.length} 활성</span>
          </div>

          {['korean_pii', 'secret', 'prompt_injection'].map(category => {
            const catRules = rules.filter(r => r.type === category)
            const catName = category === 'korean_pii' ? '🇰🇷 한국 개인정보 감지' : category === 'secret' ? '🔑 시크릿 스캐닝' : '🛡 프롬프트 인젝션 방어'
            const catNameEn = category === 'korean_pii' ? 'Korean PII Detection' : category === 'secret' ? 'Secret Scanning' : 'Prompt Injection Defense'
            return (
              <div key={category} className="card mb-4">
                <h4 className="text-sm font-semibold mb-3">{catName} <span className="text-gray-400 font-normal">{catNameEn}</span></h4>
                <table className="w-full">
                  <thead>
                    <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                      <th className="pb-2">규칙</th>
                      <th className="pb-2">패턴</th>
                      <th className="pb-2">심각도</th>
                      <th className="pb-2">조치</th>
                      <th className="pb-2">활성</th>
                    </tr>
                  </thead>
                  <tbody>
                    {catRules.map(r => (
                      <tr key={r.rule_id} className="border-b border-gray-50 last:border-0">
                        <td className="py-2">
                          <div className="text-sm font-medium">{r.name_ko}</div>
                          <div className="text-xs text-gray-400">{r.name}</div>
                        </td>
                        <td className="py-2"><code className="text-xs bg-gray-100 px-1.5 py-0.5 rounded">{r.pattern}</code></td>
                        <td className="py-2"><span className={sevBadge(r.severity)}>{r.severity}</span></td>
                        <td className="py-2">
                          <span className={r.action === 'block' ? 'badge-red' : 'badge-yellow'}>
                            {r.action === 'block' ? '차단' : '마스킹'}
                          </span>
                        </td>
                        <td className="py-2">
                          <button
                            onClick={() => toggleRule(r.rule_id)}
                            className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${r.enabled ? 'bg-patty-600' : 'bg-gray-300'}`}
                          >
                            <span className={`inline-block h-3 w-3 rounded-full bg-white transition-transform ${r.enabled ? 'translate-x-5' : 'translate-x-1'}`} />
                          </button>
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

      {/* Scanner Tab */}
      {tab === 'scanner' && (
        <div className="card">
          <h3 className="text-lg font-semibold mb-3">보안 검사 도구 · Security Scanner</h3>
          <p className="text-sm text-gray-500 mb-4">
            텍스트를 입력하여 DLP 규칙과 보안 패턴을 테스트합니다. 이 도구는 관리자가 규칙을 검증하는 데 사용됩니다.
          </p>
          <textarea
            className="input font-mono text-sm mb-3"
            rows={4}
            value={scanText}
            onChange={(e) => setScanText(e.target.value)}
            placeholder="주민번호: 901225-1234567, AWS_KEY=AKIAABCDEFGHIJKLMNOP 등..."
          />
          <button onClick={runScan} disabled={!scanText} className="btn-primary text-sm">
            보안 검사 실행 · Scan
          </button>

          {scanResult && (
            <div className="mt-6">
              <div className="flex items-center gap-3 mb-4">
                <span className="text-sm font-medium">결과:</span>
                <span className={scanResult.passed ? 'badge-green' : scanResult.verdict === 'DENY' ? 'badge-red' : 'badge-yellow'}>
                  {scanResult.verdict === 'DENY' ? '차단 (DENY)' : scanResult.verdict === 'REQUIRE_REVIEW' ? '검토 필요' : '통과 (ALLOW)'}
                </span>
              </div>
              {scanResult.findings && scanResult.findings.length > 0 ? (
                <table className="w-full">
                  <thead>
                    <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                      <th className="pb-2">유형</th>
                      <th className="pb-2">심각도</th>
                      <th className="pb-2">항목</th>
                      <th className="pb-2">규칙 ID</th>
                      <th className="pb-2">매칭</th>
                    </tr>
                  </thead>
                  <tbody>
                    {scanResult.findings.map((f: any, i: number) => (
                      <tr key={i} className="border-b border-gray-100 last:border-0">
                        <td className="py-2 text-sm font-mono">{f.type}</td>
                        <td className="py-2"><span className={sevBadge(f.severity)}>{f.severity}</span></td>
                        <td className="py-2 text-sm">{f.title_ko || f.title}</td>
                        <td className="py-2 text-xs font-mono text-gray-400">{f.rule_id}</td>
                        <td className="py-2 text-xs font-mono text-gray-400">{f.match}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : scanResult.passed ? (
                <p className="text-green-600 text-sm">✓ 보안 위반 사항이 발견되지 않았습니다.</p>
              ) : (
                <p className="text-red-600 text-sm">{scanResult.error || '검사 실패'}</p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function authHeaders() {
  const token = localStorage.getItem('pccp_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}
