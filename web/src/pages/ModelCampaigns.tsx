import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'
import { GovernedActionModal } from '../components/GovernedActionModal'

type Campaign = {
  id: number; package_id: string; manifest_digest: string; targets_json: string
  state: string; delegation_json: string; reason: string; expected_epoch: number
  deadline: string; max_concurrent: number
}
type Row = { campaign: Campaign; targets: Target[]; distribution: Record<string, string | number> }
type Target = {
  id: number; organization_id: string; environment: string; ring: string
  observed_state: string; progress_bytes: number; current_digest: string
  reason_code: string; last_contact: string; approval_state: string
}
type EntitledPkg = { package_id: string; model_id: string; name_ko: string; version: string; quant_type: string }

const STATE_KO: Record<string, string> = {
  ineligible: '비대상', entitled: '자격 부여됨', awaiting_customer_approval: '고객 승인 대기',
  scheduled: '예약됨', downloading: '다운로드 중', verifying: '검증 중', staged: '스테이징됨',
  loading: '로드 중', canary: '카나리', active: '활성', paused: '일시중지', failed: '실패',
  rollback_in_progress: '롤백 진행 중', rolled_back: '롤백됨', blocked_recalled: '차단 (리콜)', offline_unknown: '오프라인/알 수 없음',
}
const STATE_TONE: Record<string, string> = {
  active: 'bg-emerald-100 text-emerald-700', canary: 'bg-sky-100 text-sky-700',
  staged: 'bg-sky-100 text-sky-700', downloading: 'bg-sky-100 text-sky-700', verifying: 'bg-sky-100 text-sky-700',
  awaiting_customer_approval: 'bg-amber-100 text-amber-800', offline_unknown: 'bg-gray-200 text-gray-700',
  blocked_recalled: 'bg-red-100 text-red-700', failed: 'bg-red-100 text-red-700',
  ineligible: 'bg-gray-100 text-gray-500', rolled_back: 'bg-gray-100 text-gray-600',
}
const CAMP_STATE_KO: Record<string, string> = { draft: '초안', active: '활성', paused: '일시중지', completed: '완료', cancelled: '취소됨' }

export default function ModelCampaigns() {
  const [rows, setRows] = useState<Row[]>([])
  const [entitled, setEntitled] = useState<EntitledPkg[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ package_id: '', orgs: '', reason: '', canary_pct: 10 })
  const [preview, setPreview] = useState<any>(null)
  const [entOpen, setEntOpen] = useState(false)
  const [ent, setEnt] = useState({ organization_id: '', package_id: '', reason: '' })
  const [action, setAction] = useState<{ row: Row; kind: 'pause' | 'activate' | 'rollback' | 'recall'; label: string } | null>(null)
  const [reason, setReason] = useState('')
  const [rollbackTo, setRollbackTo] = useState('')
  const [approveFor, setApproveFor] = useState<{ c: Campaign; t: Target } | null>(null)

  const load = () => {
    api.mdCampaigns().then((d: Row[]) => setRows(Array.isArray(d) ? d : [])).catch(() => {})
    api.mdEntitled().then((d: EntitledPkg[]) => setEntitled(Array.isArray(d) ? d : [])).catch(() => {})
  }
  useEffect(load, [])

  const runPreview = () =>
    api.mdPreview({
      package_id: form.package_id,
      targets_json: JSON.stringify(form.orgs.split(',').map((s) => s.trim()).filter(Boolean).map((organization_id) => ({ organization_id }))),
    }).then(setPreview).catch((e: any) => showToast(e.message))

  const doAction = () => {
    if (!action) return
    const a = action
    const done = () => { setAction(null); setReason(''); showToast('캠페인 작업을 수행했습니다'); load() }
    const fail = (e: any) => showToast(e.message || '작업 실패')
    if (a.kind === 'recall') {
      api.mdRecall({ package_id: a.row.campaign.package_id, reason }).then(done).catch(fail)
    } else if (a.kind === 'rollback') {
      if (!rollbackTo.trim()) {
        showToast('롤백 대상 패키지 ID가 필요합니다')
        return
      }
      api.mdRollback(a.row.campaign.id, { reason, rollback_to: rollbackTo.trim(), expected_epoch: a.row.campaign.expected_epoch }).then(done).catch(fail)
    } else {
      api.mdMutate(a.row.campaign.id, { action: a.kind, reason, expected_epoch: a.row.campaign.expected_epoch }).then(done).catch(fail)
    }
  }

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">모델 배포 캠페인 <span className="text-xs text-gray-400 ml-2">PAT-1444 · 서명 희망 상태 + 고객 측 아웃바운드 풀</span></h1>
          <p className="text-sm text-gray-500 mt-1">자격은 조회·다운로드 권한일 뿐 배포 권한이 아닙니다. 수동 승인이 기본이며, 침묵은 성공으로 해석되지 않습니다(오프라인/알 수 없음).</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={() => setEntOpen(true)}>자격 부여</button>
          <button className="btn text-sm" onClick={() => { setForm({ package_id: '', orgs: '', reason: '', canary_pct: 10 }); setPreview(null); setCreateOpen(true) }}>캠페인 생성</button>
        </div>
      </div>

      {/* Entitled packages (customer view) */}
      <div className="card">
        <div className="p-4 border-b"><h2 className="font-semibold">자격 있는 패키지 (아웃바운드 풀 대상)</h2></div>
        {entitled.length === 0 && <p className="p-4 text-sm text-gray-500">자격이 부여된 패키지가 없습니다.</p>}
        {entitled.map((p) => (
          <div key={p.package_id} className="p-3 border-t flex items-center justify-between text-xs">
            <div className="flex items-center gap-2">
              <span className="font-mono">{p.package_id}</span>
              <span className="px-1.5 py-0.5 rounded bg-gray-100">{p.model_id} {p.version}</span>
              {p.quant_type && <span className="text-gray-500">{p.quant_type}</span>}
            </div>
          </div>
        ))}
      </div>

      {/* Campaigns with target matrix */}
      {rows.map((row) => (
        <div key={row.campaign.id} className="card">
          <div className="p-4 border-b flex items-start justify-between gap-4">
            <div>
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium font-mono">{row.campaign.package_id}</span>
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${row.campaign.state === 'active' ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-100'}`}>{CAMP_STATE_KO[row.campaign.state] || row.campaign.state}</span>
                <span className="text-[11px] text-gray-400">epoch {row.campaign.expected_epoch}</span>
                <span className="text-[11px] font-mono text-gray-400">{row.campaign.manifest_digest?.slice(0, 19)}…</span>
              </div>
              <div className="text-xs text-gray-500 mt-1">
                사유: {row.campaign.reason}
                {row.campaign.deadline && ` · 기한 ${new Date(row.campaign.deadline).toLocaleDateString('ko-KR')}`}
                {` · 동시 다운로드 상한 ${row.campaign.max_concurrent}`}
                {JSON.parse(row.campaign.delegation_json || '{"auto":false}').auto === true && ' · 자동 롤아웃 위임 있음'}
              </div>
              <div className="flex gap-1 flex-wrap mt-1.5">
                {Object.entries(row.distribution || {}).map(([st, n]) => (
                  <span key={st} className={`text-[11px] px-1.5 py-0.5 rounded ${STATE_TONE[st] || 'bg-gray-100'}`}>{STATE_KO[st] || st} {n}</span>
                ))}
              </div>
            </div>
            <div className="flex gap-2 shrink-0">
              {row.campaign.state === 'draft' && <button className="btn-secondary text-xs" onClick={() => { setAction({ row, kind: 'activate', label: '활성화' }); setReason('') }}>활성화</button>}
              {row.campaign.state === 'active' && <button className="btn-secondary text-xs" onClick={() => { setAction({ row, kind: 'pause', label: '일시중지' }); setReason('') }}>일시중지</button>}
              <button className="btn-secondary text-xs" onClick={() => api.mdPromoteGate(row.campaign.id).then((r: any) => { showToast(`승격 ${r.promoted} · 차단 ${r.blocked} (증거 부족 시 차단)`); load() }).catch((e: any) => showToast(e.message))}>건강 게이트 승격</button>
              <button className="btn-secondary text-xs" onClick={() => { setAction({ row, kind: 'rollback', label: '롤백' }); setReason(''); setRollbackTo('') }}>롤백</button>
              <button className="btn-secondary text-xs" onClick={() => { setAction({ row, kind: 'recall', label: '긴급 리콜' }); setReason('') }}>리콜</button>
            </div>
          </div>
          <div className="overflow-auto max-h-56 text-xs">
            <table className="w-full">
              <thead className="text-gray-500 text-left"><tr><th className="py-1.5 px-4">조직</th><th>환경</th><th>링</th><th>관찰 상태</th><th>진행</th><th>승인</th><th>마지막 연락</th><th>사유 코드</th><th></th></tr></thead>
              <tbody>
                {row.targets.map((t) => (
                  <tr key={t.id} className="border-t">
                    <td className="py-1.5 px-4 font-mono">{t.organization_id}</td>
                    <td>{t.environment}</td>
                    <td>{t.ring}</td>
                    <td><span className={`px-1.5 py-0.5 rounded ${STATE_TONE[t.observed_state] || 'bg-gray-100'}`}>{STATE_KO[t.observed_state] || t.observed_state}</span></td>
                    <td>{(t.progress_bytes / 1e9).toFixed(1)}GB</td>
                    <td>{t.approval_state === 'granted' ? '승인됨' : t.approval_state === 'declined' ? '거부됨' : '필요'}</td>
                    <td className="text-gray-400">{t.last_contact ? new Date(t.last_contact).toLocaleString('ko-KR') : '없음'}</td>
                    <td className="text-gray-400">{t.reason_code}</td>
                    <td>{t.observed_state === 'awaiting_customer_approval' && (
                      <button className="btn-secondary text-[11px]" onClick={() => setApproveFor({ c: row.campaign, t })}>승인/거부</button>
                    )}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ))}
      {rows.length === 0 && <div className="card p-6 text-sm text-gray-500 text-center">캠페인이 없습니다.</div>}

      <button className="text-xs text-gray-400 hover:text-gray-600" onClick={() => api.mdReconcile().then((r: any) => { showToast(`유휴 타깃 ${r.marked_stale}개를 오프라인/알 수 없음으로 표시했습니다`); load() }).catch((e: any) => showToast(e.message))}>
        조정 스윕 실행 (30분 무연락 타깃 → 오프라인/알 수 없음)
      </button>

      {/* Create + preview */}
      <Modal open={createOpen} title="모델 배포 캠페인 생성" onClose={() => setCreateOpen(false)} size="md"
        footer={<ModalFooter onCancel={() => setCreateOpen(false)}
          onConfirm={() => api.mdCreateCampaign({
            package_id: form.package_id,
            targets_json: JSON.stringify(form.orgs.split(',').map((s) => s.trim()).filter(Boolean).map((organization_id) => ({ organization_id, environments: ['default'] }))),
            reason: form.reason, rings_json: JSON.stringify({ canary: { percentage: form.canary_pct } }),
          }).then(() => { setCreateOpen(false); showToast('캠페인 초안을 생성했습니다'); load() }).catch((e: any) => showToast(e.message))}
          confirmLabel="초안 생성" disabled={!form.package_id.trim() || !form.orgs.trim() || !form.reason.trim()} />}>
        <div className="space-y-3">
          <div><label className="label">패키지 ID *</label>
            <input className="input font-mono" value={form.package_id} onChange={(e) => setForm({ ...form, package_id: e.target.value })} placeholder="pmp_…" /></div>
          <div><label className="label">대상 조직 (쉼표 구분) *</label>
            <input className="input" value={form.orgs} onChange={(e) => setForm({ ...form, orgs: e.target.value })} placeholder="orgA, orgB" /></div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">카나리 비율 (%)</label>
              <input type="number" min={0} max={100} className="input" value={form.canary_pct} onChange={(e) => setForm({ ...form, canary_pct: Number(e.target.value) })} /></div>
            <div className="flex items-end"><button className="btn-secondary text-xs w-full" onClick={runPreview} disabled={!form.package_id || !form.orgs}>타깃 미리보기</button></div>
          </div>
          {preview && (
            <div className="text-xs bg-gray-50 rounded p-2">
              <div>적격: {(preview.eligible || []).map((e: any) => e.organization_id).join(', ') || '없음'}</div>
              <div className="text-red-500">부적격: {(preview.ineligible || []).map((e: any) => `${e.organization_id} (${e.reason})`).join(', ') || '없음'}</div>
            </div>
          )}
          <div><label className="label">사유 *</label>
            <input className="input" value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })} /></div>
          <p className="text-xs text-gray-500">활성화하려면 모든 타깃이 적격이어야 합니다. 수동 승인이 기본이며, 고객 승인 없이는 활성화로 전환되지 않습니다.</p>
        </div>
      </Modal>

      {/* Entitle */}
      <Modal open={entOpen} title="패키지 자격 부여 (조직 범위)" onClose={() => setEntOpen(false)} size="sm"
        footer={<ModalFooter onCancel={() => setEntOpen(false)}
          onConfirm={() => api.mdEntitle(ent).then(() => { setEntOpen(false); showToast('자격을 부여했습니다'); load() }).catch((e: any) => showToast(e.message))}
          confirmLabel="부여" disabled={!ent.organization_id.trim() || !ent.package_id.trim() || !ent.reason.trim()} />}>
        <div className="space-y-3">
          <div><label className="label">조직 ID</label>
            <input className="input" value={ent.organization_id} onChange={(e) => setEnt({ ...ent, organization_id: e.target.value })} /></div>
          <div><label className="label">패키지 ID</label>
            <input className="input font-mono" value={ent.package_id} onChange={(e) => setEnt({ ...ent, package_id: e.target.value })} /></div>
          <div><label className="label">사유</label>
            <input className="input" value={ent.reason} onChange={(e) => setEnt({ ...ent, reason: e.target.value })} /></div>
          <p className="text-xs text-gray-500">자격은 특정 불변 다이제스트에 대한 조회·다운로드 권한입니다. 다른 조직에는 보이지 않으며, 배포는 별도 승인이 필요합니다.</p>
        </div>
      </Modal>

      {/* Governed campaign action */}
      {action && (() => {
        const a = action
        return (
          <GovernedActionModal
            open
            danger={a.kind === 'recall' || a.kind === 'rollback'}
            requireConfirmPhrase={a.kind === 'recall'}
            confirmPhraseLabel="리콜이 모든 타깃의 새 선택을 즉시 차단함을 확인했습니다"
            title={`캠페인 ${a.label} · ${a.row.campaign.package_id}`}
            preview={
              <div className="space-y-2">
                <p className="text-sm">현재 상태 {CAMP_STATE_KO[a.row.campaign.state]} · 타깃 {a.row.targets.length}개 · epoch {a.row.campaign.expected_epoch}</p>
                {a.kind === 'rollback' && (
                  <div>
                    <label className="label">롤백 대상 패키지 ID (사전 검증 · 허용된 버전)</label>
                    <input className="input font-mono text-xs" value={rollbackTo}
                      onChange={(e) => setRollbackTo(e.target.value)} placeholder="pmp_…" />
                  </div>
                )}
              </div>
            }
            confirmLabel={`${a.label} 실행`}
            reason={reason}
            onReasonChange={setReason}
            onCancel={() => setAction(null)}
            onConfirm={doAction}
            canConfirm={a.kind !== 'rollback' || rollbackTo.trim().length > 0}
          />
        )
      })()}

      {/* Customer approve/decline */}
      {approveFor && (() => {
        const a = approveFor
        return (
          <Modal open title={`배포 승인 · ${a.t.organization_id} / ${a.t.environment}`} size="sm"
            onClose={() => setApproveFor(null)}
            footer={<div className="flex gap-2 justify-end">
              <button className="btn-secondary text-sm" onClick={() => {
                api.mdApprove(a.c.id, { environment: a.t.environment, approve: false })
                  .then(() => { setApproveFor(null); showToast('거부했습니다 — 반복 강제 설치는 일어나지 않습니다'); load() }).catch((e: any) => showToast(e.message))
              }}>거부</button>
              <button className="btn text-sm" onClick={() => {
                api.mdApprove(a.c.id, { environment: a.t.environment, approve: true })
                  .then(() => { setApproveFor(null); showToast('승인했습니다 — 아웃바운드 풀이 시작됩니다'); load() }).catch((e: any) => showToast(e.message))
              }}>승인</button>
            </div>}>
            <div className="space-y-2 text-sm">
              <p>패키지 <span className="font-mono">{a.c.package_id}</span></p>
              <p className="text-xs text-gray-500">승인하면 고객 측 배포 에이전트가 서명된 매니페스트를 검증한 뒤 아웃바운드로 다운로드합니다. 거부해도 타깃 상태가 보존되며 반복 설치가 강제되지 않습니다.</p>
            </div>
          </Modal>
        )
      })()}
    </div>
  )
}
