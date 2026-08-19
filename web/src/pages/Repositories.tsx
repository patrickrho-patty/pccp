import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useServerTable } from '../hooks/useServerTable'
import { useFavorites, FavoriteStar } from '../hooks/useFavorites'
import { useRowNav } from '../hooks/useRowNav'
import { EntitySelect } from '../components/EntitySelect'
import { ResponsiveTable, Column } from '../components/ResponsiveTable'
import { Modal, ModalFooter } from '../components/Modal'
import EmptyState from '../components/EmptyState'
import { exportCSV } from '../utils/csv'
import { formatShortTime } from '../utils/format'
import { showToast } from '../components/Toast'
import { useConfirm } from '../components/useConfirm'
import { resolveRepoSync } from '../repoSync'

const PAGE_SIZE = 25

export default function Repositories() {
  const confirm = useConfirm()
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [bpRepo, setBpRepo] = useState<any>(null)
  const [bpLevel, setBpLevel] = useState('standard')
  const [bpBranch, setBpBranch] = useState('main')
  const [webhookRepo, setWebhookRepo] = useState<any>(null)
  const [webhookInfo, setWebhookInfo] = useState<any>(null)
  const [baselineRepo, setBaselineRepo] = useState<any>(null)
  const [baselineForm, setBaselineForm] = useState({ branch: '', commit_sha: '', commit_message: '', author_name: '', author_email: '' })
  const { favorites, sortPinnedFirst } = useFavorites('repositories')
  const [selectedRepos, setSelectedRepos] = useState<Set<string>>(new Set())
  const [syncingIds, setSyncingIds] = useState<Set<string>>(new Set())
  const [form, setForm] = useState({ name: '', slug: '', project_id: '', scm_provider: 'github', clone_url: '', default_branch: 'main', sensitivity: 'internal' })

  const table = useServerTable<any>((q) =>
    api.listRepositories({
      page: String(q.page), size: String(q.size), search: q.search,
      sort: q.sort,
      ...q.filters,
    })
  , { size: PAGE_SIZE })

  const rows = useMemo(() => sortPinnedFirst(table.rows, r => r.id), [table.rows, favorites])
  const openDetail = (r: any) => window.location.assign(`/repositories/${r.id}`)
  const { selectedIndex } = useRowNav(rows.length, (i) => openDetail(rows[i]), true)

  const resetForm = () => setForm({ name: '', slug: '', project_id: '', scm_provider: 'github', clone_url: '', default_branch: 'main', sensitivity: 'internal' })

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createRepository(form)
      resetForm(); setShowForm(false)
      showToast('저장소 등록됨', 'success')
      table.reload()
    } catch (err: any) { showToast('생성 실패: ' + err.message, 'error') }
  }

  const handleEdit = (r: any) => {
    setEditingId(r.id)
    setForm({ name: r.name || '', slug: r.slug || '', project_id: r.project_id || '', scm_provider: r.scm_provider || 'github', clone_url: r.clone_url || '', default_branch: r.default_branch || 'main', sensitivity: r.sensitivity || 'internal' })
    setShowForm(true)
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingId) return
    try {
      await api.updateRepository(editingId, form)
      setEditingId(null); setShowForm(false)
      showToast('수정 완료', 'success')
      table.reload()
    } catch (err: any) { showToast('수정 실패: ' + err.message, 'error') }
  }

  const handleDelete = async (r: any) => {
    if (!await confirm({ title: '삭제 확인', message: `'${r.name}' 저장소를 삭제(등록 해제)하시겠습니까?`, danger: true })) return
    try { await api.deleteRepository(r.id); showToast('등록 해제됨', 'info'); table.reload() } catch { showToast('실패했습니다 · action failed', 'error') }
  }

  const handleSync = async (r: any) => {
    if (syncingIds.has(r.id)) return // idempotence: no duplicate jobs
    setSyncingIds(prev => new Set(prev).add(r.id))
    showToast('동기화 시작...', 'info')
    try {
      const res: any = await api.syncRepository(r.id)
      showToast(`동기화 완료 · HEAD ${res.head?.slice(0, 8)}`, 'success')
      table.reload()
    } catch (err: any) { showToast('동기화 실패: ' + err.message, 'error') }
    finally { setSyncingIds(prev => { const n = new Set(prev); n.delete(r.id); return n }) }
  }

  const copyText = (text: string) => {
    navigator.clipboard?.writeText(text)
    showToast('클립보드에 복사됨', 'info')
  }

  const openBranchProtection = (r: any) => {
    setBpRepo(r)
    setBpLevel('standard')
    setBpBranch(r.default_branch || 'main')
  }

  const submitBranchProtection = async () => {
    if (!bpRepo) return
    try {
      await fetch('/api/scm/branch-protection', {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ repository_id: bpRepo.id, branch: bpBranch, level: bpLevel, requires_approval: bpLevel === 'release' || bpLevel === 'production' || bpLevel === 'locked' }),
      })
      setBpRepo(null)
      showToast('브랜치 보호 설정됨', 'success')
    } catch { showToast('설정 실패', 'error') }
  }

  const openWebhook = async (r: any) => {
    setWebhookRepo(r)
    try { setWebhookInfo(await api.repoWebhookInfo(r.id)) } catch { setWebhookInfo(null) }
  }

  const submitBaseline = async () => {
    if (!baselineRepo || !baselineForm.commit_sha) { showToast('커밋 SHA를 입력하세요', 'error'); return }
    try {
      await fetch(`/api/repositories/${baselineRepo.id}/baselines`, {
        method: 'POST', headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ branch: baselineForm.branch, commit_sha: baselineForm.commit_sha, commit_message: baselineForm.commit_message, author_name: baselineForm.author_name, author_email: baselineForm.author_email }),
      })
      setBaselineRepo(null)
      showToast('베이스라인 기록됨 (§18.3)', 'success')
    } catch { showToast('기록 실패', 'error') }
  }

  const scmIcon: Record<string, string> = { github: '🐙', gitlab: '🦊', bitbucket: '🪣', gitea: '🍵', git: '📦' }
  // Canonical sync state (PAT-1493) — one object shared with the detail
  // page, resolved once per row per rows-change and reused by every
  // column/expand render.
  const syncById = useMemo(() => {
    const m = new Map<string, ReturnType<typeof resolveRepoSync>>()
    for (const r of rows) m.set(r.id, resolveRepoSync(r))
    return m
  }, [rows])
  const syncOf = (r: any) => syncById.get(r.id) ?? resolveRepoSync(r)

  const columns: Column<any>[] = [
    {
      key: 'sel', header: '✓', cardLabel: '선택',
      render: (r) => (
        <input type="checkbox" checked={selectedRepos.has(r.id)} onClick={e => e.stopPropagation()} onChange={() => { const n = new Set(selectedRepos); if (n.has(r.id)) n.delete(r.id); else n.add(r.id); setSelectedRepos(n) }} />
      ),
    },
    { key: 'pin', header: '★', cardLabel: '고정', render: (r) => <FavoriteStar entity="repositories" id={r.id} /> },
    {
      key: 'name', header: '저장소', cardLabel: '저장소',
      render: (r) => {
        const sync = syncOf(r)
        return (
        <div className="flex items-center gap-2">
          <span className="text-lg">{scmIcon[r.scm_provider] || '📦'}</span>
          <div>
            <div className="font-medium text-sm"><Link to={`/repositories/${r.id}`} className="text-blue-600 hover:underline">{r.name}</Link> <span className={sync.badgeClass}>{sync.phaseLabel}</span></div>
            {r.clone_url && <div className="text-xs text-gray-400 font-mono truncate max-w-xs">{r.clone_url} <button className="text-gray-400 hover:text-blue-600" onClick={e => { e.stopPropagation(); copyText(r.clone_url) }} title="복사">⧉</button></div>}
          </div>
        </div>
        )
      },
      onClick: (r) => setExpandedId(expandedId === r.id ? null : r.id),
    },
    {
      key: 'project', header: '프로젝트', cardLabel: '프로젝트',
      render: (r) => r.project_id
        ? <Link to={`/projects/${r.project_id}`} className="text-sm text-blue-600 hover:underline" onClick={e => e.stopPropagation()}>프로젝트 →</Link>
        : <span className="text-xs text-gray-400">-</span>,
    },
    { key: 'scm', header: 'SCM', cardLabel: 'SCM', render: (r) => <span className="badge-gray">{r.scm_provider || 'git'}</span> },
    {
      key: 'branch', header: '브랜치', cardLabel: '브랜치',
      render: (r) => <span className="text-sm font-mono">{r.default_branch || 'main'}</span>,
    },
    {
      key: 'sensitivity', header: '민감도', cardLabel: '민감도',
      render: (r) => <span className={r.sensitivity === 'restricted' || r.sensitivity === 'confidential' ? 'badge-red' : r.sensitivity === 'internal' ? 'badge-yellow' : 'badge-blue'}>{r.sensitivity || 'internal'}</span>,
    },
    {
      key: 'last', header: '마지막 동기화', cardLabel: '마지막 동기화',
      render: (r) => <span className="text-xs text-gray-400">{formatShortTime(syncOf(r).lastSuccessAt)}</span>,
    },
    {
      key: 'actions', header: '작업', cardLabel: '작업',
      render: (r) => (
        <div className="flex gap-2 flex-wrap" onClick={e => e.stopPropagation()}>
          <button onClick={() => handleSync(r)} disabled={syncingIds.has(r.id)} className="text-xs text-green-600 hover:underline disabled:text-gray-400 disabled:no-underline">{syncingIds.has(r.id) ? '동기화 중...' : '동기화'}</button>
          <button onClick={() => handleEdit(r)} className="btn-link">편집</button>
          <button onClick={() => openBranchProtection(r)} className="text-xs text-yellow-600 hover:underline">브랜치 보호</button>
          <button onClick={() => { setBaselineRepo(r); setBaselineForm({ branch: r.default_branch || 'main', commit_sha: '', commit_message: '', author_name: '', author_email: '' }) }} className="btn-link">베이스라인</button>
          <button onClick={() => openWebhook(r)} className="text-xs text-gray-500 hover:underline">웹훅</button>
          <button onClick={() => handleDelete(r)} className="btn-link-danger">삭제</button>
        </div>
      ),
    },
  ]

  const expanded = (r: any) => {
    const sync = syncOf(r)
    return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-6 expand-enter">
      <div>
        <div className="text-xs font-semibold text-gray-600 mb-2">저장소 정보</div>
        <div className="space-y-1 text-xs text-gray-500">
          <div>ID: <span className="font-mono">{r.id}</span> <button className="text-gray-400 hover:text-blue-600" onClick={() => copyText(r.id)}>⧉</button></div>
          <div>슬러그: {r.slug || '-'}</div>
          <div>Clone URL: {r.clone_url || '-'}</div>
          <div>기본 브랜치: <span className="font-mono">{r.default_branch || 'main'}</span></div>
          <div>동기화: {sync.phaseLabel}{r.last_commit_at && ` · 커밋 ${formatShortTime(r.last_commit_at)}`}</div>
          <div>마지막 성공: {formatShortTime(sync.lastSuccessAt)}</div>
          {sync.sourceRevision && <div>소스 리비전: <span className="font-mono">{sync.sourceRevision.slice(0, 10)}</span></div>}
          {sync.lastError && <div className="text-red-500">최근 실패: {sync.lastError}</div>}
          <div>생성일: {r.created_at?.slice(0, 10)}</div>
        </div>
      </div>
      <div>
        <div className="text-xs font-semibold text-gray-600 mb-2">프로젝트</div>
        {r.project_id ? <Link to={`/projects/${r.project_id}`} className="btn-link">프로젝트 상세 →</Link> : <span className="text-xs text-gray-400">연결된 프로젝트 없음</span>}
        <div className="text-xs font-semibold text-gray-600 mb-2 mt-3">거버넌스</div>
        <div className="space-y-2">
          <button onClick={() => openBranchProtection(r)} className="text-xs text-yellow-600 hover:underline block">🌿 브랜치 보호 설정</button>
          <Link to={`/repositories/${r.id}`} className="text-xs text-blue-600 hover:underline block">📂 파일 브라우저 (상세) →</Link>
          <Link to="/explorer" className="text-xs text-blue-600 hover:underline block">🔬 프로바이던스 탐색 →</Link>
        </div>
      </div>
      <div>
        <div className="text-xs font-semibold text-gray-600 mb-2">보안</div>
        <div className="space-y-1 text-xs text-gray-500">
          <div>민감도: {r.sensitivity || 'internal'} — 열지도/접근 게이트에 반영 (§33.5)</div>
          <div>보호 프로필: {r.sensitivity === 'restricted' || r.sensitivity === 'confidential' ? 'P2' : r.sensitivity === 'internal' ? 'P1' : 'P0'}</div>
        </div>
        <Link to="/security" className="text-xs text-blue-600 hover:underline block mt-2">🛡 보안 발견 보기 →</Link>
      </div>
    </div>
    )
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6 flex-wrap gap-2">
        <div>
          <h1 className="text-2xl font-bold">저장소 <span className="text-gray-400 text-lg font-normal">Repositories</span></h1>
          <p className="text-xs text-gray-400 mt-1">Git 저장소 관리 · 동기화 · 파일 브라우저 · 브랜치 보호 · 웹훅 · PRD §18</p>
        </div>
        <div className="flex gap-2 shrink-0 flex-wrap">
          <button onClick={() => { setEditingId(null); resetForm(); setShowForm(!showForm) }} className="btn-primary">{showForm ? '취소' : '+ 저장소 추가'}</button>
          <button onClick={() => exportCSV(`repos_${new Date().toISOString().slice(0,10)}.csv`, ['저장소명', 'SCM', 'Clone URL', '기본 브랜치', '민감도', '상태', '동기화'], table.rows.map(r => [r.name, r.scm_provider, r.clone_url, r.default_branch, r.sensitivity, r.status, r.sync_status]))} className="btn-sm btn-secondary">📥 CSV</button>
        </div>
      </div>

      {showForm && (
        <form onSubmit={editingId ? handleUpdate : handleCreate} className="card mb-6 space-y-4 expand-enter">
          <h2 className="text-sm font-semibold">{editingId ? '저장소 수정' : '새 저장소'}</h2>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div><label className="label">저장소명 · Name</label><input className="input" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="backend-api" required /></div>
            <div><label className="label">슬러그 · Slug</label><input className="input" value={form.slug} onChange={e => setForm({ ...form, slug: e.target.value })} placeholder="backend-api" disabled={!!editingId} /></div>
            <div><label className="label">프로젝트 · Project</label><EntitySelect entity="project" value={form.project_id} onChange={v => setForm({ ...form, project_id: v })} /></div>
            <div><label className="label">SCM 커넥터 · Provider</label><EntitySelect entity="scm_connector" value={form.scm_provider} onChange={v => setForm({ ...form, scm_provider: v })} noneLabel="Git (기본)" /></div>
            <div><label className="label">Clone URL</label><input className="input font-mono text-xs" value={form.clone_url} onChange={e => setForm({ ...form, clone_url: e.target.value })} placeholder="https://github.com/org/repo.git" /></div>
            <div><label className="label">기본 브랜치</label><input className="input" value={form.default_branch} onChange={e => setForm({ ...form, default_branch: e.target.value })} placeholder="main" /></div>
            <div className="md:col-span-3"><label className="label">민감도 · Sensitivity (§33.5 열지도/접근)</label>
              <select className="input" value={form.sensitivity || 'internal'} onChange={e => setForm({ ...form, sensitivity: e.target.value })}>
                <option value="public">공개 · Public</option>
                <option value="internal">내부 · Internal</option>
                <option value="confidential">기밀 · Confidential</option>
                <option value="restricted">제한 · Restricted</option>
              </select>
            </div>
          </div>
          <button type="submit" className="btn-primary">{editingId ? '수정 저장' : '생성'}</button>
        </form>
      )}

      {/* Server-side filter bar (C4) */}
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <input className="input flex-1 min-w-[200px]" placeholder="저장소명, URL 검색..." value={table.search} onChange={e => table.setSearch(e.target.value)} />
        <select className="input max-w-[150px] text-xs" value={table.filters.sensitivity || ''} onChange={e => table.setFilter('sensitivity', e.target.value)}>
          <option value="">민감도: 전체</option>
          <option value="public">공개</option><option value="internal">내부</option><option value="confidential">기밀</option><option value="restricted">제한</option>
        </select>
        <select className="input max-w-[140px] text-xs" value={table.filters.status || ''} onChange={e => table.setFilter('status', e.target.value)}>
          <option value="">상태: 전체</option>
          <option value="active">활성</option><option value="unregistered">해제됨</option>
        </select>
        {selectedRepos.size > 0 && (
          <button
            onClick={async () => {
              if (!await confirm({ title: '일괄 등록 해제', message: `${selectedRepos.size}개 저장소를 등록 해제하시겠습니까?`, danger: true })) return
              let ok = 0
              for (const id of selectedRepos) { try { await api.deleteRepository(id); ok++ } catch { showToast('실패했습니다 · action failed', 'error') } }
              showToast(`${ok}/${selectedRepos.size} 해제됨`, ok === selectedRepos.size ? 'success' : 'error')
              setSelectedRepos(new Set()); table.reload()
            }}
            className="btn-sm btn-danger"
          >
            일괄 해제 ({selectedRepos.size})
          </button>
        )}
      </div>

      <div className="card !p-0">
        {table.loading && table.rows.length === 0 ? (
          <div className="p-8 space-y-3 animate-pulse"><div className="h-4 bg-gray-100 rounded w-3/4" /><div className="h-4 bg-gray-100 rounded w-1/2" /></div>
        ) : table.rows.length === 0 ? (
          <EmptyState icon="📦" title="첫 저장소를 연결하세요" message="SCM 커넥터로 저장소를 등록하면 동기화/파일 브라우저가 활성화됩니다" action={{ label: '+ 저장소 추가', onClick: () => setShowForm(true) }} />
        ) : (
          <ResponsiveTable columns={columns} rows={rows} rowKey={(r) => r.id} expand={(r) => expandedId === r.id ? expanded(r) : null} />
        )}
        <div className="flex items-center justify-between px-4 py-3 text-xs text-gray-500 border-t border-gray-100">
          <span>{(table.page - 1) * PAGE_SIZE + 1}-{Math.min(table.page * PAGE_SIZE, table.total)} / {table.total}건</span>
          <div className="flex gap-1">
            <button onClick={() => table.setPage(table.page - 1)} disabled={table.page === 1} className="btn-sm btn-secondary">이전</button>
            <span className="px-2 py-1">{table.page} / {Math.max(Math.ceil(table.total / PAGE_SIZE), 1)}</span>
            <button onClick={() => table.setPage(table.page + 1)} disabled={table.page * PAGE_SIZE >= table.total} className="btn-sm btn-secondary">다음</button>
          </div>
        </div>
      </div>

      {/* Branch protection modal (A4) */}
      <Modal open={!!bpRepo} title="🌿 브랜치 보호 · Branch Protection" subtitle={bpRepo?.name} onClose={() => setBpRepo(null)} size="sm"
        footer={<ModalFooter onCancel={() => setBpRepo(null)} onConfirm={submitBranchProtection} confirmLabel="설정" />}>
        <div className="space-y-3">
          <div>
            <label className="label">브랜치 · Branch</label>
            <input className="input font-mono" value={bpBranch} onChange={e => setBpBranch(e.target.value)} placeholder="main" />
          </div>
          {[
            { value: 'standard', label: '표준 · Standard', desc: '기본 보호 규칙' },
            { value: 'protected', label: '보호됨 · Protected', desc: '직접 푸시 금지, PR 필수' },
            { value: 'release', label: '릴리스 · Release', desc: '승인 필수, 변경 제한' },
            { value: 'production', label: '프로덕션 · Production', desc: '최고 수준 보호 + 승인' },
            { value: 'locked', label: '잠금 · Locked', desc: '모든 변경 금지' },
          ].map(opt => (
            <label key={opt.value} className={`flex items-center gap-3 p-2 rounded-lg cursor-pointer border mb-1 ${bpLevel === opt.value ? 'border-blue-400 bg-blue-50' : 'border-gray-200 hover:bg-gray-50'}`}>
              <input type="radio" name="bpLevel" value={opt.value} checked={bpLevel === opt.value} onChange={e => setBpLevel(e.target.value)} />
              <div>
                <div className="text-sm font-medium">{opt.label}</div>
                <div className="text-xs text-gray-400">{opt.desc}</div>
              </div>
            </label>
          ))}
        </div>
      </Modal>

      {/* Webhook config modal (UX13) */}
      <Modal open={!!webhookRepo} title="웹훅 설정 · Webhook" subtitle={webhookRepo?.name} onClose={() => setWebhookRepo(null)} size="lg"
        footer={<ModalFooter onCancel={() => setWebhookRepo(null)} onConfirm={async () => { const res: any = await api.rotateWebhookSecret(webhookRepo.id); setWebhookInfo(res); showToast('시크릿 교체됨', 'info') }} confirmLabel="시크릿 교체" />}>
        {webhookInfo ? (
          <div className="space-y-3">
            <p className="text-xs text-gray-500">SCM 시스템의 webhook에 아래 URL을 등록하고, 시크릿으로 HMAC 서명(X-PCCP-Signature = hex(sha256(secret, body)))을 설정하세요.</p>
            <div>
              <label className="label">Webhook URL</label>
              <div className="flex gap-2 shrink-0 flex-wrap"><code className="flex-1 text-xs bg-gray-50 border border-gray-200 rounded p-2 break-all">{webhookInfo.url}</code><button className="btn-sm btn-secondary flex-shrink-0" onClick={() => copyText(webhookInfo.url)}>⧉</button></div>
            </div>
            <div>
              <label className="label">시크릿 · Secret</label>
              <div className="flex gap-2 shrink-0 flex-wrap"><code className="flex-1 text-xs bg-gray-50 border border-gray-200 rounded p-2 break-all font-mono">{webhookInfo.secret}</code><button className="btn-sm btn-secondary flex-shrink-0" onClick={() => copyText(webhookInfo.secret)}>⧉</button></div>
            </div>
          </div>
        ) : <p className="text-xs text-gray-400">로딩 중...</p>}
      </Modal>

      {/* Baseline modal (B1) */}
      <Modal open={!!baselineRepo} title="베이스라인 기록 · Record Baseline" subtitle={`${baselineRepo?.name} · §18.3 불변 태스크 기준`} onClose={() => setBaselineRepo(null)} size="sm"
        footer={<ModalFooter onCancel={() => setBaselineRepo(null)} onConfirm={submitBaseline} confirmLabel="기록" disabled={!baselineForm.commit_sha} />}>
        <div className="space-y-3">
          <div><label className="label">브랜치</label><input className="input font-mono" value={baselineForm.branch} onChange={e => setBaselineForm({ ...baselineForm, branch: e.target.value })} /></div>
          <div><label className="label">커밋 SHA (필수)</label><input className="input font-mono" value={baselineForm.commit_sha} onChange={e => setBaselineForm({ ...baselineForm, commit_sha: e.target.value })} placeholder="abc123..." /></div>
          <div><label className="label">커밋 메시지</label><input className="input" value={baselineForm.commit_message} onChange={e => setBaselineForm({ ...baselineForm, commit_message: e.target.value })} /></div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="label">작성자</label><input className="input" value={baselineForm.author_name} onChange={e => setBaselineForm({ ...baselineForm, author_name: e.target.value })} /></div>
            <div><label className="label">이메일</label><input className="input" value={baselineForm.author_email} onChange={e => setBaselineForm({ ...baselineForm, author_email: e.target.value })} /></div>
          </div>
        </div>
      </Modal>
    </div>
  )
}

function authHeaders(): Record<string, string> { const token = sessionStorage.getItem('pccp_token'); return token ? { Authorization: `Bearer ${token}` } : {} }
