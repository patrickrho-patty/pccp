import test from 'node:test'
import assert from 'node:assert/strict'

import {
  DEFAULT_ALLOWED_MODELS,
  allowedModelDestination,
  allowedModelPolicySummary,
  filterCatalogModels,
  modelPackageState,
  normalizeAllowedModelItems,
} from './allowedModelView.ts'

test('canonical package destinations encode one stable package id', () => {
  assert.equal(allowedModelDestination({ id: '모델/a b', label: '모델', state: 'single', package_id: 'pkg/한국어 a' }), '/models/pkg%2F%ED%95%9C%EA%B5%AD%EC%96%B4%20a')
})

test('catalog and class fallbacks use explicit encoded filters', () => {
  assert.equal(allowedModelDestination({ id: 'catalog/a b', label: '모델', state: 'unavailable', catalog_model_id: 'catalog/a b' }), '/models?catalog=catalog%2Fa%20b')
  assert.equal(allowedModelDestination({ id: 'restricted/a b', label: 'restricted/a b', state: 'restricted', entity_kind: 'model' }), '/models?catalog=restricted%2Fa%20b')
  assert.equal(allowedModelDestination({ id: 'unknown/a b', label: 'unknown/a b', state: 'unknown' }), '/models?class=unknown%2Fa%20b')
})

test('normalization preserves canonical state and rejects malformed item shapes', () => {
  assert.deepEqual(normalizeAllowedModelItems([
    { id: 'catalog-a', label: '카탈로그 A', state: 'retired', entity_kind: 'model', catalog_model_id: 'catalog-a', package_id: 'pkg-a' },
    { id: '', label: 'invalid', state: 'single' },
    'raw-json-is-not-an-item',
  ]), [
    { id: 'catalog-a', label: '카탈로그 A', state: 'retired', entity_kind: 'model', catalog_model_id: 'catalog-a', package_id: 'pkg-a' },
  ])
})

test('restricted model identifiers are not relabeled as model classes', () => {
  assert.deepEqual(normalizeAllowedModelItems([
    { id: 'code', label: 'code', state: 'restricted', entity_kind: 'model' },
  ]), [
    { id: 'code', label: 'code', state: 'restricted', entity_kind: 'model' },
  ])
})

test('new projects retain the restrictive canonical default', () => {
  assert.deepEqual(DEFAULT_ALLOWED_MODELS, ['patty-code-standard'])
})

test('class projections receive the canonical Korean class label', () => {
  assert.deepEqual(normalizeAllowedModelItems([{ id: 'code', label: 'code', state: 'many' }]), [
    { id: 'code', label: '코드 생성', state: 'many' },
  ])
  assert.deepEqual(normalizeAllowedModelItems([{ id: 'code', label: 'code', state: 'single', catalog_model_id: 'code' }]), [
    { id: 'code', label: 'code', state: 'single', catalog_model_id: 'code' },
  ])
})

test('malformed policy is not summarized as unrestricted', () => {
  assert.equal(allowedModelPolicySummary([], 'invalid'), '정책 데이터 확인 필요')
  assert.equal(allowedModelPolicySummary([], 'unrestricted'), '제한 없음 · 모든 모델 허용')
  assert.equal(allowedModelPolicySummary([], 'configured'), '허용 모델 정보를 확인할 수 없음')
})

test('catalog filters match exact ids, family, and nested entitlement class', () => {
  const models = [
    { catalog_model_id: 'catalog-code', family: 'code', entitlement: { class: 'enterprise-code' } },
    { catalog_model_id: 'catalog-chat', family: 'chat', entitlement: { class: 'enterprise-chat' } },
  ]
  assert.deepEqual(filterCatalogModels(models, 'catalog-code', '').map(m => m.catalog_model_id), ['catalog-code'])
  assert.deepEqual(filterCatalogModels(models, '', 'code').map(m => m.catalog_model_id), ['catalog-code'])
  assert.deepEqual(filterCatalogModels(models, '', 'enterprise-chat').map(m => m.catalog_model_id), ['catalog-chat'])
  assert.deepEqual(filterCatalogModels(models, 'missing', '').map(m => m.catalog_model_id), [])
})

test('model package lifecycle reads the registry state field, never a legacy status field', () => {
  assert.equal(modelPackageState({ state: 'published', status: 'recalled' }), 'published')
  assert.equal(modelPackageState({ status: 'published' }), 'unknown')
})

test('large sets use a keyboard-native disclosure and keep every model destination discoverable', async () => {
  const [{ createServer }, React, { renderToStaticMarkup }, { StaticRouter }] = await Promise.all([
    import('vite'),
    import('react'),
    import('react-dom/server'),
    import('react-router-dom/server.js'),
  ])
  const vite = await createServer({ root: new URL('..', import.meta.url).pathname, appType: 'custom', server: { middlewareMode: true } })
  try {
    const { AllowedModelChips } = await vite.ssrLoadModule('/src/allowedModels.tsx')
    const items = Array.from({ length: 250 }, (_, index) => ({ id: `class-${index}`, label: `모델 ${index}`, state: 'unknown' }))
    const html = renderToStaticMarkup(React.createElement(StaticRouter, { location: '/' }, React.createElement(AllowedModelChips, { items, policyState: 'configured' })))
    assert.match(html, /^<div/)
    assert.equal((html.match(/href=/g) || []).length, 250)
    assert.match(html, /<details/)
    assert.match(html, /<summary[^>]*>외 245개<\/summary>/)
    assert.doesNotMatch(html, /tabindex="-1"/)
  } finally {
    await vite.close()
  }
})
