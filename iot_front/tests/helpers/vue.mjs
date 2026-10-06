import { readFileSync } from 'node:fs'
import { stripTypeScriptTypes } from 'node:module'

// Shared request helpers a component imports are defined in its context unless
// the test supplies its own double.
const shared = [
  '../../src/latest.js',
  '../../src/composables/useListLoader.js',
  '../../src/composables/useDeviceSearch.js',
  '../../src/clipboard.js',
  '../../src/router/paths.ts',
  '../../src/transfer.ts',
  '../../src/composables/usePagedList.js',
  '../../src/deleteAction.js',
  '../../src/composables/useRealtime.ts',
  '../../src/knowledge.js',
  '../../src/composables/useKnowledgeBinding.js'
].map(file =>
  (file.endsWith('.ts') ? stripTypeScriptTypes : String)(readFileSync(new URL(file, import.meta.url), 'utf8'))
    .replace(/^import\s[^'"]*['"][^'"]+['"];?$/gm, '')
    .replace(/^export (?=async function|function|const)/gm, '')
)
const helpers = {
  latest: shared[0],
  isAbort: shared[0],
  useListLoader: shared.slice(0, 2).join('\n'),
  useDeviceSearch: shared.slice(0, 3).join('\n'),
  copyText: shared[3],
  takeNavigation: shared[4],
  uploadWithProgress: shared[5],
  downloadWithProgress: shared[5],
  transferText: shared[5],
  usePagedList: shared[6],
  confirmed: shared[7],
  useRealtime: shared[8],
  agentKey: shared[9],
  agentName: shared[9],
  categoryLabel: shared[9],
  isIndexing: shared[9],
  retryable: shared[9],
  useKnowledgeBinding: [shared[0], shared[1], shared[9], shared[10]].join('\n')
}

// vm contexts have no AbortController unless a test provides one.
const abortController = `var AbortController = typeof AbortController !== 'undefined' ? AbortController : class {
  signal = { aborted: false, addEventListener() {}, removeEventListener() {} }
  abort() { this.signal.aborted = true }
}
`

function preamble(source) {
  const used = Object.entries(helpers).filter(([name]) => new RegExp(`\\b${name}\\b`).test(source))
  if (!used.length) return ''
  return (
    abortController +
    used
      .map(([name, code]) => `var ${name} = typeof ${name} !== 'undefined' ? ${name} : (() => {\n${code}\nreturn ${name}\n})()\n`)
      .join('')
  )
}

// Execute the component's real setup code with each test's own I/O and lifecycle
// doubles. Vue reactivity is supplied by the caller; no copy of business logic.
export function setupScript(url) {
  const source = readFileSync(url, 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)?.[1]
  if (source === undefined) throw new Error(`Missing <script setup>: ${url}`)
  const code = source.replace(/^import\s[^'"]*['"][^'"]+['"];?$/gm, '')
  return preamble(code) + code
}
