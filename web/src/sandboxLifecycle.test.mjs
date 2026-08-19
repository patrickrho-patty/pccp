import test from 'node:test'
import assert from 'node:assert/strict'

import { sandboxActions, sandboxActionsFor, sandboxStatusMeta, sandboxRuntimeConnected } from './sandboxLifecycle.ts'

const enabled = (sb) => sandboxActions(sb).filter(a => a.enabled).map(a => a.id)

test('action table mirrors the server lifecycle state machine', () => {
  assert.deepEqual(enabled({ status: 'pending' }), ['destroy'])
  assert.deepEqual(enabled({ status: 'defined' }), ['retry', 'destroy'])
  assert.deepEqual(enabled({ status: 'running' }), ['snapshot', 'destroy'])
  assert.deepEqual(enabled({ status: 'paused' }), ['snapshot', 'destroy'])
  assert.deepEqual(enabled({ status: 'destroyed' }), [])
  assert.deepEqual(enabled({ status: 'failed' }), ['retry', 'destroy'])
})

test('runtime-disconnected sandbox cannot be snapshotted but has a recovery path', () => {
  const actions = sandboxActions({ status: 'defined' })
  const snapshot = actions.find(a => a.id === 'snapshot')
  assert.equal(snapshot.enabled, false)
  assert.ok(snapshot.reason?.includes('실행 중'))
  const retry = actions.find(a => a.id === 'retry')
  assert.equal(retry.enabled, true)
})

test('destroyed sandbox admits nothing and every action explains why', () => {
  for (const a of sandboxActions({ status: 'destroyed' })) {
    assert.equal(a.enabled, false)
    assert.ok(a.reason, a.id)
  }
})

test('unknown states fail closed', () => {
  assert.deepEqual(enabled({ status: 'running-ish' }), [])
  assert.deepEqual(enabled({}), [])
  assert.deepEqual(enabled(null), [])
})

test('destroy is the only danger action', () => {
  for (const status of ['pending', 'defined', 'running', 'paused', 'destroyed', 'failed']) {
    for (const a of sandboxActions({ status })) {
      assert.equal(a.danger === true, a.id === 'destroy')
    }
  }
})

test('status meta covers every lifecycle state with fallback', () => {
  for (const status of ['pending', 'defined', 'running', 'paused', 'destroyed', 'failed']) {
    assert.ok(sandboxStatusMeta(status).ko)
    assert.ok(sandboxStatusMeta(status).badge)
  }
  assert.equal(sandboxStatusMeta('weird').ko, 'weird')
  assert.equal(sandboxStatusMeta('').ko, '알 수 없음')
})

test('runtime provider honesty: none (...) means not connected', () => {
  assert.equal(sandboxRuntimeConnected({ runtime_provider: 'docker-api' }), true)
  assert.equal(sandboxRuntimeConnected({ runtime_provider: 'none (no container runtime reachable)' }), false)
  assert.equal(sandboxRuntimeConnected({ runtime_provider: '' }), false)
  assert.equal(sandboxRuntimeConnected({}), false)
})

test('detail page actions follow the server valid_actions, mirror supplies labels/reasons', () => {
  // The server list is authoritative even when it disagrees with the
  // client mirror table.
  const actions = sandboxActionsFor(['destroy'])
  assert.deepEqual(actions.filter(a => a.enabled).map(a => a.id), ['destroy'])
  const snapshot = actions.find(a => a.id === 'snapshot')
  assert.equal(snapshot.enabled, false)
  assert.ok(snapshot.reason, 'disabled action must carry the mirror reason')
  assert.equal(actions.find(a => a.id === 'destroy').danger, true)
  // Empty/missing server list fails closed.
  assert.deepEqual(sandboxActionsFor([]).filter(a => a.enabled), [])
})
