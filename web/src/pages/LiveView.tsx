import { useState, useEffect } from 'react'
import { api } from '../api'
import { Link } from 'react-router-dom'

export default function LiveView() {
  const [harnesses, setHarnesses] = useState<any[]>([])
  const [sessions, setSessions] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [terminalLines, setTerminalLines] = useState<Record<string, string[]>>({})

  const load = () => {
    api.listHarnesses().then(data => setHarnesses(Array.isArray(data) ? data : []))
    api.listSessions().then(data => setSessions(Array.isArray(data) ? data : []))
    api.listUsers().then(data => setUsers(Array.isArray(data) ? data : []))
  }

  useEffect(() => {
    load()
    // Simulate terminal output updates
    const interval = setInterval(() => {
      setTerminalLines(prev => {
        const next = { ...prev }
        harnesses.filter(h => h.status === 'enrolled' || h.status === 'active').forEach(h => {
          const lines = next[h.harness_id] || []
          const samples = [
            '> 코드 분석 중...',
            '> src/payment/refund.go 수정',
            '> AI: refund amount validation 추가',
            '> pytest tests/test_refund.py',
            '> 3 passed, 0 failed',
            '> git add -A && git commit',
            '> [feature/refund abc1234] Add refund logic',
            '> 하네스: 김개발님이 코딩 중...',
            '> model: Patty Code Standard',
            '> tokens: in=2048 out=512',
            '> context: 8 files loaded',
          ]
          const newLine = samples[Math.floor(Math.random() * samples.length)]
          next[h.harness_id] = [...lines.slice(-8), newLine]
        })
        return next
      })
    }, 2000)
    return () => clearInterval(interval)
  }, [harnesses.length])

  const getUserName = (harness: any) => {
    const session = sessions.find(s => s.harness_id === harness.harness_id && s.status === 'active')
    if (session) {
      const user = users.find(u => u.id === session.user_id)
      return user?.name_ko || user?.name || '?'
    }
    return '-'
  }

  const getSession = (harness: any) => {
    return sessions.find(s => s.harness_id === harness.harness_id && s.status === 'active')
  }

  const riskColor = (state: string) => {
    if (state === 'high') return 'bg-red-500'
    if (state === 'elevated') return 'bg-yellow-500'
    return 'bg-green-500'
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">실시간 하네스 <span className="text-gray-400 text-lg font-normal">Live Harness Wall</span></h1>
          <p className="text-sm text-gray-500 mt-1">모든 활성 하네스의 실시간 화면 · Real-time view of all active harnesses</p>
        </div>
        <div className="flex items-center gap-4 text-sm">
          <span className="text-gray-500">{harnesses.filter(h => h.status === 'enrolled' || h.status === 'active').length} 활성</span>
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse" /> LIVE
          </span>
        </div>
      </div>

      {harnesses.filter(h => h.status === 'enrolled' || h.status === 'active').length === 0 ? (
        <div className="card text-center py-12">
          <p className="text-gray-400 mb-2">활성 하네스가 없습니다</p>
          <p className="text-sm text-gray-400">하네스가 등록되고 세션이 시작되면 여기에 실시간으로 표시됩니다.</p>
        </div>
      ) : (
        <div className="grid grid-cols-3 gap-3">
          {harnesses.filter(h => h.status === 'enrolled' || h.status === 'active').map(h => {
            const session = getSession(h)
            const userName = getUserName(h)
            const isExpanded = expandedId === h.id

            return (
              <div key={h.id}
                className={`border rounded-lg overflow-hidden transition-all ${
                  isExpanded ? 'col-span-3' : ''
                } ${h.risk_state === 'high' ? 'border-red-300' : 'border-gray-200'}`}>

                {/* Card Header */}
                <div className="flex items-center justify-between px-3 py-2 bg-gray-900 text-white">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className={`w-2 h-2 rounded-full ${riskColor(h.risk_state)} flex-shrink-0 animate-pulse`} />
                    <span className="text-xs font-mono truncate">{h.harness_id}</span>
                  </div>
                  <div className="flex items-center gap-2 flex-shrink-0">
                    <span className="text-xs text-gray-400">{userName}</span>
                    <span className="text-xs text-gray-500">v{h.binary_version}</span>
                  </div>
                </div>

                {/* Session Info Bar */}
                {session && (
                  <div className="flex items-center gap-2 px-3 py-1 bg-gray-100 text-xs border-b border-gray-200">
                    <span className="font-medium truncate">{session.title || '제목 없음'}</span>
                    <span className="text-gray-400">·</span>
                    <span className="text-gray-500">{session.model_class}</span>
                    <span className="ml-auto badge-green text-xs">활성</span>
                  </div>
                )}

                {/* Terminal */}
                <div
                  className={`bg-black p-2 font-mono ${isExpanded ? 'h-[400px]' : 'h-[120px]'} overflow-y-auto`}
                  style={{ fontSize: isExpanded ? '12px' : '9px', lineHeight: isExpanded ? '1.5' : '1.3' }}
                  onClick={() => setExpandedId(isExpanded ? null : h.id)}>
                  {(terminalLines[h.harness_id] || [
                    '> 하네스 대기 중...',
                    '> 세션이 시작되면 출력이 표시됩니다',
                  ]).map((line, i) => (
                    <div key={i} className={
                      line.startsWith('>') ? 'text-gray-400' :
                      line.includes('AI:') ? 'text-green-400' :
                      line.includes('passed') ? 'text-green-400' :
                      line.includes('failed') || line.includes('error') ? 'text-red-400' :
                      'text-gray-300'
                    }>
                      {line}
                    </div>
                  ))}
                </div>

                {/* Footer Actions */}
                {isExpanded && (
                  <div className="px-3 py-2 bg-gray-50 border-t border-gray-200">
                    <div className="grid grid-cols-4 gap-4 text-xs">
                      <div>
                        <span className="text-gray-500">위험도:</span>{' '}
                        <span className={h.risk_state === 'normal' ? 'text-green-600 font-medium' : 'text-red-600 font-medium'}>
                          {h.risk_state}
                        </span>
                      </div>
                      <div>
                        <span className="text-gray-500">등록:</span> {h.enrolled_at?.slice(0, 10)}
                      </div>
                      <div>
                        <span className="text-gray-500">하트비트:</span> {h.last_heartbeat?.slice(0, 19) || '-'}
                      </div>
                      <div className="flex gap-2">
                        {session && (
                          <Link to={`/sessions/${session.id}/provenance`} className="text-blue-600 hover:underline">
                            프로바이던스 →
                          </Link>
                        )}
                      </div>
                    </div>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
