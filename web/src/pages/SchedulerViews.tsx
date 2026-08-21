import { useEffect, useState } from 'react'
import EmptyState from '../components/EmptyState'
import { api } from '../api'
import {
  kvDirViewModel, pdRows, programsViewModel, shadowViewModel,
  KVDirViewModel, PDRow, ProgramsViewModel, ShadowViewModel,
} from '../schedulerViews'

// SchedulerViews renders the PAT-1445 traffic-director panels: KV state
// plane, prefill/decode capacity, agent programs, and the baseline-vs-
// candidate shadow/canary comparison. Every panel degrades independently
// when its view is unavailable (scheduler down or older build).

function useView<T>(load: () => Promise<any>, shape: (raw: any) => T) {
  const [model, setModel] = useState<T | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let live = true
    load()
      .then((raw) => { if (live) setModel(shape(raw)) })
      .catch(() => { if (live) setFailed(true) })
    return () => { live = false }
  }, [])
  return { model, failed }
}

function Panel({ title, en, failed, children }: { title: string; en: string; failed: boolean; children: React.ReactNode }) {
  return (
    <section className="bg-white border border-gray-200 rounded-lg p-5">
      <h2 className="text-lg font-semibold mb-1">{title} <span className="text-gray-400 text-sm font-normal">{en}</span></h2>
      {failed ? <p className="text-sm text-gray-400 py-4">뷰를 사용할 수 없습니다 · View unavailable (scheduler offline or older build)</p> : children}
    </section>
  )
}

export default function SchedulerViews() {
  const kvdir = useView<KVDirViewModel>(api.schedulerKVDir, kvDirViewModel)
  const pd = useView<PDRow[]>(api.schedulerPD, pdRows)
  const programs = useView<ProgramsViewModel>(api.schedulerPrograms, programsViewModel)
  const shadow = useView<ShadowViewModel>(api.schedulerShadow, shadowViewModel)

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">스케줄러 <span className="text-gray-400 text-lg font-normal">Traffic Director</span></h1>
      <p className="text-xs text-gray-400 mb-6">KV 상태 평면, 프리필/디코드 용량, 에이전트 프로그램, 섀도/칸ary 정책 비교 · PAT-1445</p>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
        <Panel title="KV 디렉터리" en="KV State Plane" failed={kvdir.failed}>
          {kvdir.model && (
            kvdir.model.extents === 0 ? <EmptyState message="캐시 익스텐트가 없습니다" /> : (
              <div className="space-y-3 text-sm">
                <div className="flex gap-6 text-gray-600">
                  <span>익스텐트 <b>{kvdir.model.extents}</b></span>
                  <span>검증됨 <b>{kvdir.model.verified}</b></span>
                  <span>미검증 <b>{kvdir.model.unverified}</b></span>
                </div>
                <table className="w-full text-left">
                  <thead><tr className="text-xs text-gray-400 border-b"><th className="py-1">티어</th><th>로케이션</th></tr></thead>
                  <tbody>
                    {kvdir.model.tiers.map((t) => (
                      <tr key={t.tier} className="border-b border-gray-100"><td className="py-1 font-mono">{t.tier}</td><td>{t.locations}</td></tr>
                    ))}
                  </tbody>
                </table>
                {kvdir.model.hotPrefixes.length > 0 && (
                  <div>
                    <h3 className="text-xs font-semibold text-gray-500 mb-1">핫 프리픽스 (복제 후보)</h3>
                    <ul className="text-xs text-gray-600 space-y-0.5">
                      {kvdir.model.hotPrefixes.map((h) => (
                        <li key={h.hash} className="font-mono truncate">{h.hash.slice(0, 16)}… · hits {h.hits} · replicas {h.replicas} · {h.tokens} tok</li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )
          )}
        </Panel>

        <Panel title="P/D 용량" en="Prefill/Decode Capacity" failed={pd.failed}>
          {pd.model && (
            pd.model.length === 0 ? <EmptyState message="등록된 모델이 없습니다" /> : (
              <table className="w-full text-sm text-left">
                <thead>
                  <tr className="text-xs text-gray-400 border-b">
                    <th className="py-1">모델</th><th>프리필 점유</th><th>상태</th><th>P</th><th>D</th><th>통합</th>
                  </tr>
                </thead>
                <tbody>
                  {pd.model.map((m) => (
                    <tr key={m.model} className="border-b border-gray-100">
                      <td className="py-1 font-medium">{m.model}</td>
                      <td>{Math.round(m.prefillShare * 100)}%</td>
                      <td>
                        {m.imbalance
                          ? <span className="text-red-600 font-semibold">불균형</span>
                          : m.engaged
                            ? <span className="text-amber-600">분리됨</span>
                            : <span className="text-gray-500">통합</span>}
                      </td>
                      <td>{m.prefill}</td><td>{m.decode}</td><td>{m.aggregated}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          )}
        </Panel>

        <Panel title="에이전트 프로그램" en="Agent Programs" failed={programs.failed}>
          {programs.model && (
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div><div className="text-xs text-gray-400">프로그램</div><div className="text-xl font-semibold">{programs.model.programs}</div></div>
              <div><div className="text-xs text-gray-400">툴 대기 중</div><div className="text-xl font-semibold">{programs.model.toolPaused}</div></div>
              <div><div className="text-xs text-gray-400">누적 턴</div><div className="text-xl font-semibold">{programs.model.turns}</div></div>
              <div><div className="text-xs text-gray-400">일시 예측 오차</div><div className="text-xl font-semibold">{programs.model.predictionErrors}</div></div>
            </div>
          )}
        </Panel>

        <Panel title="섀도 / 칸ary" en="Shadow & Canary" failed={shadow.failed}>
          {shadow.model && (
            <div className="space-y-3 text-sm">
              <div className="flex gap-6 text-gray-600">
                <span>리시트 <b>{shadow.model.receipts}</b></span>
                <span>섀도 <b>{shadow.model.shadowed}</b></span>
                <span>일치율 <b>{shadow.model.agreementPct === null ? '—' : `${shadow.model.agreementPct}%`}</b></span>
              </div>
              {shadow.model.canary && (
                <div className="text-sm">
                  칸ary <b>{shadow.model.canary.capability}</b>: {shadow.model.canary.state}
                  {shadow.model.canary.active && <span className="ml-2 text-green-600 font-semibold">ACTIVE</span>}
                </div>
              )}
              {shadow.model.filtered.length > 0 && (
                <div>
                  <h3 className="text-xs font-semibold text-gray-500 mb-1">필터 사유</h3>
                  <ul className="text-xs text-gray-600 space-y-0.5">
                    {shadow.model.filtered.map((f) => (
                      <li key={f.reason}>{f.reason}: {f.count}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </Panel>
      </div>
    </div>
  )
}
