import { useState, useEffect } from 'react'
import { api } from '../api'

// PRD §15.3 Alert Catalog — categorized by threat family
const THREAT_CATEGORIES = [
  {
    id: 'injection',
    name: '프롬프트 인젝션 · 탈옥',
    nameEn: 'Prompt Injection & Jailbreak',
    icon: '🧪',
    desc: '모델/도구 제어 우회 시도, 명령어 재정의, 시스템 프롬프트 노출',
    prdRef: '§10C.12',
    severity: 'critical',
    rules: [
      { id: 'inj-override', name: '명령어 재정의', nameEn: 'Instruction Override', pattern: 'ignore.*previous.*instructions', action: 'block' },
      { id: 'inj-jailbreak', name: '탈옥 시도', nameEn: 'Jailbreak (DAN)', pattern: '(jailbreak|DAN|developer.mode|system.prompt)', action: 'block' },
      { id: 'inj-bypass', name: '도구 제어 우회', nameEn: 'Tool Control Bypass', pattern: '(bypass.*filter|ignore.*safety|disable.*guard)', action: 'block' },
      { id: 'inj-credential-extract', name: '시스템 자격증명 노출 시도', nameEn: 'Credential Extraction', pattern: '(show.*system.*prompt|reveal.*api.*key|print.*env)', action: 'block' },
      { id: 'inj-indirect', name: '간접 인젝션 (저장소 컨텐츠)', nameEn: 'Indirect Injection (Repo)', pattern: '<!--.*ignore.*-->|{{.*system.*}}', action: 'review' },
    ]
  },
  {
    id: 'extraction',
    name: '모델 남용 · 추출',
    nameEn: 'Model Abuse & Extraction',
    icon: '🤖',
    desc: '모델 지식 추출, 구독 API 재판매, 프로토콜 남용',
    prdRef: '§35.2',
    severity: 'critical',
    rules: [
      { id: 'extract-volume', name: '대량 자동 질의 (추출 시도)', nameEn: 'High-Volume Automated Probing', pattern: '>100 req/min from single account', action: 'throttle' },
      { id: 'extract-resale', name: '구독 API 재판매', nameEn: 'Subscription Resale', pattern: 'API-like usage pattern, no human latency', action: 'block' },
      { id: 'extract-catalog', name: '카탈로그 스푸핑', nameEn: 'Catalog Spoofing', pattern: 'inject model/base_url locally', action: 'block' },
      { id: 'extract-protocol', name: '프로토콜 남용 (DoS)', nameEn: 'Protocol Abuse/DoS', pattern: 'malformed PAPER frames, flood', action: 'block' },
    ]
  },
  {
    id: 'identity',
    name: '계정 · 신원 남용',
    nameEn: 'Account & Identity Abuse',
    icon: '👤',
    desc: '계정 공유, 자격증명 도용, 하네스 복제, 결제 사기',
    prdRef: '§10C.9-10, §35.2',
    severity: 'high',
    rules: [
      { id: 'acct-share', name: '계정 공유', nameEn: 'Account Sharing', pattern: 'concurrent distant harnesses, unusual geo', action: 'stepup' },
      { id: 'acct-clone', name: '하네스 복제', nameEn: 'Harness Cloning', pattern: 'same peer key from multiple devices', action: 'block' },
      { id: 'acct-replay', name: '자격증명 재생 공격', nameEn: 'Credential Replay', pattern: 'cryptographic replay detected', action: 'block' },
      { id: 'acct-payment', name: '결제/자격 사기', nameEn: 'Payment/Entitlement Fraud', pattern: 'plan state manipulation', action: 'block' },
      { id: 'acct-rapid', name: '빠른 등록/해지 사이클', nameEn: 'Rapid Enroll/Revoke', pattern: '>5 enroll/revoke in 1h', action: 'review' },
    ]
  },
  {
    id: 'exfil',
    name: '데이터 유출',
    nameEn: 'Data Exfiltration',
    icon: '📤',
    desc: '비밀정보 모델 노출, PII 유출, 비정상 파일 전송',
    prdRef: '§15.3',
    severity: 'critical',
    rules: [
      { id: 'exfil-rrn', name: '주민등록번호', nameEn: 'Korean RRN', pattern: '\\d{6}-\\d{7}', action: 'block' },
      { id: 'exfil-business', name: '사업자등록번호', nameEn: 'Business Registration', pattern: '\\d{3}-\\d{2}-\\d{5}', action: 'mask' },
      { id: 'exfil-aws', name: 'AWS 접근키', nameEn: 'AWS Access Key', pattern: 'AKIA[A-Z0-9]{16}', action: 'block' },
      { id: 'exfil-private-key', name: '개인키', nameEn: 'Private Key', pattern: '-----BEGIN.*PRIVATE KEY', action: 'block' },
      { id: 'exfil-jwt', name: 'JWT 토큰', nameEn: 'JWT Token', pattern: 'eyJ[a-zA-Z0-9_-]+', action: 'block' },
      { id: 'exfil-github', name: 'GitHub PAT', nameEn: 'GitHub Token', pattern: 'gh[pousr]_[A-Za-z0-9]{36}', action: 'block' },
      { id: 'exfil-file', name: '비정상 대용량 파일 전송', nameEn: 'Abnormal File Transfer', pattern: '>100MB outbound transfer', action: 'review' },
    ]
  },
  {
    id: 'supplychain',
    name: '공급망 · 코드 보안',
    nameEn: 'Supply Chain & Code Security',
    icon: '📦',
    desc: '취약한 의존성, 금지 라이선스, 승인되지 않은 패키지',
    prdRef: '§15.3',
    severity: 'high',
    rules: [
      { id: 'sc-vuln', name: '취약한 의존성 (임계치 초과)', nameEn: 'Vulnerable Dependency', pattern: 'CVSS > policy threshold', action: 'block' },
      { id: 'sc-license', name: '금지 라이선스', nameEn: 'Prohibited License', pattern: 'GPL/AGPL in proprietary repo', action: 'block' },
      { id: 'sc-crypto', name: '금지 암호화 알고리즘', nameEn: 'Prohibited Crypto', pattern: 'MD5, SHA1, DES, RC4', action: 'block' },
      { id: 'sc-mcp', name: '승인되지 않은 MCP 서버', nameEn: 'Unapproved MCP Server', pattern: 'MCP not in org allowlist', action: 'block' },
      { id: 'sc-package', name: '공급망 패키지 위험', nameEn: 'Supply Chain Package Risk', pattern: 'typosquat/known-malicious pkg', action: 'block' },
    ]
  },
  {
    id: 'infra',
    name: '인프라 공격',
    nameEn: 'Infrastructure Attacks',
    icon: '🏗️',
    desc: '샌드박스 탈출, 엔드포인트 우회, 모델 증명 손실',
    prdRef: '§15.3, §35.2',
    severity: 'critical',
    rules: [
      { id: 'infra-sandbox', name: '샌드박스 탈출 지표', nameEn: 'Sandbox Escape', pattern: 'unexpected process tree, priv escalation', action: 'block' },
      { id: 'infra-endpoint', name: '엔드포인트 우회 (직접 vLLM)', nameEn: 'Endpoint Bypass', pattern: 'raw vLLM direct access', action: 'block' },
      { id: 'infra-attest', name: '모델 엔드포인트 증명 손실', nameEn: 'Endpoint Attestation Loss', pattern: 'PIA attestation expired/invalid', action: 'block' },
      { id: 'infra-mismatch', name: '모델 아티팩트 불일치', nameEn: 'Model Artifact Mismatch', pattern: 'PMP digest mismatch', action: 'block' },
      { id: 'infra-downgrade', name: '프로토콜 다운그레이드', nameEn: 'Protocol Downgrade', pattern: 'PAPER→HTTP fallback attempt', action: 'block' },
    ]
  },
  {
    id: 'evasion',
    name: '정책 우회',
    nameEn: 'Policy Evasion',
    icon: '🕵️',
    desc: '보안 정책 회피, 난독화, 보호 브랜치 쓰기, 감사 회피',
    prdRef: '§15.3, §10C.10',
    severity: 'high',
    rules: [
      { id: 'evade-obfuscation', name: '난독화/인코딩 우회', nameEn: 'Obfuscation/Encoding', pattern: 'base64/hex encoded payloads to bypass DLP', action: 'block' },
      { id: 'evade-protected', name: '보호 브랜치 쓰기 시도', nameEn: 'Protected Branch Write', pattern: 'direct push to main/release/prod', action: 'block' },
      { id: 'evade-tamper', name: '정책/설정 변조', nameEn: 'Policy/Config Tampering', pattern: 'unauthorized config modification', action: 'block' },
      { id: 'evade-audit', name: '감사 파이프라인 중단', nameEn: 'Audit Pipeline Interruption', pattern: 'provenance gap, event spine down', action: 'alert' },
      { id: 'evade-provenance', name: '프로바이던스 위조', nameEn: 'Provenance Forgery', pattern: 'faked AI/human attribution', action: 'block' },
    ]
  },
  {
    id: 'capacity',
    name: '용량 남용',
    nameEn: 'Capacity Abuse',
    icon: '⚡',
    desc: 'GPU 고갈, 과도한 토큰 사용, 동시 하네스 한도 우회',
    prdRef: '§10C.7-8, §35.2',
    severity: 'medium',
    rules: [
      { id: 'cap-gpu', name: 'GPU 고갈 (다중 무거운 에이전트)', nameEn: 'GPU Starvation', pattern: 'many heavy-context agents from one account', action: 'throttle' },
      { id: 'cap-token', name: '과도한 토큰/컨텍스트 사용', nameEn: 'Excessive Token/Context Use', pattern: '>90% capacity sustained', action: 'throttle' },
      { id: 'cap-quota', name: '동시 하네스 할당량 우회', nameEn: 'Concurrent Harness Quota Bypass', pattern: 'work slot count exceeds lease', action: 'block' },
    ]
  },
]

type Finding = {
  id: string; finding_type: string; severity: string; title: string;
  title_ko?: string; status: string; occurred_at: string; session_id?: string;
}

export default function Security() {
  const [tab, setTab] = useState<'dashboard' | 'threats' | 'findings' | 'scanner'>('dashboard')
  const [scanText, setScanText] = useState('')
  const [scanResult, setScanResult] = useState<any>(null)
  const [findings, setFindings] = useState<Finding[]>([])
  const [stats, setStats] = useState({ critical: 0, high: 0, medium: 0, open: 0, total: 0 })

  useEffect(() => {
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
    fetch('/api/security/findings', { headers: authHeaders() })
      .then(r => r.json())
      .then(data => setFindings(Array.isArray(data) ? data : []))
      .catch(() => {})
  }, [])

  const runScan = async () => {
    if (!scanText) return
    try {
      const r = await api.securityCheck(scanText)
      setScanResult(r)
    } catch (e: any) {
      setScanResult({ error: e.message })
    }
  }

  const sevBadge = (s: string) => s === 'critical' ? 'badge-red' : s === 'high' ? 'badge-red' : s === 'medium' ? 'badge-yellow' : 'badge-blue'
  const sevText = (s: string) => s === 'critical' ? '치명적' : s === 'high' ? '높음' : s === 'medium' ? '중간' : '낮음'
  const statusBadge = (s: string) => s === 'open' ? 'badge-red' : s === 'investigating' ? 'badge-yellow' : s === 'resolved' ? 'badge-green' : 'badge-gray'
  const actionBadge = (a: string) => {
    const m: Record<string,string> = { block: 'badge-red', mask: 'badge-yellow', throttle: 'badge-blue', review: 'badge-yellow', alert: 'badge-gray', stepup: 'badge-blue' }
    return m[a] || 'badge-gray'
  }
  const actionText = (a: string) => {
    const m: Record<string,string> = { block: '차단', mask: '마스킹', throttle: '제한', review: '검토', alert: '알림', stepup: '재인증' }
    return m[a] || a
  }

  const postureScore = stats.total === 0 ? 100 : Math.max(0, 100 - stats.critical * 25 - stats.high * 10 - stats.open * 5)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">보안 운영 센터 <span className="text-gray-400 text-lg font-normal">Security Operations Center</span></h1>
      <p className="text-xs text-gray-400 mb-6">AI 코딩 위협 탐지 및 대응 · Threat detection per PRD §15, §35</p>

      {/* Tab navigation */}
      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'dashboard', label: '보안 현황', labelEn: 'Dashboard' },
          { id: 'threats', label: '위협 카탈로그', labelEn: 'Threat Catalog' },
          { id: 'findings', label: '보안 발견', labelEn: 'Findings' },
          { id: 'scanner', label: '보안 검사', labelEn: 'Scanner' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} <span className="text-xs text-gray-400">{t.labelEn}</span>
          </button>
        ))}
      </div>

      {/* Dashboard Tab */}
      {tab === 'dashboard' && (
        <div>
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="card col-span-1">
              <div className="text-center">
                <div className={`text-5xl font-bold ${postureScore >= 80 ? 'text-green-600' : postureScore >= 50 ? 'text-yellow-600' : 'text-red-600'}`}>{postureScore}</div>
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

          {/* Threat categories overview */}
          <div className="grid grid-cols-2 gap-4 mb-6">
            {THREAT_CATEGORIES.map(cat => (
              <div key={cat.id} className="card">
                <div className="flex items-start gap-3 mb-2">
                  <span className="text-2xl">{cat.icon}</span>
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <h4 className="text-sm font-semibold">{cat.name}</h4>
                      <span className={sevBadge(cat.severity)}>{sevText(cat.severity)}</span>
                    </div>
                    <p className="text-xs text-gray-400">{cat.nameEn} <span className="ml-1">({cat.prdRef})</span></p>
                  </div>
                  <span className="badge-gray">{cat.rules.length} 규칙</span>
                </div>
                <p className="text-xs text-gray-500">{cat.desc}</p>
              </div>
            ))}
          </div>

          {/* Incident Response */}
          <div className="card">
            <h3 className="text-sm font-semibold mb-3">인시던트 대응 · Incident Response (§15.4)</h3>
            <div className="grid grid-cols-3 gap-3">
              <div className="bg-gray-50 rounded-lg p-3">
                <div className="text-sm font-medium mb-1">세션 격리 · Session</div>
                <p className="text-xs text-gray-500">세션 일시정지, 모델/도구 중지, 샌드박스 보존</p>
              </div>
              <div className="bg-gray-50 rounded-lg p-3">
                <div className="text-sm font-medium mb-1">하네스 격리 · Harness</div>
                <p className="text-xs text-gray-500">인증서 회수, 통신 차단, 재등록 필요</p>
              </div>
              <div className="bg-red-50 rounded-lg p-3">
                <div className="text-sm font-medium mb-1 text-red-700">조직 잠금 · Lockdown</div>
                <p className="text-xs text-gray-500">전체 에이전트 중지, 클라우드 모델 차단</p>
              </div>
            </div>
            <button className="btn-danger w-full text-sm mt-3" onClick={async () => {
              if (!confirm('정말로 전체 조직을 잠금하시겠습니까? 모든 AI 세션이 중지됩니다.')) return
              try {
                const res = await fetch('/api/security/lockdown', { method: 'POST', headers: authHeaders() })
                if (res.ok) alert('긴급 잠금이 활성화되었습니다.')
              } catch { alert('잠금 실패') }
            }}>⚠ 긴급 조직 잠금 · Emergency Lockdown</button>
          </div>
        </div>
      )}

      {/* Threats Tab — full catalog */}
      {tab === 'threats' && (
        <div>
          <p className="text-xs text-gray-400 mb-4">PRD §15.3 초기 알림 카탈로그 + §35 위협 모델 기반 · 8개 위협 카테고리, {THREAT_CATEGORIES.reduce((a, c) => a + c.rules.length, 0)}개 탐지 규칙</p>
          {THREAT_CATEGORIES.map(cat => (
            <div key={cat.id} className="card mb-4">
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-3">
                  <span className="text-2xl">{cat.icon}</span>
                  <div>
                    <h4 className="text-sm font-semibold">{cat.name}</h4>
                    <p className="text-xs text-gray-400">{cat.nameEn} · {cat.prdRef} · {cat.desc}</p>
                  </div>
                </div>
                <span className={sevBadge(cat.severity)}>{sevText(cat.severity)}</span>
              </div>
              <table className="w-full">
                <thead>
                  <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                    <th className="pb-2">위협</th>
                    <th className="pb-2">탐지 패턴</th>
                    <th className="pb-2">조치</th>
                  </tr>
                </thead>
                <tbody>
                  {cat.rules.map(r => (
                    <tr key={r.id} className="border-b border-gray-50 last:border-0">
                      <td className="py-2">
                        <div className="text-sm font-medium">{r.name}</div>
                        <div className="text-xs text-gray-400">{r.nameEn}</div>
                      </td>
                      <td className="py-2"><code className="text-xs bg-gray-100 px-1.5 py-0.5 rounded">{r.pattern}</code></td>
                      <td className="py-2"><span className={actionBadge(r.action)}>{actionText(r.action)}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </div>
      )}

      {/* Findings Tab */}
      {tab === 'findings' && (
        <div className="card">
          <div className="flex justify-between items-center mb-4">
            <h3 className="text-lg font-semibold">보안 발견 목록 · Security Findings</h3>
            <div className="flex gap-3 items-center">
              <span className="text-sm text-gray-500">{findings.length}건</span>
              {findings.length > 0 && (
                <button onClick={() => {
                  const csv = ['timestamp,type,severity,title,status,session_id']
                  findings.forEach(f => { csv.push([f.occurred_at, f.finding_type, f.severity, (f.title_ko || f.title || '').replace(/,/g, ';'), f.status, f.session_id || ''].join(',')) })
                  const blob = new Blob([csv.join('\n')], { type: 'text/csv' })
                  const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = 'security_findings.csv'; a.click()
                }} className="btn-sm btn-secondary">CSV 내보내기</button>
              )}
            </div>
          </div>
          {findings.length === 0 ? (
            <div className="text-center py-12">
              <div className="text-4xl mb-3">✅</div>
              <p className="text-gray-500">활성 보안 발견이 없습니다.</p>
            </div>
          ) : (
            <table className="w-full">
              <thead>
                <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                  <th className="pb-3">유형</th><th className="pb-3">심각도</th><th className="pb-3">제목</th>
                  <th className="pb-3">상태</th><th className="pb-3">시간</th>
                </tr>
              </thead>
              <tbody>
                {findings.map(f => (
                  <tr key={f.id} className="border-b border-gray-100 last:border-0">
                    <td className="py-3 text-sm font-mono">{f.finding_type}</td>
                    <td className="py-3"><span className={sevBadge(f.severity)}>{f.severity}</span></td>
                    <td className="py-3 text-sm">{f.title_ko || f.title}</td>
                    <td className="py-3"><span className={statusBadge(f.status)}>{f.status}</span></td>
                    <td className="py-3 text-xs text-gray-400">{f.occurred_at?.slice(0, 19)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Scanner Tab */}
      {tab === 'scanner' && (
        <div className="card">
          <h3 className="text-lg font-semibold mb-3">보안 검사 도구 · Security Scanner</h3>
          <p className="text-sm text-gray-500 mb-4">텍스트를 입력하여 DLP/시크릿/인젝션 탐지 규칙을 테스트합니다.</p>
          <textarea className="input font-mono text-sm mb-3" rows={4}
            value={scanText} onChange={e => setScanText(e.target.value)}
            placeholder="주민번호: 901225-1234567, AWS_KEY=AKIAABCDEFGHIJKLMNOP, ignore previous instructions..." />
          <button onClick={runScan} disabled={!scanText} className="btn-primary text-sm">보안 검사 실행 · Scan</button>
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
                  <thead><tr className="border-b border-gray-200 text-left text-sm text-gray-500">
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
              ) : scanResult.passed ? <p className="text-green-600 text-sm">✓ 위반 사항 없음</p>
                : <p className="text-red-600 text-sm">{scanResult.error || '검사 실패'}</p>}
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
