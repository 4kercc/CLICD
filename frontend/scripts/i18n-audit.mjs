/**
 * i18n coverage audit.
 *
 * Extracts every Chinese-bearing string literal from frontend/src and reports
 * the ones that still contain Chinese after running through utils/i18n.ts, i.e.
 * strings the English UI would render half-translated or untranslated.
 *
 * Usage: node scripts/i18n-audit.mjs [--json]
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, extname, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')
const dictSrc = readFileSync(join(ROOT, 'utils', 'i18n.ts'), 'utf8')

// --- dictionary: exact map + regex replacements, parsed from source ---
const exactBlock = dictSrc.slice(dictSrc.indexOf('const exact'), dictSrc.indexOf('const replacements'))
const exact = {}
const entryRe = /^\s*'((?:[^'\\]|\\.)*)':\s*'((?:[^'\\]|\\.)*)',/gm
for (const m of exactBlock.matchAll(entryRe)) {
  exact[unescape(m[1])] = unescape(m[2])
}

const replBlock = dictSrc.slice(dictSrc.indexOf('const replacements'), dictSrc.indexOf('export function translateText'))
const replacements = []
const replRe = /\[\s*\/((?:[^/\\\n]|\\.)+)\/([gimsuy]*)\s*,\s*'((?:[^'\\]|\\.)*)'\s*\]/g
for (const m of replBlock.matchAll(replRe)) {
  replacements.push([new RegExp(m[1], m[2]), unescape(m[3])])
}

const sortedExact = Object.entries(exact).sort((a, b) => b[0].length - a[0].length)
const CJK = /[\u3400-\u9fff]/

function unescape(value) {
  return value.replace(/\\'/g, "'").replace(/\\\\/g, '\\')
}

function translate(value) {
  if (!CJK.test(value)) return value
  // Mirror utils/i18n.ts translateText: a whole-string dictionary hit wins
  // before any regex/substring pass, so partial rewrites cannot break it.
  const body = value.trim()
  if (exact[body]) return exact[body]
  let out = body
  for (const [re, rep] of replacements) out = out.replace(re, rep)
  for (const [src, dst] of sortedExact) out = out.split(src).join(dst)
  return out.replace(/\s{2,}/g, ' ')
}

// --- collect Chinese-bearing literals from every source file ---
const files = []
;(function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) { walk(p); continue }
    if (!['.ts', '.tsx'].includes(extname(p))) continue
    if (p.endsWith(join('utils', 'i18n.ts')) || p.endsWith('i18n-audit.mjs')) continue
    files.push(p)
  }
})(ROOT)

const literals = new Map()
const patterns = [
  /'((?:[^'\\\n]|\\.)*[\u3400-\u9fff](?:[^'\\\n]|\\.)*)'/g,
  /"((?:[^"\\\n]|\\.)*[\u3400-\u9fff](?:[^"\\\n]|\\.)*)"/g,
  /`((?:[^`\\]|\\[\s\S])*[\u3400-\u9fff](?:[^`\\]|\\[\s\S])*)`/g,
  />([^<>{}\n]*[\u3400-\u9fff][^<>{}\n]*)</g,
]
for (const file of files) {
  const src = readFileSync(file, 'utf8')
  for (const re of patterns) {
    for (const m of src.matchAll(re)) {
      let raw = m[1]
      if (/[${}]/.test(raw)) raw = raw.replace(/\$\{[^}]*\}/g, '')
      const text = unescape(raw).replace(/\\n/g, ' ').replace(/\\t/g, ' ').replace(/\s+/g, ' ').trim()
      if (!text || !CJK.test(text) || text.length > 120) continue
      if (/^[`'"\\]/.test(text)) continue
      if (!literals.has(text)) literals.set(text, new Set())
      literals.get(text).add(file.slice(file.indexOf('src')).replace(/\\/g, '/'))
    }
  }
}

const leftovers = []
for (const [text, where] of literals) {
  const out = translate(text)
  if (CJK.test(out)) leftovers.push({ text, translated: out, files: [...where].sort() })
}
leftovers.sort((a, b) => b.files.length - a.files.length || a.text.localeCompare(b.text))

if (process.argv.includes('--json')) {
  console.log(JSON.stringify({ scanned: files.length, unique: literals.size, leftovers }, null, 2))
} else {
  console.log(`scanned ${files.length} files / ${literals.size} unique Chinese literals`)
  console.log(`still Chinese after translation: ${leftovers.length}\n`)
  for (const item of leftovers) {
    console.log(`${item.text}   ==>   ${item.translated}`)
    console.log(`      ${item.files.slice(0, 4).join(', ')}`)
  }
}
