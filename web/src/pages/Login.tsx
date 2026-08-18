import { useState, useEffect } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../hooks/useAuth'

export default function Login() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [ssoMode, setSsoMode] = useState<'none' | 'oidc' | 'saml'>('none')
  const [showPassword, setShowPassword] = useState(false)
  const [oidcOrganization, setOidcOrganization] = useState('')
  const [samlOrganization, setSamlOrganization] = useState('')
  const [showMfa, setShowMfa] = useState(false)
  const [mfaCode, setMfaCode] = useState('')
  const [ssoError, setSsoError] = useState('')
  const [ssoLoading, setSsoLoading] = useState(false)
  const [ssoNotice, setSsoNotice] = useState('')
  const { login } = useAuth()
  const navigate = useNavigate()

  // Both IdPs complete against bounded backend callbacks, which redirect here
  // with the same one-time browser-bound handoff contract.
  useEffect(() => {
	const params = new URLSearchParams(window.location.search)
	const ssoHandoff = params.get('sso_handoff')
	const ssoProvider = params.get('sso_provider')

    const cleanup = () => window.history.replaceState({}, '', '/login')

	if (ssoHandoff && (ssoProvider === 'oidc' || ssoProvider === 'saml')) {
	  api.ssoSessionExchange(ssoHandoff, ssoProvider)
		.then((resp: any) => {
		  if (resp?.token) {
			login(resp.token)
		  } else {
			setSsoError('SSO 로그인에 실패했습니다 (세션 토큰 미발급)')
		  }
		})
		.catch(err => setSsoError('SSO 콜백 실패: ' + err.message))
		.finally(cleanup)
	}
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const { token, mfa } = await api.login(email, password, mfaCode)
      login(token)
      // Deep-link return-to (UX4): ?next= resumes the intended page.
      const next = new URLSearchParams(window.location.search).get('next') || '/'
      navigate(next.startsWith('/') ? next : '/')
      void mfa
    } catch (err: any) {
      const msg = String(err.message || err)
      if (msg.includes('MFA') || msg.includes('mfa')) setShowMfa(true)
      setError(msg)
    } finally {
      setLoading(false)
    }
  }

  const startOIDC = async (e: React.FormEvent) => {
    e.preventDefault()
    setSsoError('')
    setSsoLoading(true)
	try {
	  const { auth_url } = await api.ssoOIDCAuthUrl(oidcOrganization)
	  window.location.assign(auth_url)
	} catch (err: any) {
	  setSsoError('OIDC 시작 실패: ' + err.message)
      setSsoLoading(false)
    }
  }

  const startSAML = async (e: React.FormEvent) => {
    e.preventDefault()
    setSsoError('')
    setSsoLoading(true)
    try {
      const { redirect_url } = await api.ssoSAMLRedirect(samlOrganization)
      window.location.assign(redirect_url)
    } catch (err: any) {
      setSsoError('SAML 시작 실패: ' + err.message)
      setSsoLoading(false)
    }
  }

  const ssoInput = 'input bg-gray-700 border-gray-600 text-white text-sm'

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-900">
      <div className="w-full max-w-md">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-white">Patty Code</h1>
          <p className="text-gray-400 mt-2">Control Plane</p>
        </div>
        <div className="card bg-gray-800 border-gray-700">
          <h2 className="text-xl font-semibold text-white mb-6">로그인</h2>
          {error && <div className="bg-red-900/50 text-red-200 px-4 py-2 rounded-lg mb-4 text-sm">{error}</div>}
          {ssoNotice && <div className="bg-blue-900/50 text-blue-200 px-4 py-2 rounded-lg mb-4 text-xs">{ssoNotice}</div>}
          {ssoError && <div className="bg-red-900/50 text-red-200 px-4 py-2 rounded-lg mb-4 text-xs">{ssoError}</div>}
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="label text-gray-300">이메일</label>
              <input
                type="email"
                className="input bg-gray-700 border-gray-600 text-white"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="admin@patty.dev"
                required
              />
            </div>
            <div>
              <label className="label text-gray-300">비밀번호</label>
              <div className="relative">
                <input
                  type={showPassword ? 'text' : 'password'}
                  className="input bg-gray-700 border-gray-600 text-white pr-16"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute inset-y-0 right-2 text-xs text-gray-400 hover:text-gray-200"
                >
                  {showPassword ? '숨기기' : '표시'}
                </button>
              </div>
            </div>
            {showMfa && (
              <div>
                <label className="label text-gray-300">MFA 코드 (TOTP)</label>
                <input
                  className="input bg-gray-700 border-gray-600 text-white font-mono tracking-widest"
                  value={mfaCode}
                  onChange={(e) => setMfaCode(e.target.value)}
                  placeholder="6자리 코드"
                  maxLength={6}
                />
              </div>
            )}
            <button type="submit" disabled={loading} className="btn-primary w-full">
              {loading ? '로그인 중...' : showMfa ? 'MFA 확인' : '로그인'}
            </button>
          </form>

          {/* MFA — honest placeholder (spec 25 §B, §8.9) */}
          <button
            disabled
            className="w-full mt-3 px-4 py-2 rounded-lg border border-gray-600 text-gray-500 text-sm cursor-not-allowed"
            title="admin_credentials.mfa_enrolled 개념만 존재 — 두 번째 인자 미구현"
          >
            MFA (2단계 인증) <span className="badge-gray text-[10px] ml-1">준비 중 · §8.9</span>
          </button>

          {/* SSO (§8.2) — wired to the real /api/sso/* routes */}
          <div className="flex items-center gap-3 my-5">
            <div className="flex-1 border-t border-gray-700" />
            <span className="text-xs text-gray-500">또는 SSO로 로그인</span>
            <div className="flex-1 border-t border-gray-700" />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <button
              onClick={() => { setSsoMode(ssoMode === 'saml' ? 'none' : 'saml'); setSsoError('') }}
              className={`px-3 py-2 rounded-lg border text-sm transition-colors ${ssoMode === 'saml' ? 'border-patty-600 text-patty-300 bg-patty-600/10' : 'border-gray-600 text-gray-300 hover:bg-gray-700'}`}
            >
              SAML SSO
            </button>
            <button
              onClick={() => { setSsoMode(ssoMode === 'oidc' ? 'none' : 'oidc'); setSsoError('') }}
              className={`px-3 py-2 rounded-lg border text-sm transition-colors ${ssoMode === 'oidc' ? 'border-patty-600 text-patty-300 bg-patty-600/10' : 'border-gray-600 text-gray-300 hover:bg-gray-700'}`}
            >
              OIDC SSO
            </button>
          </div>

          {ssoMode !== 'none' && (
            <div className="mt-4 p-3 rounded-lg bg-gray-900/50 border border-gray-700">
              {ssoMode === 'oidc' ? (
                <form onSubmit={startOIDC} className="space-y-3">
                  <div>
                    <label className="label text-gray-400 text-xs">조직 ID 또는 슬러그</label>
                    <input className={ssoInput} value={oidcOrganization} onChange={e => setOidcOrganization(e.target.value)} placeholder="patty" required />
                  </div>
                  <button type="submit" disabled={ssoLoading} className="btn-primary w-full text-sm">
                    {ssoLoading ? '이동 중...' : 'IdP로 이동 · Continue with OIDC'}
                  </button>
                </form>
              ) : (
                <form onSubmit={startSAML} className="space-y-3">
                  <div>
                    <label className="label text-gray-400 text-xs">조직 ID 또는 슬러그</label>
                    <input className={ssoInput} value={samlOrganization} onChange={e => setSamlOrganization(e.target.value)} placeholder="patty" required />
                  </div>
                  <button type="submit" disabled={ssoLoading} className="btn-primary w-full text-sm">
                    {ssoLoading ? '이동 중...' : 'IdP로 이동 · Continue with SAML'}
                  </button>
                </form>
              )}
              <p className="text-[10px] text-gray-500 mt-3 leading-relaxed">
                조직의 관리자가 등록한 IdP 설정으로만 연결됩니다. 로그인 요청에서 IdP 주소나 클라이언트 정보를 변경할 수 없습니다.
              </p>
            </div>
          )}

          <div className="mt-4 text-center">
            <Link to="/bootstrap" className="text-sm text-patty-400 hover:text-patty-300">
              초기 설정 (Bootstrap)
            </Link>
          </div>
        </div>
      </div>
    </div>
  )
}
