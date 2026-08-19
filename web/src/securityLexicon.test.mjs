import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  validateRule, validateLexicon, previewRule, diffLexicon, parseLexiconPayload,
  regexSafety, LEXICON_RULE_IDS,
} from './securityLexicon.ts'

test('valid Korean PII rule validates and compiles for non-sensitive sample', () => {
  const v = validateRule({ id: 'kr-phone', pattern: '010-?\\d{3,4}-?\\d{4}', severity: 'high' })
  assert.equal(v.ok, true)
  assert.ok(v.compiled instanceof RegExp)
  const results = previewRule({ id: 'kr-phone', pattern: '010-?\\d{3,4}-?\\d{4}' }, [
    { label: 'hit', text: '연락처 010-1234-5678' },
    { label: 'none', text: '안녕하세요' },
  ])
  assert.equal(results[0].matched, true)
  assert.equal(results[1].matched, false)
})

test('invalid regex syntax cannot publish', () => {
  const v = validateRule({ id: 'bad', pattern: '(unclosed' })
  assert.equal(v.ok, false)
  assert.ok(v.errors.some(e => e.includes('정규식')))
})

test('catastrophic/unsafe regex is rejected', () => {
  assert.equal(regexSafety('(a+)+$').unsafe, true)  // nested quantifier (exponential)
  assert.equal(regexSafety('(a|b)*+').unsafe, true) // group + possessive-style
  assert.equal(regexSafety('a{0,3}{1,}').unsafe, true) // adjacent quantifiers
  assert.equal(regexSafety('(?=x)y').unsafe, true)  // lookahead requires advanced mode
  assert.equal(regexSafety('hello').unsafe, false)
  assert.equal(regexSafety('a*a').unsafe, false)    // separate quantifiers are safe
})

test('empty rule and bad id are rejected', () => {
  const v = validateRule({ id: '', pattern: 'x' })
  assert.equal(v.ok, false)
  const v2 = validateRule({ id: 'Has Space', pattern: 'x' })
  assert.equal(v2.ok, false)
})

test('duplicate ids and empty lexicon are rejected at the whole map level', () => {
  const dup = validateLexicon({ 'a': 'x+', 'a': 'y+' })
  assert.equal(dup.ok, false)
  const empty = validateLexicon({})
  assert.equal(empty.ok, false)
})

test('diff reports added/removed/changed/unchanged', () => {
  const d = diffLexicon({ a: '1', b: '2', c: '3' }, { a: '1', b: '9', d: '4' })
  assert.deepEqual(d.added, ['d'])
  assert.deepEqual(d.removed, ['c'])
  assert.deepEqual(d.changed, ['b'])
  assert.deepEqual(d.unchanged, ['a'])
})

test('parseLexiconPayload accepts object-form and rejects bad JSON', () => {
  const ok = parseLexiconPayload(JSON.stringify({ 'kr-phone': { pattern: '010-?\\d{3,4}-?\\d{4}', severity: 'high' } }))
  assert.equal(ok.ok, true)
  assert.equal(ok.patterns['kr-phone'], '010-?\\d{3,4}-?\\d{4}')
  assert.equal(parseLexiconPayload('not json').ok, false)
})

test('canonical rule ids are present in the editor vocabulary', () => {
  for (const id of ['kr-rrn', 'kr-phone', 'kr-passport', 'english-ssn', 'secret']) {
    assert.ok(LEXICON_RULE_IDS.includes(id), `missing ${id}`)
  }
})
