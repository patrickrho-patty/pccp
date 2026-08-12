import { useState, useEffect } from 'react'
import { api } from '../api'

export default function Security() {
  const [text, setText] = useState('')
  const [result, setResult] = useState<any>(null)
  const [loading, setLoading] = useState(false)

  const check = async () => {
    if (!text) return
    setLoading(true)
    try {
      const r = await api.securityCheck(text)
      setResult(r)
    } catch (e: any) {
      setResult({ error: e.message })
    }
    setLoading(false)
  }

  const sevBadge = (s: string) => {
    const m: Record<string,string> = { critical: 'badge-red', high: 'badge-red', medium: 'badge-yellow', low: 'badge-blue' }
    return m[s] || 'badge-gray'
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">보안 검사 <span className="text-gray-400 text-lg font-normal">Security Check</span></h1>

      <div className="card mb-6">
        <label className="label">검사할 텍스트 (Text to scan)</label>
        <textarea
          className="input font-mono text-sm"
          rows={5}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="주민번호, 사업자번호, API 키 등이 포함된 텍스트를 입력하세요"
        />
        <button onClick={check} disabled={loading || !text} className="btn-primary mt-3">
          {loading ? '검사 중...' : '보안 검사 실행'}
        </button>
      </div>

      {result && (
        <div className="card">
          <div className="flex items-center gap-3 mb-4">
            <h2 className="text-lg font-semibold">결과</h2>
            <span className={result.passed ? 'badge-green' : result.verdict === 'DENY' ? 'badge-red' : 'badge-yellow'}>
              {result.verdict}
            </span>
          </div>

          {result.findings && result.findings.length > 0 ? (
            <table className="w-full">
              <thead>
                <tr className="border-b border-gray-200 text-left text-sm text-gray-500">
                  <th className="pb-2">유형</th>
                  <th className="pb-2">심각도</th>
                  <th className="pb-2">이름</th>
                  <th className="pb-2">규칙 ID</th>
                </tr>
              </thead>
              <tbody>
                {result.findings.map((f: any, i: number) => (
                  <tr key={i} className="border-b border-gray-100 last:border-0">
                    <td className="py-2 text-sm font-mono">{f.type}</td>
                    <td className="py-2"><span className={sevBadge(f.severity)}>{f.severity}</span></td>
                    <td className="py-2 text-sm">{f.title_ko || f.title}</td>
                    <td className="py-2 text-xs font-mono text-gray-400">{f.rule_id}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : result.passed ? (
            <p className="text-green-600 text-sm">보안 위반 사항이 발견되지 않았습니다.</p>
          ) : (
            <p className="text-red-600 text-sm">{result.error || '검사 실패'}</p>
          )}
        </div>
      )}
    </div>
  )
}
