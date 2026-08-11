import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../hooks/useAuth'

export default function Login() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const { login } = useAuth()
  const navigate = useNavigate()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const { token } = await api.login(email, password)
      login(token)
      navigate('/')
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

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
              <input
                type="password"
                className="input bg-gray-700 border-gray-600 text-white"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <button type="submit" disabled={loading} className="btn-primary w-full">
              {loading ? '로그인 중...' : '로그인'}
            </button>
          </form>
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
