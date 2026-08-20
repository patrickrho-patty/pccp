import { useEffect, useState } from 'react'
import { api } from '../api'
import { showToast } from '../components/Toast'
import { Modal, ModalFooter } from '../components/Modal'
import { GovernedActionModal } from '../components/GovernedActionModal'

type Campaign = {
  id: number; advertiser: string; category: string; state: string
  headline_en: string; body_en: string; headline_ko: string; body_ko: string
  destination_url: string; display_domain: string; creative_revision: number
  start_at: string; end_at: string; weight: number; impression_ceiling: number
  cpm_minor: number; currency: string; budget_minor: number
  expected_impressions: number; validated_impressions: number; clicks: number
  spend_minor: number; remaining_budget_minor: number; delivery_pct: number; eligible: boolean
}

const STATE_KO: Record<string, string> = { draft: '초안', active: '활성', paused: '일시중지', ended: '종료' }

const krw = (minor: number) => `₩${minor.toLocaleString('ko-KR')}`

export default function AdCampaigns() {
  const [campaigns, setCampaigns] = useState<Campaign[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({
    advertiser: '', category: '', headline_en: '', body_en: '', headline_ko: '', body_ko: '',
    destination_url: '', weight: 1, impression_ceiling: 0, cpm_minor: 1000, budget_minor: 100000,
  })
  const [lifecycleFor, setLifecycleFor] = useState<{ c: Campaign; action: string } | null>(null)
  const [reason, setReason] = useState('')
  const [previewFor, setPreviewFor] = useState<Campaign | null>(null)

  const load = () => api.adCampaigns().then((d: Campaign[]) => setCampaigns(Array.isArray(d) ? d : [])).catch(() => {})
  useEffect(() => { load() }, [])

  const create = () => {
    api.adCreate(form).then(() => { setCreateOpen(false); showToast('캠페인 초안을 생성했습니다'); load() })
      .catch((e: any) => showToast(e.message))
  }

  const doLifecycle = () => {
    if (!lifecycleFor) return
    const { c, action } = lifecycleFor
    api.adLifecycle(c.id, action, reason).then(() => {
      setLifecycleFor(null); setReason(''); showToast('캠페인 상태를 변경했습니다'); load()
    }).catch((e: any) => showToast(e.message))
  }

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">광고 캠페인 <span className="text-xs text-gray-400 ml-2">PAT-1435 · 비개인화 · 정수 회계</span></h1>
          <p className="text-sm text-gray-500 mt-1">Patty 플랫폼 운영자 전용입니다. 지출 = 검증 노출 × CPM / 1000 (최소 통화 단위), 예산·상한은 트랜잭션으로 강제됩니다.</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary text-sm" onClick={() =>
            api.adPublishCatalog().then((r: any) => { showToast(`카탈로그 리비전 ${r.revision} 발행 (캠페인 ${r.campaigns}개, 만료 ${new Date(r.expires_at).toLocaleTimeString('ko-KR')})`); load() })
              .catch((e: any) => showToast(e.message))}>카탈로그 서명 · 발행</button>
          <button className="btn text-sm" onClick={() => setCreateOpen(true)}>캠페인 생성</button>
        </div>
      </div>

      {campaigns.map((c) => (
        <div key={c.id} className="card p-4">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium">{c.advertiser}</span>
                <span className={`text-[11px] px-1.5 py-0.5 rounded ${c.state === 'active' ? 'bg-emerald-100 text-emerald-700' : c.state === 'paused' ? 'bg-amber-100 text-amber-800' : 'bg-gray-100'}`}>{STATE_KO[c.state] || c.state}</span>
                {c.category && <span className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100">{c.category}</span>}
                <span className="text-[11px] text-gray-400">리비전 {c.creative_revision}</span>
                {c.eligible ? <span className="text-[11px] px-1.5 py-0.5 rounded bg-sky-50 text-sky-700">카탈로그 포함</span>
                  : <span className="text-[11px] px-1.5 py-0.5 rounded bg-gray-100 text-gray-500">카탈로그 제외</span>}
              </div>
              <div className="text-xs text-gray-600 mt-1.5 truncate">
                <b>{c.headline_en}</b> — {c.body_en}
                {c.headline_ko && <span className="text-gray-400"> · {c.headline_ko} — {c.body_ko}</span>}
              </div>
              <div className="text-[11px] text-gray-400 mt-0.5 font-mono">{c.display_domain}</div>
              <div className="grid grid-cols-6 gap-2 mt-3 text-xs">
                <div><span className="text-gray-400">검증 노출</span><div className="font-bold">{c.validated_impressions.toLocaleString('ko-KR')}</div></div>
                <div><span className="text-gray-400">예상</span><div>{c.expected_impressions.toLocaleString('ko-KR')}</div></div>
                <div><span className="text-gray-400">게재율</span><div>{c.delivery_pct.toFixed(1)}%</div></div>
                <div><span className="text-gray-400">클릭</span><div>{c.clicks.toLocaleString('ko-KR')}</div></div>
                <div><span className="text-gray-400">지출</span><div className="font-bold">{krw(c.spend_minor)}</div></div>
                <div><span className="text-gray-400">잔여</span><div>{krw(c.remaining_budget_minor)} / {krw(c.budget_minor)}</div></div>
              </div>
            </div>
            <div className="flex flex-col gap-1.5 shrink-0">
              <button className="btn-secondary text-xs" onClick={() => setPreviewFor(c)}>미리보기</button>
              {c.state === 'draft' && <button className="btn-secondary text-xs" onClick={() => { setLifecycleFor({ c, action: 'activate' }); setReason('') }}>활성화</button>}
              {c.state === 'active' && <button className="btn-secondary text-xs" onClick={() => { setLifecycleFor({ c, action: 'pause' }); setReason('') }}>일시중지</button>}
              {c.state !== 'ended' && <button className="btn-secondary text-xs" onClick={() => { setLifecycleFor({ c, action: 'end' }); setReason('') }}>종료</button>}
            </div>
          </div>
        </div>
      ))}
      {campaigns.length === 0 && <div className="card p-6 text-sm text-gray-500 text-center">캠페인이 없습니다.</div>}

      {/* Terminal card preview */}
      {previewFor && (
        <Modal open title="터미널 카드 미리보기" size="lg" onClose={() => setPreviewFor(null)}
          footer={<ModalFooter onCancel={() => setPreviewFor(null)} onConfirm={() => setPreviewFor(null)} confirmLabel="닫기" />}>
          <div className="font-mono text-sm space-y-2">
            <div className="border border-gray-300 dark:border-gray-600 rounded px-3 py-2 hover:border-emerald-500 transition-colors group">
              <div className="flex justify-between">
                <span className="font-bold group-hover:text-emerald-600">{previewFor.headline_en}</span>
                <span className="text-gray-400 text-xs">Ad</span>
              </div>
              <div className="text-gray-500 truncate">{previewFor.body_en}</div>
              <a className="underline text-xs group-hover:text-emerald-600" href={previewFor.destination_url} target="_blank" rel="noreferrer">{previewFor.display_domain}</a>
            </div>
            {previewFor.headline_ko && (
              <div className="border border-gray-300 rounded px-3 py-2 group hover:border-emerald-500 transition-colors">
                <div className="flex justify-between">
                  <span className="font-bold group-hover:text-emerald-600">{previewFor.headline_ko}</span>
                  <span className="text-gray-400 text-xs">Ad · 한국어 로캘</span>
                </div>
                <div className="text-gray-500 truncate">{previewFor.body_ko}</div>
                <span className="underline text-xs group-hover:text-emerald-600">{previewFor.display_domain}</span>
              </div>
            )}
            <p className="text-[11px] text-gray-400">고정 높이 카드 — 헤드라인 1줄, 본문 1줄 말줄임, 우측 Ad 라벨. 호버/포커스 시 테두리·헤드라인·링크만 강조색으로 전환됩니다.</p>
          </div>
        </Modal>
      )}

      {/* Create */}
      <Modal open={createOpen} title="광고 캠페인 생성" onClose={() => setCreateOpen(false)} size="lg"
        footer={<ModalFooter onCancel={() => setCreateOpen(false)} onConfirm={create} confirmLabel="초안 생성"
          disabled={!form.advertiser.trim() || !form.headline_en.trim() || !form.body_en.trim() || !form.destination_url.trim()} />}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">광고주 *</label>
              <input className="input" value={form.advertiser} onChange={(e) => setForm({ ...form, advertiser: e.target.value })} /></div>
            <div><label className="label">카테고리</label>
              <input className="input" value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })} /></div>
          </div>
          <div><label className="label">영어 헤드라인 * (120자)</label>
            <input className="input" value={form.headline_en} onChange={(e) => setForm({ ...form, headline_en: e.target.value })} /></div>
          <div><label className="label">영어 본문 * (240자)</label>
            <input className="input" value={form.body_en} onChange={(e) => setForm({ ...form, body_en: e.target.value })} /></div>
          <div><label className="label">한국어 헤드라인 (선택 — 없으면 영어 사용)</label>
            <input className="input" value={form.headline_ko} onChange={(e) => setForm({ ...form, headline_ko: e.target.value })} /></div>
          <div><label className="label">한국어 본문 (선택)</label>
            <input className="input" value={form.body_ko} onChange={(e) => setForm({ ...form, body_ko: e.target.value })} /></div>
          <div><label className="label">대상 URL * (https)</label>
            <input className="input font-mono" value={form.destination_url} onChange={(e) => setForm({ ...form, destination_url: e.target.value })} placeholder="https://…" /></div>
          <div className="grid grid-cols-4 gap-3">
            <div><label className="label">가중치</label>
              <input type="number" min={1} className="input" value={form.weight} onChange={(e) => setForm({ ...form, weight: Number(e.target.value) })} /></div>
            <div><label className="label">노출 상한 (0=무제한)</label>
              <input type="number" min={0} className="input" value={form.impression_ceiling} onChange={(e) => setForm({ ...form, impression_ceiling: Number(e.target.value) })} /></div>
            <div><label className="label">CPM (원)</label>
              <input type="number" min={1} className="input" value={form.cpm_minor} onChange={(e) => setForm({ ...form, cpm_minor: Number(e.target.value) })} /></div>
            <div><label className="label">예산 (원)</label>
              <input type="number" min={1} className="input" value={form.budget_minor} onChange={(e) => setForm({ ...form, budget_minor: Number(e.target.value) })} /></div>
          </div>
          <p className="text-xs text-gray-500">예상 노출 = min(상한, 예산×1000÷CPM). 검증 노출만 과금되며 텍스트 전용 크리에이티브만 허용됩니다.</p>
        </div>
      </Modal>

      {/* Governed lifecycle */}
      {lifecycleFor && (
        <GovernedActionModal
          open
          danger={lifecycleFor.action === 'end'}
          title={`캠페인 ${lifecycleFor.action === 'activate' ? '활성화' : lifecycleFor.action === 'pause' ? '일시중지' : '종료'} · ${lifecycleFor.c.advertiser}`}
          preview={<p className="text-sm">현재 {STATE_KO[lifecycleFor.c.state]} · 검증 노출 {lifecycleFor.c.validated_impressions.toLocaleString('ko-KR')} · 지출 {krw(lifecycleFor.c.spend_minor)}</p>}
          confirmLabel="실행"
          reason={reason}
          onReasonChange={setReason}
          onCancel={() => setLifecycleFor(null)}
          onConfirm={doLifecycle}
        />
      )}
    </div>
  )
}
