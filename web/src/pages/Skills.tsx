import { useState, useEffect } from 'react'
import { api } from '../api'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'

// Skills (PAT-1456) — managed skill inventory + Required/Optional/Blocked
// policy controls. List, detail, and the effective-state explanation all come
// from the same admin endpoint so the surface cannot drift.

type SkillRow = {
  skill_identity: string
  display_name?: string
  execution_mode?: string
  source?: string
  plugin_package?: string
  package_digest?: string
  content_digest?: string
  description?: string
  required: number
  optional: number
  blocked: number
  unknown: number
  installed: number
  missing: number
  unverified: number
  drifted: number
  shadowed: number
  affected: number
}

const STATE_META: Record<string, { ko: string; color: string }> = {
  required: { ko: '필수', color: 'bg-blue-50 text-blue-700 border-blue-200' },
  optional: { ko: '선택', color: 'bg-gray-100 text-gray-600 border-gray-200' },
  blocked: { ko: '차단', color: 'bg-red-50 text-red-700 border-red-200' },
}

export default function Skills() {
  const confirm = useConfirm()
  const [items, setItems] = useState<SkillRow[]>([])
  const [enforcement, setEnforcement] = useState(true)
  const [hasAssignments, setHasAssignments] = useState(false)
  const [q, setQ] = useState('')
  const [stateFilter, setStateFilter] = useState('any')
  const [driftFilter, setDriftFilter] = useState('')
  const [loading, setLoading] = useState(true)
  // Assignment modal
  const [assignModal, setAssignModal] = useState(false)
  const [editRow, setEditRow] = useState<SkillRow | null>(null)
  const [form, setForm] = useState({ skill_identity: '', scope: 'org', scope_id: '', state: 'blocked', digest: '', reason: '' })
  const [delivering, setDelivering] = useState(false)

  const reload = (qur = q, st = stateFilter, dr = driftFilter) => {
    setLoading(true)
    const params = new URLSearchParams()
    if (qur) params.set('q', qur)
    if (st && st !== 'any') params.set('state', st)
    if (dr) params.set('drift', dr)
    const qs = params.toString()
    api.listSkills(qs ? `?${qs}` : '').then((data) => {
      setItems(Array.isArray(data.items) ? data.items : [])
      setEnforcement(data.enforcement !== false)
      setHasAssignments(!!data.has_assignments)
      setLoading(false)
    }).catch(() => setLoading(false))
  }

  useEffect(() => { reload() }, [])

  const openAssign = (row: SkillRow | null) => {
    setEditRow(row)
    setForm({
      skill_identity: row?.skill_identity || '',
      scope: 'org', scope_id: '', state: 'blocked', digest: row?.content_digest || '', reason: '',
    })
    setAssignModal(true)
  }

  const saveAssignment = async () => {
    if (!form.skill_identity.trim()) { showToast('스킬 ID를 입력하세요', 'error'); return }
    try {
      await api.upsertSkillAssignment(form)
      showToast('정책 적용됨', 'success')
      setAssignModal(false)
      reload()
    } catch (e: any) {
      showToast(e.message || '실패', 'error')
    }
  }



  const deliver = async () => {
    setDelivering(true)
    try {
      const out = await api.deliverSkillEpoch()
      showToast(`배포 완료 — epoch ${out.epoch_number} · 대상 ${out.targets}대`, 'success')
    } catch (e: any) {
      showToast(e.message || '배포 실패', 'error')
    } finally {
      setDelivering(false)
    }
  }

  const badge = (n: number, meta: { ko: string; color: string }) => (
    n > 0 ? <span className={`text-[10px] px-2 py-0.5 rounded-full border ${meta.color}`}>{n} {meta.ko}</span> : null
  )

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-semibold">스킬 관리</h1>
          <p className="text-xs text-gray-500 mt-1">
            하네스에 설치된 스킬을 확인하고 필수/선택/차단을 조직·팀·플릿·사용자 범위로 제어합니다.
            {' '}{enforcement ? '실행 중 (알 수 없는 스킬 = 차단)' : '감사 모드 (차단하지 않음)'}
          </p>
        </div>
        <div className="flex gap-2">
          {hasAssignments && (
            <button className="btn-sm btn-secondary" onClick={deliver} disabled={delivering}>
              {delivering ? '배포 중…' : '📤 정책 배포'}
            </button>
          )}
          <button className="btn-sm btn-primary" onClick={() => openAssign(null)}>+ 정책 추가</button>
        </div>
      </div>

      <div className="flex gap-2 mb-3 flex-wrap">
        <input className="input max-w-[260px] text-xs" placeholder="스킬 ID / 이름 / 패키지 검색" value={q}
          onChange={e => setQ(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') reload() }} />
        <select className="input max-w-[150px] text-xs" value={stateFilter} onChange={e => { setStateFilter(e.target.value); reload(q, e.target.value, driftFilter) }}>
          <option value="any">전체 상태</option>
          <option value="required">필수</option>
          <option value="optional">선택</option>
          <option value="blocked">차단</option>
          <option value="unknown">미지정</option>
          <option value="unverified">미검증</option>
        </select>
        <select className="input max-w-[140px] text-xs" value={driftFilter} onChange={e => { setDriftFilter(e.target.value); reload(q, stateFilter, e.target.value) }}>
          <option value="">전체</option>
          <option value="drifted">드리프트/누락</option>
        </select>
        <button className="btn-sm btn-secondary" onClick={() => { reload(); }}>조회</button>
      </div>

      <div className="card overflow-x-auto">
        {loading ? (
          <p className="text-sm text-gray-400 p-4">불러오는 중…</p>
        ) : items.length === 0 ? (
          <EmptyState title="등록된 스킬 없음" desc="하네스가 아직 스킬 인벤토리를 보고하지 않았거나 정책이 없습니다." />
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-gray-500 border-b border-gray-100">
                <th className="py-2 px-2">스킬</th>
                <th className="py-2 px-2">소스</th>
                <th className="py-2 px-2">상태 요약</th>
                <th className="py-2 px-2">설치</th>
                <th className="py-2 px-2">건강</th>
                <th className="py-2 px-2 text-right">동작</th>
              </tr>
            </thead>
            <tbody>
              {items.map(row => (
                <tr key={row.skill_identity} className="border-b border-gray-50 hover:bg-gray-50/50">
                  <td className="py-2 px-2">
                    <div className="font-medium">{row.display_name || row.skill_identity}</div>
                    <div className="text-[10px] text-gray-400 font-mono">{row.skill_identity}</div>
                  </td>
                  <td className="py-2 px-2 text-gray-500">
                    {row.source || '—'}{row.plugin_package ? <div className="text-[10px] font-mono">{row.plugin_package}</div> : null}
                  </td>
                  <td className="py-2 px-2">
                    <div className="flex gap-1 flex-wrap">
                      {badge(row.required, STATE_META.required)}
                      {badge(row.blocked, STATE_META.blocked)}
                      {row.unknown > 0 ? <span className="text-[10px] px-2 py-0.5 rounded-full border bg-amber-50 text-amber-700 border-amber-200">{row.unknown} 미지정</span> : null}
                      {badge(row.optional, STATE_META.optional)}
                    </div>
                  </td>
                  <td className="py-2 px-2">{row.installed}<span className="text-gray-400">/하네스 {row.affected}</span></td>
                  <td className="py-2 px-2">
                    <div className="flex gap-1 flex-wrap">
                      {row.missing > 0 ? <span className="text-[10px] px-2 py-0.5 rounded-full border bg-red-50 text-red-700 border-red-200">{row.missing} 누락</span> : null}
                      {row.drifted > 0 ? <span className="text-[10px] px-2 py-0.5 rounded-full border bg-yellow-50 text-yellow-700 border-yellow-200">{row.drifted} 드리프트</span> : null}
                      {row.unverified > 0 ? <span className="text-[10px] px-2 py-0.5 rounded-full border bg-amber-50 text-amber-700 border-amber-200">{row.unverified} 미검증</span> : null}
                      {row.shadowed > 0 ? <span className="text-[10px] px-2 py-0.5 rounded-full border bg-gray-100 text-gray-500 border-gray-200">{row.shadowed} 섀도</span> : null}
                    </div>
                  </td>
                  <td className="py-2 px-2 text-right whitespace-nowrap">
                    <button className="btn-sm btn-secondary" onClick={() => openAssign(row)}>정책</button>
                    <button className="btn-sm btn-secondary ml-1" onClick={() => removeAssignment(row)}>해제</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {assignModal && (
        <Modal title={editRow ? `정책 설정 — ${editRow.skill_identity}` : '새 정책'} onClose={() => setAssignModal(false)}>
          <div className="space-y-3">
            {!editRow && (
              <div>
                <label className="text-xs text-gray-500">스킬 ID (캐노니컬 identity@package)</label>
                <input className="input text-xs" placeholder="skill@package" value={form.skill_identity}
                  onChange={e => setForm({ ...form, skill_identity: e.target.value })} />
              </div>
            )}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs text-gray-500">적용 범위</label>
                <select className="input text-xs" value={form.scope} onChange={e => setForm({ ...form, scope: e.target.value })}>
                  <option value="org">조직</option>
                  <option value="team">팀</option>
                  <option value="fleet">플릿/하네스</option>
                  <option value="user">사용자</option>
                </select>
              </div>
              <div>
                <label className="text-xs text-gray-500">{form.scope === 'org' ? '범위 ID' : '대상 ID'}</label>
                <input className="input text-xs" placeholder={form.scope === 'org' ? '(조직 전체)' : 'ID'} value={form.scope_id}
                  disabled={form.scope === 'org'}
                  onChange={e => setForm({ ...form, scope_id: e.target.value })} />
              </div>
            </div>
            <div>
              <label className="text-xs text-gray-500">상태</label>
              <select className="input text-xs" value={form.state} onChange={e => setForm({ ...form, state: e.target.value })}>
                <option value="required">필수 (Required)</option>
                <option value="optional">선택 (Optional)</option>
                <option value="blocked">차단 (Blocked)</option>
              </select>
            </div>
            <div>
              <label className="text-xs text-gray-500">승인 다이제스트 (검증된 버전)</label>
              <input className="input text-xs font-mono" placeholder="approved content digest" value={form.digest}
                onChange={e => setForm({ ...form, digest: e.target.value })} />
            </div>
            <div>
              <label className="text-xs text-gray-500">사유</label>
              <textarea className="input text-xs" rows={2} value={form.reason}
                onChange={e => setForm({ ...form, reason: e.target.value })} />
            </div>
          </div>
          <ModalFooter>
            <button className="btn-sm btn-secondary" onClick={() => setAssignModal(false)}>취소</button>
            <button className="btn-sm btn-primary" onClick={saveAssignment}>저장</button>
          </ModalFooter>
        </Modal>
      )}
    </div>
  )
}
