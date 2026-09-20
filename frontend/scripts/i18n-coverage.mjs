/**
 * Checks a list of strings (e.g. captured from the rendered English UI) against
 * the dictionary, reporting which ones would still contain Chinese.
 *
 * Usage: node scripts/i18n-coverage.mjs <file-with-one-string-per-line>
 *        (or pipe strings on stdin)
 */
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')
const src = readFileSync(join(ROOT, 'utils', 'i18n.ts'), 'utf8')

const exactBlock = src.slice(src.indexOf('const exact'), src.indexOf('const replacements'))
const exact = {}
for (const m of exactBlock.matchAll(/^\s*'((?:[^'\\]|\\.)*)':\s*'((?:[^'\\]|\\.)*)',/gm)) {
  exact[m[1].replace(/\\'/g, "'")] = m[2].replace(/\\'/g, "'")
}
const replBlock = src.slice(src.indexOf('const replacements'), src.indexOf('export function translateText'))
const replacements = []
for (const m of replBlock.matchAll(/\[\s*\/((?:[^/\\\n]|\\.)+)\/([gimsuy]*)\s*,\s*'((?:[^'\\]|\\.)*)'\s*\]/g)) {
  replacements.push([new RegExp(m[1], m[2]), m[3].replace(/\\'/g, "'")])
}
const sortedExact = Object.entries(exact).sort((a, b) => b[0].length - a[0].length)
const CJK = /[\u3400-\u9fff]/

function translateText(value) {
  const body = value.trim()
  if (exact[body]) return exact[body]
  let out = body
  for (const [re, rep] of replacements) out = out.replace(re, rep)
  for (const [s, d] of sortedExact) out = out.split(s).join(d)
  return out.replace(/\s{2,}/g, ' ').trim()
}

const input = process.argv[2] ? readFileSync(process.argv[2], 'utf8') : readFileSync(0, 'utf8')
const lines = input.split('\n').map((l) => l.trim()).filter((l) => l && CJK.test(l))

let failures = 0
for (const line of lines) {
  const out = translateText(line)
  if (CJK.test(out)) {
    failures++
    console.log(`FAIL  ${line}\n   -> ${out}`)
  }
}
console.log(`\n${lines.length - failures}/${lines.length} strings fully translated`)
process.exit(failures === 0 ? 0 : 1)
