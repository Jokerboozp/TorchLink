// Mirrors the source repository's deployStaging follow-up in
// scripts/build-exe-for-python-sdk.ts, without packaging a standalone binary.
// The current carrier entry is the unified `dsh` CLI; the former
// dsh-sdk-jsonrpc-demo packaged-bin entry was removed upstream.
import { cp, lstat, mkdir, readFile, readdir, realpath, rm } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import { dirname, join, resolve, sep } from 'node:path'

const harnessRoot = resolve(process.argv[2] ?? '/harness')
const stagingRoot = resolve(process.argv[3] ?? join(harnessRoot, 'runtime-node'))
const sourceNodeModules = join(harnessRoot, 'python', 'sdk-runtime', 'node_modules')

if (stagingRoot === harnessRoot || harnessRoot.startsWith(`${stagingRoot}${sep}`)) {
  throw new Error(`refusing unsafe runtime carrier root: ${stagingRoot}`)
}

async function copyPackage(source, destination) {
  const nestedNodeModules = join(source, 'node_modules')
  await mkdir(dirname(destination), { recursive: true })
  await cp(source, destination, {
    recursive: true,
    dereference: true,
    filter: path => path !== nestedNodeModules && !path.startsWith(`${nestedNodeModules}${sep}`),
  })
}

const manifestPath = join(stagingRoot, 'package.json')
const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
const directDependencies = Object.keys(manifest.dependencies ?? {}).sort()
const restored = []
for (const dependency of directDependencies) {
  const destination = join(stagingRoot, 'node_modules', dependency)
  if (existsSync(destination)) continue
  const source = join(sourceNodeModules, dependency)
  if (!existsSync(source)) {
    throw new Error(`deployed dependency is absent from the carrier and source closure: ${dependency}`)
  }
  await copyPackage(source, destination)
  restored.push(dependency)
}

async function firstSymlink(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    const metadata = await lstat(path)
    if (metadata.isSymbolicLink()) return path
    if (metadata.isDirectory()) {
      const nested = await firstSymlink(path)
      if (nested !== undefined) return nested
    }
  }
  return undefined
}

const nodeModules = join(stagingRoot, 'node_modules')
let materialized = 0
let remaining = await firstSymlink(nodeModules)
while (remaining !== undefined) {
  const segments = remaining.slice(nodeModules.length + 1).split(sep)
  const binIndex = segments.lastIndexOf('.bin')
  if (binIndex >= 0) {
    await rm(join(nodeModules, ...segments.slice(0, binIndex + 1)), { recursive: true, force: true })
  } else {
    const source = await realpath(remaining)
    await rm(remaining, { recursive: true, force: true })
    await copyPackage(source, remaining)
    materialized += 1
  }
  remaining = await firstSymlink(nodeModules)
}

const missing = directDependencies.filter(dependency => !existsSync(join(nodeModules, dependency)))
if (missing.length > 0) throw new Error(`runtime carrier dependencies remain missing: ${missing.join(', ')}`)

const entry = join(nodeModules, '@deepseek-ai', 'dsh', 'lib', 'bin.js')
if (!existsSync(entry)) throw new Error(`runtime carrier entry is missing: ${entry}`)

process.stdout.write(
  `DeepSeek Harness runtime carrier prepared: ${restored.length} hoists restored, ${materialized} links materialized\n`,
)
