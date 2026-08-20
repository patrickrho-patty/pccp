import { useState, useEffect } from 'react'
import { api } from '../api'

// GPU Queue / QoS Ops (PAT-1443) — 내부 SRE/엔지니어링 전용 표면.
// 측정된 사실과 추정값을 명시적으로 구분하고 신뢰도/최신 시각을 표시합니다.
// 사용자/세션/요청/저장소/프롬프트 식별자는 수집되지 않습니다.

type Forecast = {
  p50_wait_ms: number
  p90_wait_ms: number
  sample_n: number
  window_hours: number
  mae_p50_ms: number
  mae_p90_ms: number
  underpredict_rate: number
  health: string
}

export default function QosOps() {
  const [snapshot, setSnapshot] = useState<any>(null)
  const [outcomes, setOutcomes] = useState<any>(null)
  const [timeline, setTimeline] = useState<any[]>([])
  const [deployment, setDeployment] = useState('public')
  const [model, setModel] = useState('')

  const reload = () => {
    const q = `?deployment=${encodeURIComponent(deployment)}${model ? `&model=${encodeURIComponent(model)}` : ''}`
    api.qosSnapshot(q).then(setSnapshot).catch(() => setSnapshot(null))
    api.qosOutcomes(`?deployment=${encodeURIComponent(deployment)}`).then(setOutcomes).catch(() => setOutcomes(null))
    api.qosTimeline(q).then(setTimeline).catch(() => setTimeline([]))
  }
  useEffect(() => { reload() }, [deployment])

  const f: Forecast | undefined = snapshot?.forecast
  const pct = snapshot?.wait_percentiles || {}

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-semibold">GPU 대기열 / QoS 운영</h1>
          <p className="text-xs text-gray-500 mt-1">
            내부 SRE/용량 분석 전용 · 측정값과 추정값(신뢰도 포함)을 구분 · 인프라 차원만 집계
          </p>
        </div>
        <div className="flex gap-2">
          <select className="input max-w-[160px] text-xs" value={deployment} onChange={e => setDeployment(e.target.value)}>
            <option value="public">public</option><option value="enterprise">enterprise</option><option value="sovereign">sovereign</option>
          </select>
          <input className="input max-w-[180px] text-xs" placeholder="모델 필터" value={model} onChange={e => setModel(e.target.value)} />
          <button className="btn-sm btn-secondary" onClick={reload}>조회</button>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
        <Metric label="Active / Max" value={snapshot ? `${snapshot.active ?? 0} / ${snapshot.max_concurrency ?? 50}` : '—'} accent="blue" />
        <Metric label="Queued" value={snapshot ? String(snapshot.queued ?? 0) : '—'} accent="orange" />
        <Metric label="Service Rate (완료)" value={snapshot ? String(snapshot.service_rate ?? 0) : '—'} accent="green" />
        <Metric label="Forecast p50" value={f && f.health !== 'insufficient' ? `${Math.round(f.p50_wait_ms)}ms` : f ? '근거 부족' : '—'}
          accent={f?.health === 'degraded' ? 'red' : 'green'} sub={f && f.health !== 'insufficient' ? `p90 ${Math.round(f.p90_wait_ms)}ms · n=${f.sample_n}` : undefined} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 mb-4">
        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">대기 퍼센타일 (측정)</h2>
          {pct && Object.keys(pct).length ? (
            <div className="space-y-1 text-xs">
              {Object.entries(pct).map(([k, v]) => (
                <div key={k} className="flex justify-between border-b border-gray-50 py-1">
                  <span className="text-gray-500">{k.toUpperCase()}</span>
                  <span className="font-mono">{Math.round(Number(v))}ms</span>
                </div>
              ))}
              <div className="text-[10px] text-gray-400 pt-1">window {snapshot?.measured?.window_hours}h · sample {snapshot?.measured?.sample_n} · 업데이트 {snapshot?.measured?.updated_at ? new Date(snapshot.measured.updated_at).toLocaleString() : '—'}</div>
            </div>
          ) : <p className="text-xs text-gray-400">측정 데이터 없음</p>}
          <div className="text-[10px] text-gray-400 mt-2">측정값: 통계적으로 올바른 정렬 분포 퍼센타일 경로 (삽입순 오류 제거됨)</div>
        </div>

        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">대기 예측 (추정 · EWMA)</h2>
          {f ? (
            <div className="text-xs space-y-1">
              <div className="flex justify-between"><span className="text-gray-500">건강</span>
                <span>{f.health === 'healthy' ? '🟢 양호' : f.health === 'degraded' ? '🟠 저하' : '⚪ 근거 부족'}</span></div>
              <div className="flex justify-between"><span className="text-gray-500">p50 예측</span><span className="font-mono">{Math.round(f.p50_wait_ms)}ms</span></div>
              <div className="flex justify-between"><span className="text-gray-500">p90 보수 범위</span><span className="font-mono">{Math.round(f.p90_wait_ms)}ms</span></div>
              <div className="flex justify-between"><span className="text-gray-500">샘플 / 창</span><span className="font-mono">{f.sample_n} / {f.window_hours}h</span></div>
              <div className="flex justify-between"><span className="text-gray-500">MAE p50/p90</span><span className="font-mono">{Math.round(f.mae_p50_ms)} / {Math.round(f.mae_p90_ms)}ms</span></div>
              <div className="flex justify-between"><span className="text-gray-500">저예측률</span><span className="font-mono">{Math.round((f.underpredict_rate || 0) * 100)}%</span></div>
              <div className="text-[10px] text-gray-400 pt-1">추정값은 신뢰/최신 시각과 함께 제공되며 불충분 시 숨김 — 허위 정밀도 없음</div>
            </div>
          ) : <p className="text-xs text-gray-400">예측 없음</p>}
        </div>

        <div className="card p-4">
          <h2 className="text-sm font-medium mb-2">대기열 결과(24h)</h2>
          {outcomes?.outcomes ? (
            <div className="space-y-1 text-xs">
              {Object.entries(outcomes.outcomes).map(([k, v]) => (
                <div key={k} className="flex justify-between border-b border-gray-50 py-1">
                  <span className="text-gray-500">{k}</span>
                  <span className="font-mono">{String(v)}</span>
                </div>
              ))}
            </div>
          ) : <p className="text-xs text-gray-400">결과 없음</p>}
        </div>
      </div>

      <div className="card p-4">
        <h2 className="text-sm font-medium mb-2">최근 익명 라이프사이클(드릴다운, 식별자 없음)</h2>
        {timeline.length === 0 ? <p className="text-xs text-gray-400">타임라인 이벤트 없음</p> : (
          <table className="w-full text-xs">
            <thead><tr className="text-left text-gray-500 border-b border-gray-100">
              <th className="py-1 px-1">시각</th><th className="py-1 px-1">라이프사이클</th><th className="py-1 px-1">모델</th><th className="py-1 px-1">티어/클래스</th><th className="py-1 px-1 text-right">대기(ms)</th>
            </tr></thead>
            <tbody>
              {timeline.map((e, i) => (
                <tr key={i} className="border-b border-gray-50">
                  <td className="py-1 px-1 text-gray-400">{new Date(e.occurred_at).toLocaleTimeString()}</td>
                  <td className="py-1 px-1 font-mono">{e.lifecycle}</td>
                  <td className="py-1 px-1">{e.model || '—'}</td>
                  <td className="py-1 px-1">{e.traffic_class || '—'}</td>
                  <td className="py-1 px-1 text-right font-mono">{e.queue_wait_ms ? `${e.queue_wait_ms}ms` : ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function Metric({ label, value, accent, sub }: { label: string; value: string; accent: string; sub?: string }) {
  const colors: Record<string, string> = {
    blue: 'text-blue-700', orange: 'text-orange-700', green: 'text-green-700', red: 'text-red-700',
  }
  return (
    <div className="card p-3">
      <div className="text-[10px] text-gray-500">{label}</div>
      <div className={`text-2xl font-semibold ${colors[accent] || 'text-gray-900'}`}>{value}</div>
      {sub && <div className="text-[10px] text-gray-400">{sub}</div>}
    </div>
  )
}
