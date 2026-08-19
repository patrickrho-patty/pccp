import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'
import { GovernedActionModal } from '../components/GovernedActionModal'
import { HV_STATE_KO, HV_STATE_TONE, HV_CAMPAIGN_STATE_KO, RING_KO, isValidVersion, deadlineTone } from '../releaseCampaignView'

type Release = {
  id: number; release_id: string; version: string; build_profile: string
  platform: string; artifact_digest: string; channel: string
  published_at: string; revoked: boolean; revoked_reason: string
}
type Campaign = {
  id: number; release_id: string; target_version: string; min_version: string
  ring: string; percentage: number; start_time: string; deadline: string
  severity: string; state: string; reason: string; expected_epoch: number
}
type FleetState = { harnesses: any[]; distribution: Record<string, number> }
type Exception = {
  id: number; harness_ids_json: string; current_version: string; target_version: string
  reason: string; owner: string; approved_by: string; compensating_controls: string
  expires_at: string; revoked: boolean
}

export default function ReleaseCampaigns() {
  const [releases, setReleases] = useState<Release[]>([])
  const [campaigns, setCampaigns] = useState<Campaign[]>([])
  const [fleet, setFleet] = useState<FleetState | null>(null)
  const [exceptions, setExceptions] = useState<Exception[]>([])
  const [preview, setPreview] = useState<any>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ release_id: '', target_version: '', min_version: '', ring: 'canary', percentage: 10, deadline: '', reason: '' })
  const [mutateFor, setMutateFor] = useState<Campaign | null>(null)
  const [mut, setMut] = useState({ action: 'activate', reason: '' })
  const [relOpen, setRelOpen] = useState(false)
  const [rel, setRel] = useState({ release_id: '', version: '', build_profile: 'enterprise', platform: 'linux/amd64/deb', artifact_digest: '', channel: 'stable' })
  const [revokeFor, setRevokeFor] = useState<Release | null>(null)
  const [revokeReason, setRevokeReason] = useState('')
  const [excOpen, setExcOpen] = useState(false)
  const [exc, setExc] = useState({ harness_ids: '', current_version: '', target_version: '', reason: '', owner: '', approved_by: '', compensating_controls: '', expires_at: '' })

  const load = () => {
    api.hvReleases().then((d: Release[]) => setReleases(Array.isArray(d) ? d : [])).catch(() => {})
    api.hvCampaigns().then((d: Campaign[]) => setCampaigns(Array.isArray(d) ? d : [])).catch(() => {})
    api.hvFleetStates().then(setFleet).catch(() => {})
    api.hvExceptions().then((d: Exception[]) => setExceptions(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(load, [])

  const runPreview = () => {
    api.hvPreview({ min_version: form.min_version, target_version: form.target_version, percentage: form.percentage, cohort_seed: 'preview' })
      .then(setPreview).catch((e: any) => showToast(e.message))
  }

  const createCampaign = () =>
    api.hvCreateCampaign({ ...form, percentage: Number(form.percentage) }).then(() => {
      setCreateOpen(false); showToast('캠페인 초안을 생성했습니다'); load()
    }).catch((e: any) => showToast(e.message))

  const doMutate = () => {
    if (!mutateFor) return
    const c = mutateFor
    api.hvMutate(c.id, { ...mut, expected_epoch: c.expected_epoch }).then(() => {
      setMutateFor(null); setMut({ action: 'activate', reason: '' }); showToast('캠페인 상태를 변경했습니다'); load()
    }).catch((e: any) => showToast(e.message))
  }

  const doRevoke = () => {
    if (!revokeFor) return
    const rl = revokeFor
    api.hvRevokeRelease(rl.id, { reason: revokeReason }).then(() => {
      setRevokeFor(null); setRevokeReason(''); showToast('릴리스를 폐기했습니다 (즉시 적용)'); load()
    }).catch((e: any) => showToast(e.message))
  }

  const createException = () =>
    api.hvCreateException({ ...exc, harness_ids: exc.harness_ids.split(',').map((s) => s.trim()).filter(Boolean) })
      .then(() => { setExcOpen(false); showToast('예외를 생성했습니다'); load() })
      .catch((e: any) => showToast(e.message))

  const formValid = isValidVersion(form.target_version) && (form.min_version === '' || isValidVersion(form.min_version)) && form.reason.trim() !== ''

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">릴리스 캠페인 <span className="text-xs text-gray-400 ml-2">PAT-1449 · 하네스 버전 거버넌스</span></h1>
          <p className="text-sm text-gray-500 mt-1">대상/최소 버전을 분리하고 결정론적 코호트로 단계 배포합니다. 검증 가능한 빌드 정체성(릴리스 ID + 다이제스트)만 신뢰합니다.</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={() => setRelOpen(true)}>릴리스 등록</button>
          <button className="btn text-sm" onClick={() => { setForm({ release_id: '', target_version: '', min_version: '', ring: 'canary', percentage: 10, deadline: '', reason: '' }); setPreview(null); setCreateOpen(true) }}>캠페인 생성</button>
        </div>
      </div>

      {/* Fleet state distribution */}
      {fleet && (
        <div className="card p-4">
          <h2 className="font-semibold mb-3">플릿 버전 상태 분포 (도출된 상태 — 편집 불가)</h2>
          <div className="flex gap-2 flex-wrap">
            {Object.entries(fleet.distribution || {}).map(([state, n]) => (
              <span key={state} className={`text-xs px-2 py-1 rounded ${HV_STATE_TONE[state] || 'bg-gray-100'}`}>
                {HV_STATE_KO[state] || state} {n}
              </span>
            ))}
            {Object.keys(fleet.distribution || {}).length === 0 && <span className="text-sm text-gray-500">아직 하트비트 보고가 없습니다.</span>}
          </div>
          {fleet.harnesses?.length > 0 && (
            <div className="mt-3 max-h-48 overflow-auto text-xs">
              <table className="w-full">
                <thead className="text-gray-500 text-left"><tr><th className="py-1">하네스</th><th>버전</th><th>릴리스</th><th>설치 소유자</th><th>상태</th><th>사유</th></tr></thead>
                <tbody>
                  {fleet.harnesses.map((h: any) => (
                    <tr key={h.harness_id} className="border-t">
                      <td className="py-1 font-mono">{h.harness_id}</td>
                      <td>{h.version}</td>
                      <td className="font-mono text-gray-500">{h.release_id}</td>
                      <td>{h.installation_owner}</td>
                      <td><span className={`px-1.5 py-0.5 rounded ${HV_STATE_TONE[h.state]}`}>{HV_STATE_KO[h.state]}</span></td>
                      <td className="text-gray-400">{h.reason}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Campaigns */}
      <div className="card">
        <div className="p-4 border-b"><h2 className="font-semibold">캠페인</h2></div>
        {campaigns.length === 0 && <p className="p-4 text-sm text-gray-500">캠페인이 없습니다.</p>}
        {campaigns.map((c) => {
          const tone = deadlineTone(c.deadline)
          return (
            <div key={c.id} className="p-4 border-t flex items-start justify-between gap-4">
              <div className="min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium">→ {c.target_version}</span>
                  {c.min_version && <span className="text-xs px-1.5 py-0.5 rounded bg-gray-100">최소 {c.min_version}</span>}
                  <span className="text-[11px] px-1.5 py-0.5 rounded bg-sky-100 text-sky-700">{RING_KO[c.ring]} {c.percentage}%</span>
                  <span className={`text-[11px] px-1.5 py-0.5 rounded ${c.state === 'active' ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-100'}`}>{HV_CAMPAIGN_STATE_KO[c.state] || c.state}</span>
                  {c.severity === 'emergency' && <span className="text-[11px] px-1.5 py-0.5 rounded bg-red-100 text-red-700">긴급</span>}
                  {tone === 'soon' && <span className="text-[11px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800">기한 임박</span>}
                  {tone === 'past' && <span className="text-[11px] px-1.5 py-0.5 rounded bg-orange-100 text-orange-700">기한 경과 (제한 전환)</span>}
                </div>
                <div className="text-xs text-gray-500 mt-1">
                  사유: {c.reason}
                  {c.deadline && ` · 시행 기한 ${new Date(c.deadline).toLocaleString('ko-KR', { timeZone: 'Asia/Seoul' })} KST`}
                  {` · epoch ${c.expected_epoch}`}
                </div>
              </div>
              <div className="flex gap-2 shrink-0">
                {c.state === 'draft' && <button className="btn-secondary text-xs" onClick={() => { setMutateFor(c); setMut({ action: 'activate', reason: '' }) }}>활성화</button>}
                {c.state === 'active' && <button className="btn-secondary text-xs" onClick={() => { setMutateFor(c); setMut({ action: 'pause', reason: '' }) }}>일시중지</button>}
                {c.state === 'paused' && <button className="btn-secondary text-xs" onClick={() => { setMutateFor(c); setMut({ action: 'resume', reason: '' }) }}>재개</button>}
                {c.state !== 'cancelled' && <button className="btn-secondary text-xs" onClick={() => { setMutateFor(c); setMut({ action: 'cancel', reason: '' }) }}>취소</button>}
                {c.state === 'active' && <button className="btn-secondary text-xs" onClick={() => { setMutateFor(c); setMut({ action: 'rollback', reason: '' }) }}>롤백</button>}
              </div>
            </div>
          )
        })}
      </div>

      {/* Release catalog */}
      <div className="card">
        <div className="p-4 border-b"><h2 className="font-semibold">릴리스 카탈로그 (서명 · 다이제스트)</h2></div>
        {releases.map((rl) => (
          <div key={rl.id} className="p-3 border-t flex items-center justify-between text-xs">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-mono">{rl.release_id}</span>
              <span className="px-1.5 py-0.5 rounded bg-gray-100">{rl.version}</span>
              <span className="px-1.5 py-0.5 rounded bg-gray-100">{rl.build_profile}</span>
              <span className="text-gray-500">{rl.platform}</span>
              <span className="font-mono text-gray-400">{rl.artifact_digest?.slice(0, 19)}…</span>
              {rl.revoked && <span className="px-1.5 py-0.5 rounded bg-red-100 text-red-700">폐기됨 — {rl.revoked_reason}</span>}
            </div>
            {!rl.revoked && (
              <button className="btn-secondary text-xs" onClick={() => setRevokeFor(rl)}>긴급 폐기</button>
            )}
          </div>
        ))}
        {releases.length === 0 && <p className="p-4 text-sm text-gray-500">등록된 릴리스가 없습니다.</p>}
      </div>

      {/* Exceptions */}
      <div className="card">
        <div className="p-4 border-b flex items-center justify-between">
          <h2 className="font-semibold">버전 예외 (범위 · 만료 · 승인 필수)</h2>
          <button className="btn-secondary text-xs" onClick={() => setExcOpen(true)}>예외 생성</button>
        </div>
        {exceptions.map((ex) => (
          <div key={ex.id} className="p-3 border-t flex items-center justify-between text-xs">
            <div>
              <div className="flex items-center gap-2">
                <span className="font-mono">{(JSON.parse(ex.harness_ids_json || '[]')).join(', ')}</span>
                <span className="px-1.5 py-0.5 rounded bg-gray-100">{ex.current_version} → {ex.target_version}</span>
                {ex.revoked
                  ? <span className="px-1.5 py-0.5 rounded bg-gray-200 text-gray-600">철회됨</span>
                  : <span className="px-1.5 py-0.5 rounded bg-amber-100 text-amber-800">만료 {new Date(ex.expires_at).toLocaleDateString('ko-KR')}</span>}
              </div>
              <div className="text-gray-500 mt-0.5">{ex.reason} · 보완 통제: {ex.compensating_controls} · 소유 {ex.owner} / 승인 {ex.approved_by}</div>
            </div>
            {!ex.revoked && (
              <button className="btn-secondary text-xs" onClick={() =>
                api.hvRevokeException(ex.id, { reason: '위험 상승 (콘솔에서 철회)' }).then(load).catch((e: any) => showToast(e.message))}>철회</button>
            )}
          </div>
        ))}
        {exceptions.length === 0 && <p className="p-4 text-sm text-gray-500">예외가 없습니다.</p>}
      </div>

      {/* Campaign create + preview */}
      <Modal open={createOpen} title="릴리스 캠페인 생성" onClose={() => setCreateOpen(false)} size="lg"
        footer={<ModalFooter onCancel={() => setCreateOpen(false)} onConfirm={createCampaign} confirmLabel="초안 생성" disabled={!formValid} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-3 gap-3">
            <div><label className="label">대상 버전 *</label>
              <input className="input" value={form.target_version} onChange={(e) => setForm({ ...form, target_version: e.target.value })} placeholder="1.6.0" /></div>
            <div><label className="label">최소 버전 (시행 하한)</label>
              <input className="input" value={form.min_version} onChange={(e) => setForm({ ...form, min_version: e.target.value })} placeholder="1.5.0" /></div>
            <div><label className="label">릴리스 ID</label>
              <input className="input" value={form.release_id} onChange={(e) => setForm({ ...form, release_id: e.target.value })} placeholder="rel-1.6.0" /></div>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <div><label className="label">링</label>
              <select className="input" value={form.ring} onChange={(e) => setForm({ ...form, ring: e.target.value })}>
                <option value="canary">카나리</option><option value="beta">베타</option><option value="stable">안정</option>
              </select></div>
            <div><label className="label">코호트 비율 (%)</label>
              <input type="number" min={0} max={100} className="input" value={form.percentage} onChange={(e) => setForm({ ...form, percentage: Number(e.target.value) })} /></div>
            <div><label className="label">시행 기한</label>
              <input type="datetime-local" className="input" value={form.deadline} onChange={(e) => setForm({ ...form, deadline: e.target.value })} /></div>
          </div>
          <div><label className="label">사유 *</label>
            <input className="input" value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })} placeholder="예: 보안 취약점 조기 차단" /></div>
          <div className="flex items-center gap-2">
            <button className="btn-secondary text-xs" disabled={!form.min_version || !isValidVersion(form.min_version)} onClick={runPreview}>영향 미리보기</button>
            {preview && (
              <span className="text-xs text-gray-600">
                영향 {preview.counts?.affected ?? 0} · 이미 준수 {preview.counts?.already_compliant ?? 0} · 코호트 제외 {preview.counts?.excluded_by_cohort ?? 0} · 검증 불가 {preview.counts?.ineligible_unknown ?? 0}
              </span>
            )}
          </div>
          <p className="text-xs text-gray-500">동일 (코호트 시드, 하네스) 조합의 소속은 항상 동일하게 결정되어 하트비트 사이에 코호트가 흔들리지 않습니다. 최소 버전은 안정 릴리스만 지정할 수 있습니다.</p>
        </div>
      </Modal>

      {/* Governed campaign mutation */}
      {mutateFor && (() => {
        const c = mutateFor
        return (
          <GovernedActionModal
            open
            title={`캠페인 ${mut.action === 'activate' ? '활성화' : mut.action === 'pause' ? '일시중지' : mut.action === 'resume' ? '재개' : mut.action === 'cancel' ? '취소' : '롤백'}`}
            subtitle={`→ ${c.target_version} · epoch ${c.expected_epoch}`}
            warnings={mut.action === 'rollback' || mut.action === 'cancel' ? [{ kind: 'high', text: '진행 중인 배포가 중단됩니다. 건강한 하네스는 다운그레이드되지 않습니다.' }] : []}
            preview={<p className="text-sm">현재 상태: {HV_CAMPAIGN_STATE_KO[c.state]} · 링 {RING_KO[c.ring]} {c.percentage}%</p>}
            confirmLabel="실행"
            reason={mut.reason}
            onReasonChange={(reason) => setMut({ ...mut, reason })}
            onCancel={() => setMutateFor(null)}
            onConfirm={doMutate}
          />
        )
      })()}

      {/* Emergency revoke */}
      {revokeFor && (() => {
        const rl = revokeFor
        return (
          <GovernedActionModal
            open
            danger
            requireConfirmPhrase
            confirmPhraseLabel="폐기가 즉시 적용되고 해당 릴리스를 실행하는 모든 하네스가 제한 모드로 전환됨을 확인했습니다"
            title={`릴리스 긴급 폐기 · ${rl.release_id}`}
            subtitle={rl.version}
            preview={<p className="text-sm">폐기된 릴리스를 실행 중인 하네스는 즉시 revoked 상태로 전환되고, 실행이 거부됩니다. 예외로 우회할 수 없습니다.</p>}
            confirmLabel="폐기 실행"
            reason={revokeReason}
            onReasonChange={setRevokeReason}
            onCancel={() => setRevokeFor(null)}
            onConfirm={doRevoke}
          />
        )
      })()}

      {/* Release register */}
      <Modal open={relOpen} title="릴리스 등록 (카탈로그)" onClose={() => setRelOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setRelOpen(false)}
          onConfirm={() => api.hvRegisterRelease(rel).then(() => { setRelOpen(false); showToast('릴리스를 등록했습니다'); load() }).catch((e: any) => showToast(e.message))}
          confirmLabel="등록" disabled={!rel.release_id.trim() || !isValidVersion(rel.version)} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">릴리스 ID (불변)</label>
              <input className="input" value={rel.release_id} onChange={(e) => setRel({ ...rel, release_id: e.target.value })} placeholder="rel-1.6.0" /></div>
            <div><label className="label">버전</label>
              <input className="input" value={rel.version} onChange={(e) => setRel({ ...rel, version: e.target.value })} placeholder="1.6.0" /></div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">빌드 프로파일</label>
              <select className="input" value={rel.build_profile} onChange={(e) => setRel({ ...rel, build_profile: e.target.value })}>
                <option value="public">public</option><option value="enterprise">enterprise</option><option value="sovereign">sovereign</option>
              </select></div>
            <div><label className="label">채널</label>
              <select className="input" value={rel.channel} onChange={(e) => setRel({ ...rel, channel: e.target.value })}>
                <option value="stable">안정</option><option value="beta">베타</option><option value="canary">카나리</option>
              </select></div>
          </div>
          <div><label className="label">플랫폼</label>
            <input className="input" value={rel.platform} onChange={(e) => setRel({ ...rel, platform: e.target.value })} /></div>
          <div><label className="label">아티팩트 다이제스트</label>
            <input className="input font-mono" value={rel.artifact_digest} onChange={(e) => setRel({ ...rel, artifact_digest: e.target.value })} placeholder="sha256:…" /></div>
          <p className="text-xs text-gray-500">하트비트 보고의 (릴리스 ID, 다이제스트, 프로파일)이 카탈로그와 일치해야 유효한 버전으로 인정됩니다. 불일치는 구버전이 아니라 '검증 불가' 상태가 됩니다.</p>
        </div>
      </Modal>

      {/* Exception create */}
      <Modal open={excOpen} title="버전 예외 생성 (만료 필수)" onClose={() => setExcOpen(false)} size="lg"
        footer={<ModalFooter onCancel={() => setExcOpen(false)} onConfirm={createException} confirmLabel="예외 생성"
          disabled={!exc.harness_ids.trim() || !exc.reason.trim() || !exc.owner.trim() || !exc.approved_by.trim() || !exc.compensating_controls.trim() || !exc.expires_at} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-3 gap-3">
            <div><label className="label">대상 하네스 (쉼표 구분)</label>
              <input className="input" value={exc.harness_ids} onChange={(e) => setExc({ ...exc, harness_ids: e.target.value })} placeholder="h1, h2" /></div>
            <div><label className="label">현재 버전</label>
              <input className="input" value={exc.current_version} onChange={(e) => setExc({ ...exc, current_version: e.target.value })} /></div>
            <div><label className="label">대상 버전</label>
              <input className="input" value={exc.target_version} onChange={(e) => setExc({ ...exc, target_version: e.target.value })} /></div>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <div><label className="label">소유자</label>
              <input className="input" value={exc.owner} onChange={(e) => setExc({ ...exc, owner: e.target.value })} /></div>
            <div><label className="label">승인자</label>
              <input className="input" value={exc.approved_by} onChange={(e) => setExc({ ...exc, approved_by: e.target.value })} /></div>
            <div><label className="label">만료 (90일 이내)</label>
              <input type="datetime-local" className="input" value={exc.expires_at} onChange={(e) => setExc({ ...exc, expires_at: e.target.value })} /></div>
          </div>
          <div><label className="label">사유</label>
              <input className="input" value={exc.reason} onChange={(e) => setExc({ ...exc, reason: e.target.value })} /></div>
          <div><label className="label">보완 통제</label>
            <input className="input" value={exc.compensating_controls} onChange={(e) => setExc({ ...exc, compensating_controls: e.target.value })} placeholder="예: 해당 하네스 네트워크 격리" /></div>
          <p className="text-xs text-gray-500">예외는 일반 고객 통제 기한만 유예할 수 있습니다. 폐기된 릴리스, 검증 불가 정체성, 호스팅 보안 하한은 예외로 우회할 수 없으며 만료 시 자동으로 무효가 됩니다.</p>
        </div>
      </Modal>
    </div>
  )
}
