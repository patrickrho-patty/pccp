import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'

// 배포 프로파일 (§34) — console-side picker. NOTE: POST /api/auth/bootstrap은
// 현재 조직을 enterprise로 고정 생성함 (internal/api/server.go handleBootstrap).
// 선택한 프로파일은 요청 본문에 포함되어 전송됩니다(향후 API 반영 대비).
const PROFILES = [
  {
    id: 'enterprise',
    icon: '🏢',
    ko: '엔터프라이즈',
    en: 'Enterprise',
    desc: '사내/프라이빗 배포 — 좌석 기반 라이선스, 내부 정책 팩',
    descEn: 'On-prem / private deployment',
  },
  {
    id: 'public',
    icon: '☁️',
    ko: '퍼블릭 클라우드',
    en: 'Public Cloud',
    desc: '퍼블릭 구독자 플랜(free~enterprise), 용량 리스 기반',
    descEn: 'Public subscriber plans & capacity leases',
  },
  {
    id: 'sovereign',
    icon: '🛡️',
    ko: '주권 · 정부',
    en: 'Sovereign / Government',
    desc: '에어갭 트러스트 번들, 오프라인 업데이트, 데이터 주권',
    descEn: 'Air-gapped trust bundles & offline updates',
  },
]

export default function Bootstrap() {
  const [step, setStep] = useState(1)
  const [profile, setProfile] = useState('enterprise')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [orgName, setOrgName] = useState('')
  const [policyPack, setPolicyPack] = useState('')
  const [demoData, setDemoData] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const navigate = useNavigate()

  const pwValid = password.length >= 8
  const pwMatch = password !== '' && password === confirm
  const canSubmit = orgName.trim() !== '' && email.trim() !== '' && pwValid && pwMatch

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!canSubmit) return
    setError('')
    try {
      const res = await api.bootstrap(email, password, orgName, profile, policyPack, demoData)
      setSuccess(true)
      void res
      setTimeout(() => navigate('/'), 2000)
    } catch (err: any) {
      setError(err.message)
    }
  }

  const inputCls = 'input bg-gray-700 border-gray-600 text-white'

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-900">
      <div className="w-full max-w-lg">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-white">Patty Code</h1>
          <p className="text-gray-400 mt-2">초기 설정 · First-Run Setup</p>
        </div>

        {/* Step indicator */}
        <div className="flex items-center justify-center gap-2 mb-6 text-xs">
          <span className={step === 1 ? 'text-patty-300 font-semibold' : 'text-gray-500'}>1. 배포 프로파일</span>
          <span className="text-gray-600">→</span>
          <span className={step === 2 ? 'text-patty-300 font-semibold' : 'text-gray-500'}>2. 관리자 계정</span>
        </div>

        <div className="card bg-gray-800 border-gray-700">
          {step === 1 ? (
            <div>
              <h2 className="text-xl font-semibold text-white mb-1">배포 프로파일 선택 <span className="text-sm text-gray-400 font-normal">Deployment Profile (§34)</span></h2>
              <p className="text-xs text-gray-400 mb-4">프로파일은 기본 정책, 신뢰 소스, 플랜 구조를 결정합니다.</p>
              <div className="space-y-2">
                {PROFILES.map(p => (
                  <button
                    key={p.id}
                    onClick={() => setProfile(p.id)}
                    className={`w-full text-left p-3 rounded-lg border transition-colors ${profile === p.id ? 'border-patty-600 bg-patty-600/10' : 'border-gray-600 hover:bg-gray-700'}`}
                  >
                    <div className="flex items-center gap-2">
                      <span className="text-xl">{p.icon}</span>
                      <div>
                        <div className={`text-sm font-semibold ${profile === p.id ? 'text-patty-300' : 'text-white'}`}>{p.ko} <span className="text-xs text-gray-400 font-normal">{p.en}</span></div>
                        <div className="text-[11px] text-gray-400">{p.desc}</div>
                      </div>
                    </div>
                  </button>
                ))}
              </div>

              {/* Policy pack — honest placeholder (spec 26 §B, §41): no backend route at bootstrap */}
              <div className="mt-4 p-3 rounded-lg border border-gray-700 bg-gray-900/50">
                <div className="flex items-center justify-between">
                  <div>
                    <div className="text-sm text-gray-300">초기 정책 팩 · Policy Pack (§41)</div>
                    <div className="text-[11px] text-gray-500">컴플라이언스 프레임워크 기본 정책 세트 — 아직 부트스트랩에서 선택할 수 없습니다</div>
                  </div>
                  <span className="badge-gray text-[10px]">준비 중</span>
                </div>
              </div>

              <p className="text-[10px] text-gray-500 mt-3 leading-relaxed">
                ℹ️ 현재 부트스트랩 API는 조직을 <span className="font-mono">enterprise</span> 프로파일로 고정 생성합니다. 선택한 프로파일({profile})은 요청에 포함되어 전송되며 API 반영 시 즉시 적용됩니다.
              </p>

              <button onClick={() => setStep(2)} className="btn-primary w-full mt-4">다음 · Next</button>
            </div>
          ) : (
            <div>
              <h2 className="text-xl font-semibold text-white mb-1">관리자 계정 생성</h2>
              <p className="text-xs text-gray-400 mb-4">선택한 프로파일: <span className="text-patty-300 font-semibold">{PROFILES.find(p => p.id === profile)?.ko}</span></p>
              {error && <div className="bg-red-900/50 text-red-200 px-4 py-2 rounded-lg mb-4 text-sm">{error}</div>}
              {success && <div className="bg-green-900/50 text-green-200 px-4 py-2 rounded-lg mb-4 text-sm">초기 설정 완료! 로그인 페이지로 이동합니다.</div>}
              {!success && (
                <form onSubmit={handleSubmit} className="space-y-4">
                  <div>
                    <label className="label text-gray-300">조직명</label>
                    <input className={inputCls} value={orgName} onChange={(e) => setOrgName(e.target.value)} placeholder="Patty Enterprise" required />
                  </div>
                  <div>
                    <label className="label text-gray-300">관리자 이메일</label>
                    <input type="email" className={inputCls} value={email} onChange={(e) => setEmail(e.target.value)} placeholder="admin@company.com" required />
                  </div>
                  <div>
                    <label className="label text-gray-300">비밀번호</label>
                    <input type="password" className={inputCls} value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
                    {password !== '' && !pwValid && <p className="text-xs text-red-400 mt-1">8자 이상이어야 합니다.</p>}
                  </div>
                  <div>
                    <label className="label text-gray-300">비밀번호 확인</label>
                    <input type="password" className={inputCls} value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
                    {confirm !== '' && !pwMatch && <p className="text-xs text-red-400 mt-1">비밀번호가 일치하지 않습니다.</p>}
                  </div>
                  <div className="flex gap-2">
                    <button type="button" onClick={() => setStep(1)} className="btn-secondary flex-1">이전</button>
                    <div>
              <label className="label text-gray-300">컴플라이언스 프레임워크 (§41)</label>
              <select className={inputCls + ' w-full'} value={policyPack} onChange={(e) => setPolicyPack(e.target.value)}>
                <option value="">선택 안 함</option>
                <option value="CSAP">CSAP (클라우드 보안 인증)</option>
                <option value="ISMS-P">ISMS-P</option>
                <option value="KISA">KISA 가이드라인</option>
                <option value="AI-BASIC">인공지능 기본법</option>
              </select>
            </div>
            <label className="flex items-center gap-2 text-sm text-gray-300">
              <input type="checkbox" checked={demoData} onChange={(e) => setDemoData(e.target.checked)} className="w-4 h-4" />
              데모 데이터 생성 (명시적 선택 — 기본 꺼짐)
            </label>
                        <button type="submit" disabled={!canSubmit} className="btn-primary flex-1 disabled:opacity-50">초기 설정 실행</button>
                  </div>
                </form>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
