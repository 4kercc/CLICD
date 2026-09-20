/**
 * Reports duplicate keys in the `exact` dictionary of utils/i18n.ts.
 * TypeScript rejects duplicate object properties (TS1117), so this is used to
 * keep the map clean after batch additions.
 *
 * Usage: node scripts/i18n-duplicates.mjs [--fix]
 */
import { readFileSync, writeFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const file = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'utils', 'i18n.ts')
const lines = readFileSync(file, 'utf8').split('\n')

const start = lines.findIndex((l) => l.includes('const exact'))
const end = lines.findIndex((l) => l.includes('const replacements'))
const entryRe = /^\s*'((?:[^'\\]|\\.)*)':\s*'((?:[^'\\]|\\.)*)',\s*$/

const first = new Map()
const duplicates = []
for (let i = start; i < end; i++) {
  const m = lines[i].match(entryRe)
  if (!m) continue
  if (first.has(m[1])) {
    duplicates.push({ key: m[1], firstLine: first.get(m[1]), dupLine: i, value: m[2] })
  } else {
    first.set(m[1], i)
  }
}

for (const d of duplicates) {
  console.log(`line ${d.dupLine + 1}: '${d.key}' duplicates line ${d.firstLine + 1}`)
}
console.log(`duplicate keys: ${duplicates.length}`)

if (process.argv.includes('--fix') && duplicates.length) {
  const drop = new Set(duplicates.map((d) => d.dupLine))
  // Duplicates carry a comment header belonging to the removed group; keep the
  // file tidy by dropping a preceding "// ---" comment that would be orphaned.
  for (const line of [...drop].sort((a, b) => b - a)) {
    lines.splice(line, 1)
  }
  writeFileSync(file, lines.join('\n'))
  console.log('removed duplicate lines')
}
