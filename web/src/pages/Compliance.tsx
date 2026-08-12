import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'

// Korean compliance frameworks per PRD §41
// Each has real control requirements mapped to PCCP features

type ControlStatus = 'compliant' | 'partial' | 'gap' | 'not_assessed'

type Control = {
  id: string
  category: string
  categoryEn: string
  requirement: string
  requirementEn: string
  status: ControlStatus
  evidence?: string
  prdRef?: string
}

type Framework = {
  id: string
  name: string
  nameEn: string
  tier: string
  tierOptions: { value: string; label: string; desc: string }[]
  selectedTier: string
  authority: string
  icon: string
  desc: string
  applicableTo: string[] // enterprise, government, saas
  controls: Control[]
}

const FRAMEWORKS: Framework[] = [
  {
    id: 'csap',
    name: 'CSAP (클라우드보안인증)',
    nameEn: 'Cloud Security Assurance Program',
    tier: '간편 / 일반',
    tierOptions: [
      { value: 'simple', label: '간편 (SaaS)', desc: 'SaaS 서비스용 간소화 인증 · 39개 통제항목' },
      { value: 'standard', label: '일반 (IaaS/PaaS)', desc: '인프라 서비스용 표준 인증 · 101개 통제항목' },
    ],
    selectedTier: 'simple',
    authority: 'KISA / 과기정통부',
    icon: '☁️',
    desc: '클라우드 서비스 제공자를 위한 보안 인증. 공공기관 클라우드 이용 시 필수',
    applicableTo: ['saas', 'government'],
    controls: [
      { id: 'CSAP-1.1', category: '식별 및 인증', categoryEn: 'Identification & Authentication', requirement: '고유 식별 정보를 통한 사용자 인증', requirementEn: 'Unique ID-based user authentication', status: 'compliant', evidence: 'OAuth/OIDC + PAPER peer credential', prdRef: '§8.2' },
      { id: 'CSAP-1.2', category: '식별 및 인증', categoryEn: 'Identification & Authentication', requirement: '다중 요소 인증 (MFA)', requirementEn: 'Multi-factor authentication', status: 'partial', evidence: 'WebAuthn 지원, SMS/TOTP 미구현', prdRef: '§8.4' },
      { id: 'CSAP-1.3', category: '식별 및 인증', categoryEn: 'Identification & Authentication', requirement: '비밀번호 복잡도 및 주기적 변경', requirementEn: 'Password complexity and rotation', status: 'compliant', evidence: '인증정책 정책 팩으로 관리', prdRef: '§41' },
      { id: 'CSAP-2.1', category: '접근 통제', categoryEn: 'Access Control', requirement: '역할 기반 접근 통제 (RBAC)', requirementEn: 'Role-based access control', status: 'compliant', evidence: '조직/부서/프로젝트 계층 RBAC', prdRef: '§12.3, §13' },
      { id: 'CSAP-2.2', category: '접근 통제', categoryEn: 'Access Control', requirement: '최소 권한 원칙', requirementEn: 'Least privilege principle', status: 'compliant', evidence: '위임 관리 + 예외 승인 워크플로', prdRef: '§12.3' },
      { id: 'CSAP-3.1', category: '암호화', categoryEn: 'Encryption', requirement: '전송 구간 암호화', requirementEn: 'Data in transit encryption', status: 'compliant', evidence: 'PAPER TLS 1.3 + COSE-Sign1', prdRef: '§36' },
      { id: 'CSAP-3.2', category: '암호화', categoryEn: 'Encryption', requirement: '저장 데이터 암호화', requirementEn: 'Data at rest encryption', status: 'partial', evidence: 'DB 암호화 인프라 필요', prdRef: '§36' },
      { id: 'CSAP-4.1', category: '감사 및 로깅', categoryEn: 'Audit & Logging', requirement: '보안 감사 로그 기록 및 보존', requirementEn: 'Security audit log retention', status: 'compliant', evidence: '감사 이벤트 + 증거 번들', prdRef: '§40' },
      { id: 'CSAP-4.2', category: '감사 및 로깅', categoryEn: 'Audit & Logging', requirement: '로그 변조 방지', requirementEn: 'Log tamper protection', status: 'compliant', evidence: '서명된 이벤트 체인', prdRef: '§39.6' },
      { id: 'CSAP-5.1', category: '취약점 관리', categoryEn: 'Vulnerability Management', requirement: '정기적 보안 취약점 점검', requirementEn: 'Regular vulnerability assessment', status: 'compliant', evidence: '의존성 스캔 + 취약점 정책', prdRef: '§15.3' },
      { id: 'CSAP-6.1', category: '개인정보 보호', categoryEn: 'Privacy Protection', requirement: '개인정보 수집·이용 동의', requirementEn: 'PII collection consent', status: 'gap', evidence: '계정 포털 동의 기능 미구현', prdRef: '§16, §27' },
      { id: 'CSAP-6.2', category: '개인정보 보호', categoryEn: 'Privacy Protection', requirement: '개인정보 마스킹 및 삭제', requirementEn: 'PII masking and deletion', status: 'compliant', evidence: 'DLP 규칙 (주민번호/사업자번호)', prdRef: '§16.3' },
    ]
  },
  {
    id: 'isms-p',
    name: 'ISMS-P (정보보안관리체계)',
    nameEn: 'Information Security Management System-P',
    tier: '인증 등급',
    tierOptions: [
      { value: 'level1', label: '1등급 (최고)', desc: '52개 통제항목 모두 충족' },
      { value: 'level2', label: '2등급', desc: '기본 등급, 주요 통제항목 충족' },
      { value: 'level3', label: '3등급', desc: '최소 등급' },
    ],
    selectedTier: 'level2',
    authority: 'KISA / 과기정통부',
    icon: '🛡',
    desc: '정보보호 관리체계 인증. 조직의 정보보안 수준을 객관적으로 평가',
    applicableTo: ['enterprise', 'government'],
    controls: [
      { id: 'ISMS-1.1', category: '보안관리체계', categoryEn: 'Security Governance', requirement: '정보보호 조직 구성', requirementEn: 'Security organization', status: 'compliant', evidence: '위임 관리자 역할 체계', prdRef: '§12.3' },
      { id: 'ISMS-1.2', category: '보안관리체계', categoryEn: 'Security Governance', requirement: '정보보호 정책 수립', requirementEn: 'Security policy establishment', status: 'compliant', evidence: '정책 팩 + ABAC', prdRef: '§13, §41' },
      { id: 'ISMS-2.1', category: '자산 관리', categoryEn: 'Asset Management', requirement: '정보자산 식별 및 분류', requirementEn: 'Information asset classification', status: 'partial', evidence: '분류 시스템 부분 구현', prdRef: '§27' },
      { id: 'ISMS-2.2', category: '자산 관리', categoryEn: 'Asset Management', requirement: '데이터 보존 및 폐기', requirementEn: 'Data retention and disposal', status: 'compliant', evidence: '보존 클래스 + 법적 보존', prdRef: '§40.4-5' },
      { id: 'ISMS-3.1', category: '접근 통제', categoryEn: 'Access Control', requirement: '사용자 계정 관리', requirementEn: 'User account management', status: 'compliant', evidence: '사용자 생명주기 관리', prdRef: '§8' },
      { id: 'ISMS-4.1', category: '암호화', categoryEn: 'Encryption', requirement: '암호화 알고리즘 표준 준수', requirementEn: 'Standard crypto algorithms', status: 'compliant', evidence: 'KCMVP 인증 알고리즘 지원', prdRef: '§36' },
      { id: 'ISMS-5.1', category: '물리적 보안', categoryEn: 'Physical Security', requirement: '물리적 접근 통제', requirementEn: 'Physical access control', status: 'not_assessed', evidence: '데이터센터 물리보안은 인프라 담당', prdRef: '-' },
      { id: 'ISMS-6.1', category: '운영 보안', categoryEn: 'Operations Security', requirement: '악성코드 방지', requirementEn: 'Malware protection', status: 'compliant', evidence: '공급망 스캔 + 도구 승인', prdRef: '§15.3, §17' },
      { id: 'ISMS-7.1', category: '통신 보안', categoryEn: 'Communications Security', requirement: '네트워크 분리 및 보호', requirementEn: 'Network segregation', status: 'partial', evidence: '네트워크 브로커 + 구역 정책', prdRef: '§17.4' },
      { id: 'ISMS-8.1', category: '시스템 획득·개발·유지보수', categoryEn: 'System Development', requirement: '보안 코딩 가이드라인', requirementEn: 'Secure coding guidelines', status: 'compliant', evidence: '코딩 표준 팩 + 자동 검사', prdRef: '§33.11' },
      { id: 'ISMS-9.1', category: '공급자 관계', categoryEn: 'Supplier Relationships', requirement: '공급망 보안 관리', requirementEn: 'Supply chain security', status: 'compliant', evidence: '의존성 정책 + 라이선스 관리', prdRef: '§15.3' },
      { id: 'ISMS-10.1', category: '사고 관리', categoryEn: 'Incident Management', requirement: '보안 사고 대응 절차', requirementEn: 'Security incident response', status: 'compliant', evidence: '인시던트 대응 + 격리 모드', prdRef: '§15.4' },
    ]
  },
  {
    id: 'privacy',
    name: '개인정보보호법',
    nameEn: 'Personal Information Protection Act (PIPA)',
    tier: '적용 범위',
    tierOptions: [
      { value: 'sensitive', label: '민감정보 처리', desc: '민감정보(건강, 생체 등) 처리 시 강화된 통제' },
      { value: 'standard', label: '일반 개인정보', desc: '일반 개인정보 처리 기준' },
    ],
    selectedTier: 'standard',
    authority: '개인정보보호위원회',
    icon: '🔐',
    desc: '개인정보의 수집, 이용, 제공, 파기 전 과정 보호. 한국 SaaS 필수 준수',
    applicableTo: ['enterprise', 'government', 'saas'],
    controls: [
      { id: 'PIPA-1.1', category: '수집 및 이용', categoryEn: 'Collection & Use', requirement: '개인정보 수집 시 동의', requirementEn: 'Consent for PII collection', status: 'gap', evidence: '동의 관리 UI 필요', prdRef: '§16' },
      { id: 'PIPA-1.2', category: '수집 및 이용', categoryEn: 'Collection & Use', requirement: '수집 목적 명시', requirementEn: 'Purpose specification', status: 'partial', evidence: '부분 구현', prdRef: '§16' },
      { id: 'PIPA-2.1', category: '안전성 확보', categoryEn: 'Security Measures', requirement: '개인정보 암호화 저장', requirementEn: 'Encrypted PII storage', status: 'partial', evidence: 'DB 암호화 인프라 필요', prdRef: '§36' },
      { id: 'PIPA-2.2', category: '안전성 확보', categoryEn: 'Security Measures', requirement: '한국 PII 자동 감지 및 마스킹', requirementEn: 'Korean PII auto-detection', status: 'compliant', evidence: '주민번호/사업자번호/전화번호 DLP', prdRef: '§16.3' },
      { id: 'PIPA-3.1', category: '파기', categoryEn: 'Disposal', requirement: '보유 기간 경과 시 파기', requirementEn: 'Retention expiry disposal', status: 'compliant', evidence: '보존 클래스 + 자동 만료', prdRef: '§40.4' },
      { id: 'PIPA-4.1', category: '처리 정지', categoryEn: 'Processing Rights', requirement: '개인정보 처리 정지 요구', requirementEn: 'Right to stop processing', status: 'gap', evidence: '사용자 권리 관리 UI 필요', prdRef: '§27' },
      { id: 'PIPA-5.1', category: '영향평가', categoryEn: 'Impact Assessment', requirement: '개인정보 영향평가', requirementEn: 'Privacy Impact Assessment (PIA)', status: 'gap', evidence: 'PIA 도구 미구현', prdRef: '§27' },
    ]
  },
  {
    id: 'kisa-secure',
    name: 'KISA 안전한 소프트웨어 개발',
    nameEn: 'KISA Secure Software Development Guide',
    tier: '적용',
    tierOptions: [
      { value: 'applied', label: '적용', desc: '소프트웨어 개발보안 가이드 준수' },
    ],
    selectedTier: 'applied',
    authority: 'KISA / 행정안전부',
    icon: '💻',
    desc: '안전한 소프트웨어 개발을 위한 가이드라인. 공공기관 SW 개발 시 의무',
    applicableTo: ['enterprise', 'government'],
    controls: [
      { id: 'KISA-1.1', category: '입력 데이터 검증', categoryEn: 'Input Validation', requirement: 'SQL 인젝션 방지', requirementEn: 'SQL injection prevention', status: 'compliant', evidence: 'ORM 매개변수화 쿼리', prdRef: '§15.3' },
      { id: 'KISA-1.2', category: '입력 데이터 검증', categoryEn: 'Input Validation', requirement: '프롬프트 인젝션 방지', requirementEn: 'Prompt injection prevention', status: 'compliant', evidence: '인젝션 탐지 규칙 + 컨텍스트 방화벽', prdRef: '§16.4' },
      { id: 'KISA-2.1', category: '보안 기능', categoryEn: 'Security Features', requirement: '인증 및 세션 관리', requirementEn: 'Auth and session management', status: 'compliant', evidence: 'JWT + PAPER 피어 인증', prdRef: '§8' },
      { id: 'KISA-3.1', category: '에러 처리', categoryEn: 'Error Handling', requirement: '예외 처리 및 에러 메시지 관리', requirementEn: 'Exception and error message handling', status: 'compliant', evidence: '구조화된 에러 응답', prdRef: '-' },
      { id: 'KISA-4.1', category: '취약한 암호화', categoryEn: 'Cryptography', requirement: '안전한 난수 생성 및 키 관리', requirementEn: 'Secure RNG and key management', status: 'compliant', evidence: 'KMS/키 브로커', prdRef: '§36' },
      { id: 'KISA-5.1', category: '취약점 관리', categoryEn: 'Vulnerability Management', requirement: '정기적 코드 보안 점검', requirementEn: 'Regular code security review', status: 'compliant', evidence: 'AI 코드 보안 분석 + 감사', prdRef: '§15, §19' },
    ]
  },
  {
    id: 'ai-gov',
    name: 'AI 가이던스 및 거버넌스',
    nameEn: 'AI Governance',
    tier: '적용',
    tierOptions: [
      { value: 'kr', label: '한국 AI 기본법', desc: 'AI 시스템 개발·운영 투명성 및 안전성 확보' },
      { value: 'iso42001', label: 'ISO/IEC 42001', desc: 'AI 관리체계 국제표준' },
    ],
    selectedTier: 'kr',
    authority: '과기정통부 / ISO',
    icon: '🤖',
    desc: 'AI 시스템의 투명성, 안전성, 책임성 확보를 위한 가이던스',
    applicableTo: ['enterprise', 'government'],
    controls: [
      { id: 'AI-1.1', category: '투명성', categoryEn: 'Transparency', requirement: 'AI 사용 사실 명시', requirementEn: 'AI usage disclosure', status: 'compliant', evidence: 'AI/인간 코드 기여 라벨링', prdRef: '§19' },
      { id: 'AI-1.2', category: '투명성', categoryEn: 'Transparency', requirement: 'AI 의사결정 과정 기록', requirementEn: 'AI decision logging', status: 'compliant', evidence: '세션 타임라인 + 프로바이던스', prdRef: '§14.3' },
      { id: 'AI-2.1', category: '안전성', categoryEn: 'Safety', requirement: 'AI 출력 검증 및 통제', requirementEn: 'AI output validation', status: 'compliant', evidence: 'DLP + 도구 승인 + 보안 스캔', prdRef: '§15, §17' },
      { id: 'AI-2.2', category: '안전성', categoryEn: 'Safety', requirement: 'AI 모델 변경 관리', requirementEn: 'AI model change management', status: 'compliant', evidence: '카탈로그 에포크 + 모델 리콜', prdRef: '§10A' },
      { id: 'AI-3.1', category: '책임성', categoryEn: 'Accountability', requirement: 'AI 사용에 대한 감사 가능', requirementEn: 'AI usage auditability', status: 'compliant', evidence: '감사 로그 + 증거 번들', prdRef: '§40' },
      { id: 'AI-3.2', category: '책임성', categoryEn: 'Accountability', requirement: 'AI 코드 변경에 대한 인간 검토', requirementEn: 'Human review of AI code changes', status: 'compliant', evidence: 'AI/인간 프로바이던스 + 승인', prdRef: '§19' },
    ]
  },
]

const statusConfig: Record<ControlStatus, { label: string; badge: string; color: string; icon: string }> = {
  compliant: { label: '준수', badge: 'badge-green', color: 'text-green-600', icon: '✅' },
  partial: { label: '부분 준수', badge: 'badge-yellow', color: 'text-yellow-600', icon: '⚠️' },
  gap: { label: '미준수', badge: 'badge-red', color: 'text-red-600', icon: '❌' },
  not_assessed: { label: '미평가', badge: 'badge-gray', color: 'text-gray-400', icon: '⬜' },
}

export default function Compliance() {
  const [frameworks, setFrameworks] = useState<Framework[]>(FRAMEWORKS)
  const [selectedFramework, setSelectedFramework] = useState<string>(FRAMEWORKS[0].id)
  const [tab, setTab] = useState<'overview' | 'detail' | 'remediation'>('overview')

  const fw = frameworks.find(f => f.id === selectedFramework)!

  const allControls = frameworks.flatMap(f => f.controls)
  const summary = {
    total: allControls.length,
    compliant: allControls.filter(c => c.status === 'compliant').length,
    partial: allControls.filter(c => c.status === 'partial').length,
    gaps: allControls.filter(c => c.status === 'gap').length,
    notAssessed: allControls.filter(c => c.status === 'not_assessed').length,
  }
  const complianceScore = summary.total > 0 ? Math.round((summary.compliant / summary.total) * 100) : 0

  const fwControls = fw.controls
  const fwScore = fwControls.length > 0 ? Math.round((fwControls.filter(c => c.status === 'compliant').length / fwControls.length) * 100) : 0

  const setTier = (fwId: string, tier: string) => {
    setFrameworks(fws => fws.map(f => f.id === fwId ? { ...f, selectedTier: tier } : f))
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">컴플라이언스 <span className="text-gray-400 text-lg font-normal">Compliance & Certifications</span></h1>
      <p className="text-xs text-gray-400 mb-6">한국 규제 준수 현황 · PRD §41 Korean Governance and Compliance Packs</p>

      {/* Overall compliance score */}
      <div className="grid grid-cols-6 gap-3 mb-6">
        <div className="card py-3 px-4 text-center col-span-1">
          <div className={`text-4xl font-bold ${complianceScore >= 80 ? 'text-green-600' : complianceScore >= 50 ? 'text-yellow-600' : 'text-red-600'}`}>{complianceScore}%</div>
          <div className="text-xs text-gray-500 mt-1">전체 준수율</div>
        </div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-green-600">{summary.compliant}</div><div className="text-xs text-gray-500">준수</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-yellow-600">{summary.partial}</div><div className="text-xs text-gray-500">부분 준수</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-red-600">{summary.gaps}</div><div className="text-xs text-gray-500">미준수</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-gray-400">{summary.notAssessed}</div><div className="text-xs text-gray-500">미평가</div></div>
        <div className="card py-3 px-4 text-center"><div className="text-2xl font-bold text-blue-600">{frameworks.length}</div><div className="text-xs text-gray-500">프레임워크</div></div>
      </div>

      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {[
          { id: 'overview', label: '프레임워크 현황', en: 'Overview' },
          { id: 'detail', label: '통제 항목 상세', en: 'Controls Detail' },
          { id: 'remediation', label: '해결 계획', en: 'Remediation' },
        ].map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.id ? 'border-patty-600 text-patty-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
            {t.label} <span className="text-xs text-gray-400">{t.en}</span>
          </button>
        ))}
      </div>

      {/* OVERVIEW */}
      {tab === 'overview' && (
        <div>
          {/* Framework selector */}
          <div className="grid grid-cols-5 gap-3 mb-6">
            {frameworks.map(f => {
              const score = f.controls.length > 0 ? Math.round(f.controls.filter(c => c.status === 'compliant').length / f.controls.length * 100) : 0
              return (
                <div key={f.id} className={`card cursor-pointer border-l-4 transition-all ${selectedFramework === f.id ? 'border-l-blue-500 ring-2 ring-blue-100' : 'border-l-gray-300 hover:border-l-blue-300'}`}
                  onClick={() => { setSelectedFramework(f.id); setTab('detail') }}>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-2xl">{f.icon}</span>
                    <span className={`text-lg font-bold ${score >= 80 ? 'text-green-600' : score >= 50 ? 'text-yellow-600' : 'text-red-600'}`}>{score}%</span>
                  </div>
                  <h4 className="text-xs font-semibold leading-tight">{f.name}</h4>
                  <p className="text-[10px] text-gray-400 mt-0.5">{f.nameEn}</p>
                  <p className="text-[10px] text-gray-500 mt-1">{f.authority}</p>
                </div>
              )
            })}
          </div>

          {/* Selected framework detail */}
          <div className="card">
            <div className="flex items-start justify-between mb-4">
              <div className="flex items-center gap-3">
                <span className="text-3xl">{fw.icon}</span>
                <div>
                  <h3 className="text-sm font-semibold">{fw.name}</h3>
                  <p className="text-xs text-gray-400">{fw.nameEn}</p>
                  <p className="text-xs text-gray-500 mt-1">{fw.desc}</p>
                </div>
              </div>
              <div className="text-right">
                <div className={`text-3xl font-bold ${fwScore >= 80 ? 'text-green-600' : fwScore >= 50 ? 'text-yellow-600' : 'text-red-600'}`}>{fwScore}%</div>
                <div className="text-xs text-gray-500">{fwControls.filter(c => c.status === 'compliant').length}/{fwControls.length} 준수</div>
              </div>
            </div>

            {/* Tier selector */}
            <div className="bg-gray-50 rounded-lg p-3 mb-4">
              <label className="text-xs font-medium text-gray-600 block mb-2">인증 등급 · Certification Tier</label>
              <div className="flex gap-2">
                {fw.tierOptions.map(opt => (
                  <button key={opt.value} onClick={() => setTier(fw.id, opt.value)}
                    className={`px-3 py-2 rounded-lg text-left text-sm border transition-all ${fw.selectedTier === opt.value ? 'border-blue-400 bg-blue-50' : 'border-gray-200 bg-white hover:bg-gray-50'}`}>
                    <div className="font-medium">{opt.label}</div>
                    <div className="text-[10px] text-gray-400">{opt.desc}</div>
                  </button>
                ))}
              </div>
            </div>

            {/* Category breakdown */}
            <div className="grid grid-cols-4 gap-3 mb-4">
              {(['compliant', 'partial', 'gap', 'not_assessed'] as ControlStatus[]).map(status => {
                const count = fwControls.filter(c => c.status === status).length
                return (
                  <div key={status} className={`rounded-lg p-3 text-center ${
                    status === 'compliant' ? 'bg-green-50' : status === 'partial' ? 'bg-yellow-50' : status === 'gap' ? 'bg-red-50' : 'bg-gray-100'
                  }`}>
                    <div className={`text-2xl font-bold ${statusConfig[status].color}`}>{count}</div>
                    <div className="text-xs text-gray-500">{statusConfig[status].label}</div>
                  </div>
                )
              })}
            </div>

            {/* Controls summary by category */}
            <div className="space-y-2">
              {Object.entries(fwControls.reduce((acc, c) => {
                if (!acc[c.category]) acc[c.category] = []
                acc[c.category].push(c)
                return acc
              }, {} as Record<string, Control[]>)).map(([cat, controls]) => (
                <div key={cat} className="flex items-center justify-between py-2 border-b border-gray-50">
                  <div>
                    <span className="text-sm font-medium">{cat}</span>
                    <span className="text-xs text-gray-400 ml-2">{controls[0].categoryEn}</span>
                  </div>
                  <div className="flex gap-1">
                    {controls.map(c => (
                      <span key={c.id} className={`w-6 h-6 rounded text-[10px] flex items-center justify-center ${
                        c.status === 'compliant' ? 'bg-green-100 text-green-700' :
                        c.status === 'partial' ? 'bg-yellow-100 text-yellow-700' :
                        c.status === 'gap' ? 'bg-red-100 text-red-700' : 'bg-gray-100 text-gray-400'
                      }`} title={`${c.id}: ${c.requirement}`}>
                        {statusConfig[c.status].icon}
                      </span>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* DETAIL — full controls table for selected framework */}
      {tab === 'detail' && (
        <div>
          {/* Framework tabs */}
          <div className="flex gap-2 mb-4 flex-wrap">
            {frameworks.map(f => (
              <button key={f.id} onClick={() => setSelectedFramework(f.id)}
                className={`px-3 py-1.5 text-sm rounded-lg border transition-all ${selectedFramework === f.id ? 'border-blue-400 bg-blue-50 text-blue-700' : 'border-gray-200 hover:bg-gray-50'}`}>
                {f.icon} {f.name}
              </button>
            ))}
          </div>

          <div className="card">
            <div className="flex items-center justify-between mb-3">
              <div>
                <h3 className="text-sm font-semibold">{fw.icon} {fw.name}</h3>
                <p className="text-xs text-gray-400">{fw.authority} · {fw.tierOptions.find(t => t.value === fw.selectedTier)?.label}</p>
              </div>
              <span className="badge-gray">{fwControls.length}개 통제항목</span>
            </div>

            <table className="w-full">
              <thead><tr className="border-b border-gray-200 text-left text-xs text-gray-500 uppercase tracking-wide">
                <th className="pb-3">통제 ID</th>
                <th className="pb-3">분류</th>
                <th className="pb-3">요구사항</th>
                <th className="pb-3">상태</th>
                <th className="pb-3">증거</th>
                <th className="pb-3">PRD</th>
              </tr></thead>
              <tbody>
                {fwControls.map(c => (
                  <tr key={c.id} className="border-b border-gray-100 last:border-0">
                    <td className="py-3 font-mono text-xs">{c.id}</td>
                    <td className="py-3 text-xs">
                      <div className="font-medium">{c.category}</div>
                      <div className="text-gray-400">{c.categoryEn}</div>
                    </td>
                    <td className="py-3 text-sm">
                      <div>{c.requirement}</div>
                      <div className="text-xs text-gray-400">{c.requirementEn}</div>
                    </td>
                    <td className="py-3">
                      <span className={statusConfig[c.status].badge}>{statusConfig[c.status].icon} {statusConfig[c.status].label}</span>
                    </td>
                    <td className="py-3 text-xs text-gray-500 max-w-xs">{c.evidence || '-'}</td>
                    <td className="py-3 text-xs">
                      {c.prdRef && c.prdRef !== '-' ? (
                        <Link to="/audit" className="text-blue-600 hover:underline">{c.prdRef}</Link>
                      ) : '-'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* REMEDIATION — gaps and what to do about them */}
      {tab === 'remediation' && (
        <div className="space-y-4">
          {frameworks.map(f => {
            const gaps = f.controls.filter(c => c.status === 'gap' || c.status === 'partial')
            if (gaps.length === 0) return null
            return (
              <div key={f.id} className="card">
                <div className="flex items-center gap-2 mb-3">
                  <span className="text-xl">{f.icon}</span>
                  <h3 className="text-sm font-semibold">{f.name} <span className="text-gray-400 font-normal">{f.nameEn}</span></h3>
                  <span className="badge-red ml-auto">{gaps.length}개 해결 필요</span>
                </div>
                <div className="space-y-2">
                  {gaps.map(c => (
                    <div key={c.id} className={`p-3 rounded-lg ${c.status === 'gap' ? 'bg-red-50' : 'bg-yellow-50'}`}>
                      <div className="flex items-start justify-between">
                        <div className="flex-1">
                          <div className="flex items-center gap-2 mb-1">
                            <span className="font-mono text-xs text-gray-500">{c.id}</span>
                            <span className={statusConfig[c.status].badge}>{statusConfig[c.status].label}</span>
                          </div>
                          <div className="text-sm font-medium">{c.requirement}</div>
                          <div className="text-xs text-gray-400">{c.requirementEn}</div>
                          <div className="text-xs text-gray-500 mt-1">현재 상태: {c.evidence}</div>
                          {c.prdRef && c.prdRef !== '-' && (
                            <Link to="/audit" className="text-xs text-blue-600 hover:underline mt-1 inline-block">관련 규격: {c.prdRef} →</Link>
                          )}
                        </div>
                        <button onClick={() => {
                          if (confirm('해결 계획을 입력하세요')) {}
                        }} className="btn-sm btn-secondary ml-4 whitespace-nowrap">해결 계획</button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )
          })}
          {frameworks.every(f => f.controls.filter(c => c.status === 'gap' || c.status === 'partial').length === 0) && (
            <div className="card text-center py-12">
              <div className="text-4xl mb-3">✅</div>
              <p className="text-gray-500">모든 통제항목이 준수됩니다.</p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
