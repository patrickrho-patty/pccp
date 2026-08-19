import test from 'node:test'
import assert from 'node:assert/strict'

import {
  diffAllowlist, assessAllowlistImpact, effectiveAllowlist,
  summarizeRisk, assessGateChange, isStaleBase,
} from './toolGovernance.ts'

const TOOLS = [
  { name: 'file.read', danger_level: 'low', requires_approval: false, status: 'active' },
  { name: 'shell.write', danger_level: 'high', requires_approval: true, status: 'active' },
  { name: 'http.request', danger_level: 'critical', requires_approval: true, status: 'active' },
  { name: 'git.commit', danger_level: 'medium', requires_approval: false, status: 'active' },
  { name: 'old.tool', danger_level: 'medium', requires_approval: false, status: 'retired' },
]

test('diffAllowlist splits added/removed/kept deterministically', () => {
  assert.deepEqual(diffAllowlist(['b', 'a'], ['a', 'c']), { added: ['c'], removed: ['b'], kept: ['a'] })
  assert.deepEqual(diffAllowlist([], []), { added: [], removed: [], kept: [] })
  assert.deepEqual(diffAllowlist(['a'], ['a']), { added: [], removed: [], kept: ['a'] })
})

test('adding a high/critical tool weakens protection and must be highlighted', () => {
  const impact = assessAllowlistImpact(['file.read'], ['file.read', 'shell.write', 'http.request'], TOOLS)
  assert.equal(impact.hasChanges, true)
  assert.deepEqual(impact.addedHighRisk, ['http.request', 'shell.write'])
  assert.equal(impact.weakening, true)
})

test('removing a gated tool tightens the list but is still surfaced', () => {
  const impact = assessAllowlistImpact(['shell.write', 'file.read'], ['file.read'], TOOLS)
  assert.deepEqual(impact.removedGated, ['shell.write'])
  assert.equal(impact.weakening, false)
  assert.equal(impact.hasChanges, true)
})

test('clearing a set allowlist reverts to allow-all and counts as weakening', () => {
  const impact = assessAllowlistImpact(['file.read'], [], TOOLS)
  assert.equal(impact.becomesUnset, true)
  assert.equal(impact.weakening, true)
  // Empty-from-empty is not a change at all.
  assert.equal(assessAllowlistImpact([], [], TOOLS).hasChanges, false)
})

test('unknown/retired-registry-missing names on the list are flagged', () => {
  const impact = assessAllowlistImpact(['ghost.tool'], ['ghost.tool', 'file.read'], TOOLS)
  assert.deepEqual(impact.unknown, ['ghost.tool'])
})

test('effective allowlist distinguishes unset (allow-all) from restricted', () => {
  const unset = effectiveAllowlist([], TOOLS)
  assert.equal(unset.mode, 'unset')
  assert.deepEqual(unset.allowed, ['file.read', 'git.commit', 'http.request', 'shell.write']) // retired 제외
  const restricted = effectiveAllowlist(['file.read', 'ghost.tool'], TOOLS)
  assert.equal(restricted.mode, 'restricted')
  assert.deepEqual(restricted.allowed, ['file.read', 'ghost.tool'])
  assert.deepEqual(restricted.unknown, ['ghost.tool'])
})

test('risk summary counts proposed selection by danger level', () => {
  assert.deepEqual(summarizeRisk(['file.read', 'shell.write', 'http.request', 'ghost'], TOOLS),
    { low: 1, high: 1, critical: 1, unknown: 1 })
})

test('gate change: removing a gate weakens; high/critical removal needs confirmation', () => {
  assert.deepEqual(assessGateChange(TOOLS[0], true), { from: false, to: true, weakening: false, highRisk: false })
  assert.deepEqual(assessGateChange(TOOLS[2], false), { from: true, to: false, weakening: true, highRisk: true })
  assert.deepEqual(assessGateChange({ name: 'x', danger_level: 'low', requires_approval: true }, false),
    { from: true, to: false, weakening: true, highRisk: false })
  assert.deepEqual(assessGateChange(TOOLS[1], true), { from: true, to: true, weakening: false, highRisk: false })
})

test('stale base detection catches concurrent edits regardless of order', () => {
  assert.equal(isStaleBase(['a', 'b'], ['b', 'a']), false)
  assert.equal(isStaleBase(['a'], ['a', 'b']), true)
  assert.equal(isStaleBase(['a', 'b'], ['a']), true)
  assert.equal(isStaleBase(['a'], ['b']), true)
})
