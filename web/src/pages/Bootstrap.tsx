import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'

export default function Bootstrap() {
  const [email, setEmail] = useState('admin@patty.dev')
  const [password, setPassword] = useState('')
  const [orgName, setOrgName] = useState('Patty Enterprise')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const navigate = useNavigate()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      await api.bootstrap(email, password, orgName)
      setSuccess(true)
      setTimeout(() => navigate('/'), 2000)
    } catch (err: any) {
      setError(err.message)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-900">
      <div className="w-full max-w-md">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-white">Patty Code</h1>
          <p className="text-gray-400 mt-2">초기 설정</p>
        </div>
        <div className="card bg-gray-800 border-gray-700">
          <h2 className="text-xl font-semibold text-white mb-6">관리자 계정 생성</h2>
          {error && <div className="bg-red-900/50 text-red-200 px-4 py-2 rounded-lg mb-4 text-sm">{error}</div>}
          {success && <div className="bg-green-900/50 text-green-200 px-4 py-2 rounded-lg mb-4 text-sm">초기 설정 완료! 로그인 페이지로 이동합니다.</div>}
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="label text-gray-300">조직명</label>
              <input className="input bg-gray-700 border-gray-600 text-white" value={orgName} onChange={(e) => setOrgName(e.target.value)} required />
            </div>
            <div>
              <label className="label text-gray-300">관리자 이메일</label>
              <input type="email" className="input bg-gray-700 border-gray-600 text-white" value={email} onChange={(e) => setEmail(e.target.value)} required />
            </div>
            <div>
              <label className="label text-gray-300">비밀번호</label>
              <input type="password" className="input bg-gray-700 border-gray-600 text-white" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </div>
            <button type="submit" className="btn-primary w-full">초기 설정 실행</button>
          </form>
        </div>
      </div>
    </div>
  )
}
