// securityLexicon.ts — PAT-1508: validated, versioned security-lexicon editor.
//
// The Security console used to expose the whole PII/detection lexicon as a raw
// JSON textarea ("rule_id → 정규식" map) with no schema validation, regex
// safety, diff, or version discipline. This module is the shared validator /
// compiler used by the structured editor UI (and testable in isolation): it
// defines the canonical rule schema, rejects unsafe/invalid rules, computes a
// safe detection preview for non-sensitive samples, and produces an immutable
// versioned publish with an explicit diff. The raw JSON source editor remains
// only behind an explicit advanced mode.
//
// Lives with the other shared presentation modules (evidenceView, approvalView,
// complianceView, harnessHealth) so the page never hand-rolls regex safety.

export interface LexiconRule {
  id: string
  name_ko?: string
  name_en?: string
  category?: string
  severity?: string
  pattern: string
  flags?: string
  enabled?: boolean
}

export interface LexiconVersion {
  version: string
  created_at: string
  patterns: Record<string, string | { pattern?: string; flags?: string }>
}

export interface RuleValidation {
  ok: boolean
  compiled?: RegExp
  errors: string[]
  warnings: string[]
}

export const LEXICON_RULE_IDS = [
  'kr-rrn', 'kr-brn', 'kr-bank-account', 'kr-rrn-keyword',
  'kr-phone', 'kr-passport', 'kr-drivers-license', 'kr-health-insurance',
  'english-ssn', 'secret', 'api-key', 'aws-access-key',
] as const

export const LEXICON_SEVERITIES = ['info', 'low', 'medium', 'high', 'critical'] as const

const SAFE_FLAG_SET = new Set(['i', 'g', 'm', 'u', 's'])

// ---- Validation ----------------------------------------------------------

/** Build a RegExp from a rule's pattern + optional flags, catching syntax. */
export function compileRule(pattern: string, flags?: string): RegExp | null {
  try {
    return new RegExp(pattern, flags || '')
  } catch {
    return null
  }
}

/** ReDoS guard: reject unbalanced/star-touching quantifiers and pathological
 *  constructs that allow catastrophic backtracking. This is a pragmatic
 *  static safety net, not a full ReDoS solver — the conservative default is to
 *  require the advanced source editor for anything exotic (see REQUIRES_ADVANCED). */
export function regexSafety(pattern: string): { unsafe: boolean; reason?: string } {
  if (!pattern) return { unsafe: true, reason: '패턴이 비어 있습니다' }
  // Unsupported inline constructs force the advanced source editor.
  if (/\(\?[<=!]|\(\?>|\(\?P?[<']/.test(pattern)) {
    return { unsafe: true, reason: 'lookaround/atomic/backreference — 고급 소스 모드 필요' }
  }
  // Catastrophic nested quantifier: a quantified group whose inner content
  // ends in a quantifier, e.g. `(a+)+`, `(x*y*)+` (exponential backtracking).
  if (/\((?:[^()]*[*+])+\)[*+]/.test(pattern)) {
    return { unsafe: true, reason: '중첩된 수량자 — 재귀 폭주 위험' }
  }
  // Group close immediately followed by two quantifier characters, e.g.
  // `(a|b)*+` (possessive/atomic-style, unsupported semantics).
  if (/\)[*+]\s*[*+]/.test(pattern)) {
    return { unsafe: true, reason: '그룹 뒤 연속 수량자 — 재귀 폭주 위험' }
  }
  // Two quantifiers hugging a group close, e.g. `a{0,3}{1,}`.
  if (/(?:\*|\+|\{\d+,\})\s*(?:\*|\+|\{\d+,\})/.test(pattern)) {
    return { unsafe: true, reason: '연속된 수량자 — 재귀 폭주 위험' }
  }
  return { unsafe: false }
}

/** Validate one rule row. Common edits must not require JSON escaping. */
export function validateRule(rule: LexiconRule): RuleValidation {
  const errors: string[] = []
  const warnings: string[] = []
  if (!rule.id || !rule.id.trim()) errors.push('Rule ID가 필요합니다')
  if (!/^[a-z0-9][a-z0-9._-]*$/.test(rule.id || '')) errors.push('Rule ID는 소문자/숫자/._- 만 사용할 수 있습니다')
  if (!rule.pattern || !rule.pattern.trim()) errors.push('패턴(정규식)이 필요합니다')

  const flags = rule.flags || ''
  for (const f of flags.split('')) {
    if (!SAFE_FLAG_SET.has(f)) errors.push(`지원하지 않는 플래그 '${f}'`)
  }
  if (flags.includes('g') && !flags.includes('y')) {
    // global without sticky is fine for scanning; leave as-is (no error)
  }

  const compiled = compileRule(rule.pattern || '', rule.flags)
  if (!compiled) {
    errors.push('유효하지 않은 정규식 구문')
  } else {
    const safety = regexSafety(rule.pattern || '')
    if (safety.unsafe) {
      errors.push(safety.reason || '안전하지 않은 패턴')
    }
    if (rule.pattern && rule.pattern.length < 3) {
      warnings.push('매우 짧은 패턴은 오탐 가능성이 높습니다')
    }
  }
  return { ok: errors.length === 0, compiled: compiled || undefined, errors, warnings }
}

/** Validate a whole lexicon patterns map (the versioned payload shape). */
export function validateLexicon(patterns: Record<string, string | LexiconRule>): RuleValidation {
  const errors: string[] = []
  const warnings: string[] = []
  const seen = new Set<string>()
  const ids = Object.keys(patterns || {})
  if (ids.length === 0) return { ok: false, errors: ['렉시콘이 비어 있습니다'], warnings }
  for (const id of ids) {
    if (seen.has(id)) errors.push(`중복 Rule ID: ${id}`)
    seen.add(id)
    const raw = patterns[id]
    const rule: LexiconRule = typeof raw === 'string' ? { id, pattern: raw } : { id, ...(raw as LexiconRule) }
    const v = validateRule(rule)
    errors.push(...v.errors.map(e => `${id}: ${e}`))
    warnings.push(...v.warnings.map(w => `${id}: ${w}`))
  }
  return { ok: errors.length === 0, errors, warnings }
}

// ---- Detection preview (non-sensitive samples) ---------------------------

export interface DetectionCase { label: string; text: string }

/** Run a compiled rule against non-sensitive samples; returns per-case result. */
export function previewRule(rule: LexiconRule, cases: DetectionCase[]): { input: string; matched: boolean; count: number }[] {
  const v = validateRule(rule)
  if (!v.ok || !v.compiled) {
    return cases.map(c => ({ input: c.text, matched: false, count: 0 }))
  }
  const re = new RegExp(v.compiled.source, (v.compiled.flags.includes('i') ? 'i' : '') + (v.compiled.flags.includes('s') ? 's' : '') + 'gu')
  return cases.map(c => {
    const m = c.text.match(re)
    return { input: c.text, matched: !!m, count: m ? m.length : 0 }
  })
}

export const DEMO_SAMPLES: DetectionCase[] = [
  { label: '주민등록번호', text: '고객의 주민등록번호는 900101-1234567 입니다.' },
  { label: '휴대전화', text: '연락처는 010-1234-5678 로 부탁드립니다.' },
  { label: '이메일/영문 SSN', text: '이메일 a@b.com, SSN 123-45-6789.' },
  { label: '정상 문장', text: '오늘 회의는 3시에 시작합니다.' },
]

// ---- Versioning + diff ----------------------------------------------------

export interface LexiconDiff {
  added: string[]
  removed: string[]
  changed: string[]
  unchanged: string[]
}

export function diffLexicon(before: Record<string, string>, after: Record<string, string>): LexiconDiff {
  const added: string[] = []
  const removed: string[] = []
  const changed: string[] = []
  const unchanged: string[] = []
  const all = new Set([...Object.keys(before || {}), ...Object.keys(after || {})])
  for (const id of all) {
    const b = before[id]
    const a = after[id]
    if (b === undefined) added.push(id)
    else if (a === undefined) removed.push(id)
    else if (b !== a) changed.push(id)
    else unchanged.push(id)
  }
  return { added, removed, changed, unchanged }
}

/** Normalize the textarea-derived payload into the versioned patterns map. */
export function parseLexiconPayload(raw: string): { ok: boolean; patterns: Record<string, string>; errors: string[] } {
  try {
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { ok: false, patterns: {}, errors: ['렉시콘은 rule_id → 패턴 오브젝트여야 합니다'] }
    }
    const patterns: Record<string, string> = {}
    const errors: string[] = []
    for (const [id, val] of Object.entries(parsed)) {
      if (typeof id !== 'string' || !id.trim()) continue
      if (typeof val === 'string') patterns[id] = val
      else if (val && typeof val === 'object' && typeof (val as any).pattern === 'string') patterns[id] = (val as any).pattern
      else errors.push(`${id}: 패턴 값이 문자열이 아닙니다`)
    }
    return { ok: errors.length === 0, patterns, errors }
  } catch {
    return { ok: false, patterns: {}, errors: ['JSON 구문 오류'] }
  }
}
