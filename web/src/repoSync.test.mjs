import test from 'node:test'
import assert from 'node:assert/strict'

import { resolveRepoSync, classifySyncError, treeViewState, SYNC_PHASE_LABELS, ATTEMPT_RESULT_LABELS } from './repoSync.ts'

const NOW = Date.parse('2026-08-18T12:00:00Z')
const fresh = '2026-08-18T10:00:00Z'
const old = '2026-08-16T10:00:00Z'

test('never synced: no sync row fields at all', () => {
  const s = resolveRepoSync({}, { now: NOW })
  assert.equal(s.phase, 'never')
  assert.equal(s.phaseLabel, '미동기화')
  assert.equal(s.lastSuccessAt, null)
  assert.equal(s.lastAttemptAt, null)
  assert.equal(s.lastAttemptResult, null)
  assert.equal(s.sourceRevision, null)
})

test('running and queued phases come straight from sync_status', () => {
  assert.equal(resolveRepoSync({ sync_status: 'syncing' }, { now: NOW }).phase, 'running')
  assert.equal(resolveRepoSync({ sync_status: 'syncing' }, { now: NOW }).lastAttemptResult, 'running')
  assert.equal(resolveRepoSync({ sync_status: 'queued' }, { now: NOW }).phase, 'queued')
  assert.equal(resolveRepoSync({ sync_status: 'queued' }, { now: NOW }).lastAttemptResult, 'queued')
})

test('current: fresh success with source revision', () => {
  const s = resolveRepoSync(
    { sync_status: 'synced', last_sync_at: fresh, last_commit_at: '2026-08-18T09:00:00Z', last_sync_head: 'abc123def' },
    { now: NOW },
  )
  assert.equal(s.phase, 'current')
  assert.equal(s.lastSuccessAt, fresh)
  assert.equal(s.lastAttemptResult, 'success')
  assert.equal(s.sourceRevision, 'abc123def')
})

test('stale: success older than the freshness window', () => {
  const s = resolveRepoSync(
    { sync_status: 'synced', last_sync_at: old, last_commit_at: '2026-08-16T09:00:00Z' },
    { now: NOW },
  )
  assert.equal(s.phase, 'stale')
  assert.equal(s.phaseLabel, '오래된 스냅샷')
})

test('current: empty repository — fresh sync with no commits (no last_commit_at)', () => {
  const s = resolveRepoSync({ sync_status: 'synced', last_sync_at: fresh }, { now: NOW })
  assert.equal(s.phase, 'current')
})

test('stale: synced but the success timestamp itself is missing', () => {
  const s = resolveRepoSync({ sync_status: 'synced', last_commit_at: '2026-08-18T09:00:00Z' }, { now: NOW })
  assert.equal(s.phase, 'stale')
})

test('failed: exposes latest attempt result and error', () => {
  const s = resolveRepoSync(
    { sync_status: 'failed', last_sync_at: old, last_sync_attempt_at: fresh, last_sync_error: 'gitscm: clone x: authentication failed' },
    { now: NOW },
  )
  assert.equal(s.phase, 'failed')
  assert.equal(s.lastAttemptAt, fresh)
  assert.equal(s.lastSuccessAt, old)
  assert.equal(s.lastAttemptResult, 'failed')
  assert.equal(s.lastError, 'gitscm: clone x: authentication failed')
})

test('partial: some datasets unavailable despite successful sync', () => {
  const repo = { sync_status: 'synced', last_sync_at: fresh, last_commit_at: '2026-08-18T09:00:00Z' }
  const datasets = [
    { key: 'files', label: '파일 스냅샷', available: false, reason: '스냅샷 없음' },
    { key: 'branches', label: '브랜치', available: true },
    { key: 'baselines', label: '베이스라인', available: true },
  ]
  const s = resolveRepoSync(repo, { now: NOW, datasets })
  assert.equal(s.phase, 'partial')
  assert.equal(s.phaseLabel, '부분 동기화')
  assert.deepEqual(s.datasets.filter(d => !d.available).map(d => d.key), ['files'])
})

test('partial applies to stale snapshots too', () => {
  const repo = { sync_status: 'synced', last_sync_at: old, last_commit_at: '2026-08-16T09:00:00Z' }
  const s = resolveRepoSync(repo, { now: NOW, datasets: [{ key: 'files', label: '파일', available: false }] })
  assert.equal(s.phase, 'partial')
})

test('all datasets available keeps the base phase', () => {
  const repo = { sync_status: 'synced', last_sync_at: fresh, last_commit_at: '2026-08-18T09:00:00Z' }
  const s = resolveRepoSync(repo, { now: NOW, datasets: [{ key: 'files', label: '파일', available: true }] })
  assert.equal(s.phase, 'current')
})

test('failed and running are never downgraded to partial', () => {
  const datasets = [{ key: 'files', label: '파일', available: false }]
  assert.equal(resolveRepoSync({ sync_status: 'failed' }, { now: NOW, datasets }).phase, 'failed')
  assert.equal(resolveRepoSync({ sync_status: 'syncing' }, { now: NOW, datasets }).phase, 'running')
})

test('phase labels cover every canonical phase', () => {
  for (const phase of ['never', 'queued', 'running', 'current', 'stale', 'partial', 'failed']) {
    assert.ok(SYNC_PHASE_LABELS[phase], phase)
  }
})

test('attempt result labels cover every result and match phase wording', () => {
  for (const result of ['success', 'failed', 'running', 'queued']) {
    assert.ok(ATTEMPT_RESULT_LABELS[result], result)
  }
  // running/queued reuse the phase labels so the detail page wording can
  // never drift from the sync status card (R7).
  assert.equal(ATTEMPT_RESULT_LABELS.running, SYNC_PHASE_LABELS.running)
  assert.equal(ATTEMPT_RESULT_LABELS.queued, SYNC_PHASE_LABELS.queued)
})

test('classifySyncError covers credential failure, rate limit, deleted branch, unsupported SCM', () => {
  assert.equal(classifySyncError('gitscm: repository not synced yet — run sync first').kind, 'not_synced')
  assert.equal(classifySyncError('gitscm: sync already in progress').kind, 'in_progress')
  assert.equal(classifySyncError('gitscm: clone x: authentication failed').kind, 'auth')
  assert.equal(classifySyncError('gitscm: clone x: 403 API rate limit exceeded').kind, 'rate_limit')
  assert.equal(classifySyncError("gitscm: clone x: couldn't find remote ref main").kind, 'not_found')
  assert.equal(classifySyncError('remote: Repository not found.').kind, 'not_found')
  assert.equal(classifySyncError('feature not supported by this SCM provider').kind, 'unsupported')
  assert.equal(classifySyncError('gitscm: read dir: no such file or directory').kind, 'path_missing')
  assert.equal(classifySyncError(undefined).kind, 'unknown')
  assert.equal(classifySyncError('some other failure').kind, 'unknown')
  assert.ok(classifySyncError('rate limit exceeded').label.includes('레이트 리밋'))
})

test('treeViewState distinguishes loading / empty repo / empty folder / unavailable', () => {
  assert.deepEqual(treeViewState(true, null, null, ''), { state: 'loading' })
  assert.deepEqual(treeViewState(false, [], null, ''), { state: 'empty', scope: 'repository' })
  assert.deepEqual(treeViewState(false, [], null, 'src/refunds'), { state: 'empty', scope: 'folder' })
  assert.deepEqual(treeViewState(false, [{ name: 'a.go' }], null, ''), { state: 'ok' })
  const unavailable = treeViewState(false, null, 'gitscm: repository not synced yet — run sync first', '')
  assert.equal(unavailable.state, 'unavailable')
  assert.equal(unavailable.error.kind, 'not_synced')
  // An error wins over an empty list — zero entries from a failed fetch
  // are not an "empty repository".
  assert.equal(treeViewState(false, [], 'boom', '').state, 'unavailable')
})
