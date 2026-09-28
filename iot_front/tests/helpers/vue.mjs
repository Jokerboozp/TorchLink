import { readFileSync } from 'node:fs'

// Execute the component's real setup code with each test's own I/O and lifecycle
// doubles. Vue reactivity is supplied by the caller; no copy of business logic.
export function setupScript(url) {
  const source = readFileSync(url, 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)?.[1]
  if (source === undefined) throw new Error(`Missing <script setup>: ${url}`)
  return source.replace(/^import .*$/gm, '')
}
