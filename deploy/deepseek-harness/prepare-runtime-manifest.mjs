// The upstream Python runtime manifest owns the official runtime closure. The
// IoT gateway also needs the source-built JavaScript SDK client, so include it
// in the same deploy carrier instead of copying the development workspace.
import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const manifestPath = resolve(process.argv[2] ?? '/harness/python/sdk-runtime/package.json')
const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
if (manifest === null || typeof manifest !== 'object' || Array.isArray(manifest)) {
  throw new Error(`runtime manifest must be an object: ${manifestPath}`)
}
if (manifest.dependencies === null || typeof manifest.dependencies !== 'object' || Array.isArray(manifest.dependencies)) {
  throw new Error(`runtime manifest dependencies must be an object: ${manifestPath}`)
}

const dependency = '@deepseek-ai/dsh-sdk-client'
const current = manifest.dependencies[dependency]
if (current !== undefined && current !== 'workspace:^') {
  throw new Error(`${dependency} has an unexpected runtime manifest version: ${String(current)}`)
}
manifest.dependencies[dependency] = 'workspace:^'
await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`)
process.stdout.write(`DeepSeek Harness runtime manifest includes ${dependency}\n`)
