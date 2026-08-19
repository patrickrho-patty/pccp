import { test } from 'node:test'
import assert from 'node:assert/strict'
import { tabNavNextIndex as nav } from './components/a11yLogic.ts'

test('tab arrow navigation wraps and clamps', () => {
  assert.equal(nav('ArrowRight', 0, 3), 1)
  assert.equal(nav('ArrowRight', 2, 3), 0)     // wraps
  assert.equal(nav('ArrowLeft', 0, 3), 2)      // wraps backward
  assert.equal(nav('ArrowLeft', 2, 3), 1)
  assert.equal(nav('Home', 2, 3), 0)
  assert.equal(nav('End', 0, 3), 2)
  assert.equal(nav('Enter', 1, 3), 1)          // non-nav key keeps index
  assert.equal(nav('ArrowRight', 0, 0), 0)     // empty list safe
})
