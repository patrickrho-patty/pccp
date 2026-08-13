import { useState, useEffect } from 'react'

type SeatData = {
  user_seats: { used: number; max: number; available: number; utilization: string }
  harness_seats: { used: number; max: number; available: number; utilization: string }
  active_sessions: number
  plan_tier: string
  plan_renewal_date: string
}

export function SeatWidget({ compact = false }: { compact?: boolean }) {
  const [seats, setSeats] = useState<SeatData | null>(null)

  useEffect(() => {
    let active = true
    let interval: ReturnType<typeof setInterval> | null = null

    const load = async () => {
      const token = localStorage.getItem('pccp_token')
      if (!token) return // Don't fetch without a token

      try {
        const res = await fetch('/api/organizations/seats', {
          headers: { Authorization: `Bearer ${token}` }
        })
        if (!res.ok) {
          // Stop polling on auth failure
          if (interval) clearInterval(interval)
          return
        }
        const d = await res.json()
        if (active && d && d.user_seats) {
          setSeats(d)
        }
      } catch {
        // Network error - stop polling
        if (interval) clearInterval(interval)
      }
    }

    load()
    interval = setInterval(load, 30000)

    return () => {
      active = false
      if (interval) clearInterval(interval)
    }
  }, [])

  if (!seats || !seats.user_seats || !seats.harness_seats) return null

  const userPct = seats.user_seats.utilization ? parseInt(seats.user_seats.utilization) : 0
  const harnessPct = seats.harness_seats.utilization ? parseInt(seats.harness_seats.utilization) : 0

  if (compact) {
    return (
      <div className="text-xs space-y-1">
        <div className="flex justify-between">
          <span className="text-gray-500">사용자</span>
          <span className={userPct >= 80 ? 'text-red-400' : 'text-gray-300'}>
            {seats.user_seats.used}/{seats.user_seats.max}
          </span>
        </div>
        <div className="flex justify-between">
          <span className="text-gray-500">하네스</span>
          <span className={harnessPct >= 80 ? 'text-red-400' : 'text-gray-300'}>
            {seats.harness_seats.used}/{seats.harness_seats.max}
          </span>
        </div>
        <div className="flex justify-between">
          <span className="text-gray-500">플랜</span>
          <span className="text-gray-300 uppercase">{seats.plan_tier}</span>
        </div>
      </div>
    )
  }

  return (
    <div className="card mb-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold">시트 관리 · Seat Management</h3>
        <div className="flex items-center gap-2">
          <span className={`badge-gray uppercase`}>{seats.plan_tier}</span>
          <span className="text-xs text-gray-400">활성 세션: {seats.active_sessions}</span>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-6">
        <SeatBar label="사용자 시트 · User Seats" used={seats.user_seats.used} max={seats.user_seats.max} pct={userPct} />
        <SeatBar label="하네스 시트 · Harness Seats" used={seats.harness_seats.used} max={seats.harness_seats.max} pct={harnessPct} />
      </div>
      {seats.plan_renewal_date && !seats.plan_renewal_date.startsWith('0001') && (
        <div className="mt-3 text-xs text-gray-400">플랜 갱신일: {seats.plan_renewal_date.slice(0, 10)}</div>
      )}
    </div>
  )
}

function SeatBar({ label, used, max, pct }: { label: string; used: number; max: number; pct: number }) {
  const color = pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-yellow-500' : 'bg-green-500'
  const textColor = pct >= 90 ? 'text-red-600' : pct >= 70 ? 'text-yellow-600' : 'text-gray-700'
  return (
    <div>
      <div className="flex justify-between items-baseline mb-1">
        <span className="text-xs font-medium text-gray-600">{label}</span>
        <span className={`text-sm font-bold ${textColor}`}>{used} / {max}</span>
      </div>
      <div className="w-full bg-gray-100 rounded-full h-2 overflow-hidden">
        <div className={`h-full ${color} rounded-full transition-all`} style={{ width: `${Math.min(pct, 100)}%` }} />
      </div>
      <div className="flex justify-between mt-1">
        <span className="text-[10px] text-gray-400">{max - used}석 사용 가능</span>
        <span className={`text-[10px] ${textColor}`}>{pct}% 사용 중</span>
      </div>
    </div>
  )
}
