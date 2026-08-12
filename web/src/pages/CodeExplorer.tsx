import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'

export default function CodeExplorer() {
  const [repos, setRepos] = useState<any[]>([])
  const [selectedRepo, setSelectedRepo] = useState<string | null>(null)
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  const [selectedSpan, setSelectedSpan] = useState<any>(null)
  const [provenanceSpans, setProvenanceSpans] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])

  useEffect(() => {
    fetch('/api/repositories', { headers: authHeaders() })
      .then(r => r.json()).then(data => setRepos(Array.isArray(data) ? data : [])).catch(() => {})
    fetch('/api/sessions', { headers: authHeaders() })
      .then(r => r.json()).then(data => setSessions(Array.isArray(data) ? data : [])).catch(() => {})
    fetch('/api/users', { headers: authHeaders() })
      .then(r => r.json()).then(data => setUsers(Array.isArray(data) ? data : [])).catch(() => {})
  }, [])

  const loadProvenance = (repoId: string) => {
    // Load provenance spans for this repo
    fetch(`/api/provenance/lookup?repo_id=${repoId}`, { headers: authHeaders() })
      .then(r => r.json()).then(data => {
        setProvenanceSpans(data?.spans || [])
      }).catch(() => setProvenanceSpans([]))
  }

  // Mock code with annotations for demo
  const sampleCode = [
    { line: 1, text: 'package main', attribution: 'human', user: '김개발', age: '3일 전', confidence: null },
    { line: 2, text: '', attribution: null },
    { line: 3, text: 'import (', attribution: 'human', user: '김개발', age: '3일 전', confidence: null },
    { line: 4, text: '\t"fmt"', attribution: 'human', user: '김개발', age: '3일 전', confidence: null },
    { line: 5, text: ')', attribution: 'human', user: '김개발', age: '3일 전', confidence: null },
    { line: 6, text: '', attribution: null },
    { line: 7, text: '// RefundProcessor processes payment refunds', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 8, text: 'type RefundProcessor struct {', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 9, text: '\tamount  int64', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 10, text: '\tcurrency string', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 11, text: '}', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 12, text: '', attribution: null },
    { line: 13, text: 'func (r *RefundProcessor) Process() error {', attribution: 'ai_then_human', user: '김개발', age: '10분 전', confidence: 0.88, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 14, text: '\tif r.amount <= 0 {', attribution: 'ai_then_human', user: '김개발', age: '10분 전', confidence: 0.88, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 15, text: '\t\treturn fmt.Errorf("invalid refund amount: %d", r.amount)', attribution: 'human', user: '김개발', age: '5분 전', confidence: null },
    { line: 16, text: '\t}', attribution: 'ai_then_human', user: '김개발', age: '10분 전', confidence: 0.88, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 17, text: '\t// TODO: 실제 환불 처리 로직 구현', attribution: 'human', user: '김개발', age: '방금 전', confidence: null },
    { line: 18, text: '\tfmt.Printf("Processing refund: %d %s\\n", r.amount, r.currency)', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 19, text: '\treturn nil', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
    { line: 20, text: '}', attribution: 'ai', user: '김개발', age: '방금 전', confidence: 0.95, model: 'Qwen3 MoE', session: '환불 로직 구현' },
  ]

  const attributionColors: Record<string, string> = {
    'ai': 'bg-green-50 border-l-2 border-green-400',
    'human': 'bg-blue-50 border-l-2 border-blue-400',
    'ai_then_human': 'bg-yellow-50 border-l-2 border-yellow-400',
  }

  const attributionLabels: Record<string, string> = {
    'ai': '🟢 AI 생성',
    'human': '🔵 인간 수정',
    'ai_then_human': '🟡 AI→인간 수정',
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">코드 프로바이던스 익스플로러 <span className="text-gray-400 text-lg font-normal">Code Provenance Explorer</span></h1>

      <div className="flex gap-4">
        {/* Left: File Browser */}
        <div className="w-64 card overflow-y-auto main-scroll" style={{ maxHeight: '70vh' }}>
          <h3 className="text-sm font-semibold mb-3">저장소 · Repositories</h3>
          {repos.length === 0 ? (
            <p className="text-xs text-gray-400">저장소가 없습니다</p>
          ) : repos.map(r => (
            <div key={r.id}>
              <div onClick={() => { setSelectedRepo(r.id); setSelectedFile(r.name + '/src/main.go'); loadProvenance(r.id) }}
                className={`cursor-pointer p-2 rounded text-sm hover:bg-gray-50 ${selectedRepo === r.id ? 'bg-blue-50 text-blue-700' : ''}`}>
                📁 {r.name}
              </div>
              {selectedRepo === r.id && (
                <div className="ml-4 mt-1 space-y-1">
                  <div onClick={() => setSelectedFile(r.name + '/src/main.go')}
                    className={`cursor-pointer p-1.5 rounded text-xs hover:bg-gray-50 ${selectedFile?.includes('main') ? 'bg-blue-50' : ''}`}>
                    📄 src/main.go
                  </div>
                  <div className="cursor-pointer p-1.5 rounded text-xs text-gray-400">
                    📄 src/handler.go
                  </div>
                  <div className="cursor-pointer p-1.5 rounded text-xs text-gray-400">
                    📄 src/model.go
                  </div>
                  <div className="cursor-pointer p-1.5 rounded text-xs text-gray-400">
                    📄 tests/main_test.go
                  </div>
                </div>
              )}
            </div>
          ))}

          {selectedFile && (
            <div className="mt-4 pt-3 border-t border-gray-100">
              <div className="flex items-center gap-2 text-xs text-gray-500 mb-2">
                <span>범례 · Legend:</span>
              </div>
              <div className="space-y-1">
                {Object.entries(attributionLabels).map(([key, label]) => (
                  <div key={key} className={`text-xs px-2 py-1 rounded ${attributionColors[key] || ''}`}>
                    {label}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Center: Code View */}
        <div className="flex-1 card overflow-hidden">
          {selectedFile ? (
            <>
              <div className="flex items-center justify-between px-4 py-2 border-b border-gray-200 bg-gray-50">
                <span className="text-sm font-mono">{selectedFile}</span>
                <span className="text-xs text-gray-400">{sampleCode.filter(l => l.attribution === 'ai').length} AI / {sampleCode.filter(l => l.attribution === 'human').length} Human / {sampleCode.filter(l => l.attribution === 'ai_then_human').length} Mixed</span>
              </div>
              <div className="overflow-auto" style={{ maxHeight: '60vh' }}>
                <table className="w-full text-xs font-mono">
                  <tbody>
                    {sampleCode.map(line => (
                      <tr key={line.line}
                        className={`cursor-pointer ${selectedSpan?.line === line.line ? 'ring-2 ring-blue-300' : ''} ${line.attribution ? attributionColors[line.attribution] : ''}`}
                        onClick={() => line.attribution ? setSelectedSpan(line) : setSelectedSpan(null)}>
                        <td className="text-right text-gray-400 px-2 py-0.5 select-none w-10 border-r border-gray-100">{line.line}</td>
                        <td className="px-3 py-0.5 whitespace-pre">
                          {line.text || ' '}
                          {line.attribution && (
                            <span className="ml-2 inline-flex items-center gap-1 text-[9px] text-gray-500 float-right">
                              {line.user} · {line.age}
                              {line.model && ` · ${line.model}`}
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          ) : (
            <div className="flex items-center justify-center h-full text-gray-400 text-sm py-12">
              왼쪽에서 저장소와 파일을 선택하세요
            </div>
          )}
        </div>

        {/* Right: Detail Panel */}
        {selectedSpan && (
          <div className="w-80 card overflow-y-auto main-scroll" style={{ maxHeight: '70vh' }}>
            <h3 className="text-sm font-semibold mb-3">코드 출처 상세 · Provenance Detail</h3>
            <div className="space-y-3 text-xs">
              <div className={`p-2 rounded ${attributionColors[selectedSpan.attribution] || ''}`}>
                <span className="font-medium">{attributionLabels[selectedSpan.attribution]}</span>
              </div>
              <div>
                <span className="text-gray-500">수정자:</span>{' '}
                <Link to="/users" className="text-blue-600 hover:underline">{selectedSpan.user}</Link>
              </div>
              <div>
                <span className="text-gray-500">수정 시간:</span> {selectedSpan.age}
              </div>
              {selectedSpan.model && (
                <div>
                  <span className="text-gray-500">AI 모델:</span>{' '}
                  <span className="badge-blue">{selectedSpan.model}</span>
                </div>
              )}
              {selectedSpan.session && (
                <div>
                  <span className="text-gray-500">세션:</span>{' '}
                  <Link to="/sessions" className="text-blue-600 hover:underline">{selectedSpan.session}</Link>
                </div>
              )}
              {selectedSpan.confidence && (
                <div>
                  <span className="text-gray-500">신뢰도:</span> {(selectedSpan.confidence * 100).toFixed(0)}%
                </div>
              )}

              <div className="pt-3 border-t border-gray-100">
                <div className="font-medium mb-2">하네스 리플레이 · Harness Replay</div>
                <div className="bg-black text-green-400 text-[10px] font-mono p-2 rounded">
                  <div className="text-gray-500">{'>'} 프롬프트:</div>
                  <div className="text-gray-300 ml-2">payment-service의 환불 처리 로직을 Go로 작성해주세요</div>
                  <div className="text-gray-500 mt-1">{'>'} AI 응답:</div>
                  <div className="text-green-400 ml-2">RefundProcessor 구조체를 생성했습니다...</div>
                  <div className="text-gray-500 mt-1">{'>'} 도구 호출:</div>
                  <div className="text-blue-400 ml-2">file_write: src/main.go (라인 7-20)</div>
                </div>
              </div>

              <div className="pt-3 border-t border-gray-100">
                <div className="font-medium mb-2">관련 정보 · Related</div>
                <div className="space-y-1">
                  <Link to="/sessions" className="block text-blue-600 hover:underline">→ 세션 보기</Link>
                  <Link to="/harnesses" className="block text-blue-600 hover:underline">→ 하네스 보기</Link>
                  <Link to="/users" className="block text-blue-600 hover:underline">→ 사용자 보기</Link>
                  <Link to="/audit" className="block text-blue-600 hover:underline">→ 감사 로그 보기</Link>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function authHeaders() { const token = localStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }
