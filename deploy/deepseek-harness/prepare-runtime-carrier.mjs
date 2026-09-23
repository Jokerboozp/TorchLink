// Mirrors the source repository's deployStaging follow-up in
// scripts/build-exe-for-python-sdk.ts, without packaging a standalone binary.
// The current carrier entry is the unified `dsh` CLI; the former
// dsh-sdk-jsonrpc-demo packaged-bin entry was removed upstream.
import { cp, lstat, mkdir, readFile, readdir, realpath, rm } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { existsSync } from 'node:fs' /* 引入当前代码需要的依赖。 */
import { dirname, join, resolve, sep } from 'node:path' /* 引入当前代码需要的依赖。 */

const harnessRoot = resolve(process.argv[2] ?? '/harness') /* 声明 harnessRoot。 */
const stagingRoot = resolve(process.argv[3] ?? join(harnessRoot, 'runtime-node')) /* 声明 stagingRoot。 */
const sourceNodeModules = join(harnessRoot, 'python', 'sdk-runtime', 'node_modules') /* 声明 sourceNodeModules。 */

if (stagingRoot === harnessRoot || harnessRoot.startsWith(`${stagingRoot}${sep}`)) { /* 判断条件并选择处理分支。 */
  throw new Error(`refusing unsafe runtime carrier root: ${stagingRoot}`) /* 抛出当前错误。 */
} /* 结束当前表达式或代码块。 */

async function copyPackage(source, destination) { /* 定义 copyPackage 函数。 */
  const nestedNodeModules = join(source, 'node_modules') /* 声明 nestedNodeModules。 */
  await mkdir(dirname(destination), { recursive: true }) /* 等待异步操作完成。 */
  await cp(source, destination, { /* 等待异步操作完成。 */
    recursive: true, /* 执行当前语句并推进处理流程。 */
    dereference: true, /* 执行当前语句并推进处理流程。 */
    filter: path => path !== nestedNodeModules && !path.startsWith(`${nestedNodeModules}${sep}`), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

const manifestPath = join(stagingRoot, 'package.json') /* 声明 manifestPath。 */
const manifest = JSON.parse(await readFile(manifestPath, 'utf8')) /* 声明 manifest。 */
const directDependencies = Object.keys(manifest.dependencies ?? {}).sort() /* 声明 directDependencies。 */
const restored = [] /* 声明 restored。 */
for (const dependency of directDependencies) { /* 循环处理当前数据。 */
  const destination = join(stagingRoot, 'node_modules', dependency) /* 声明 destination。 */
  if (existsSync(destination)) continue /* 判断条件并选择处理分支。 */
  const source = join(sourceNodeModules, dependency) /* 声明 source。 */
  if (!existsSync(source)) { /* 判断条件并选择处理分支。 */
    throw new Error(`deployed dependency is absent from the carrier and source closure: ${dependency}`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  await copyPackage(source, destination) /* 等待异步操作完成。 */
  restored.push(dependency) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

async function firstSymlink(directory) { /* 定义 firstSymlink 函数。 */
  for (const entry of await readdir(directory, { withFileTypes: true })) { /* 循环处理当前数据。 */
    const path = join(directory, entry.name) /* 声明 path。 */
    const metadata = await lstat(path) /* 声明 metadata。 */
    if (metadata.isSymbolicLink()) return path /* 判断条件并选择处理分支。 */
    if (metadata.isDirectory()) { /* 判断条件并选择处理分支。 */
      const nested = await firstSymlink(path) /* 声明 nested。 */
      if (nested !== undefined) return nested /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
  return undefined /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

const nodeModules = join(stagingRoot, 'node_modules') /* 声明 nodeModules。 */
let materialized = 0 /* 声明 materialized。 */
let remaining = await firstSymlink(nodeModules) /* 声明 remaining。 */
while (remaining !== undefined) { /* 循环处理当前数据。 */
  const segments = remaining.slice(nodeModules.length + 1).split(sep) /* 声明 segments。 */
  const binIndex = segments.lastIndexOf('.bin') /* 声明 binIndex。 */
  if (binIndex >= 0) { /* 判断条件并选择处理分支。 */
    await rm(join(nodeModules, ...segments.slice(0, binIndex + 1)), { recursive: true, force: true }) /* 等待异步操作完成。 */
  } else { /* 结束当前表达式或代码块。 */
    const source = await realpath(remaining) /* 声明 source。 */
    await rm(remaining, { recursive: true, force: true }) /* 等待异步操作完成。 */
    await copyPackage(source, remaining) /* 等待异步操作完成。 */
    materialized += 1 /* 更新 materialized 的值。 */
  } /* 结束当前表达式或代码块。 */
  remaining = await firstSymlink(nodeModules) /* 更新 remaining 的值。 */
} /* 结束当前表达式或代码块。 */

const missing = directDependencies.filter(dependency => !existsSync(join(nodeModules, dependency))) /* 声明 missing。 */
if (missing.length > 0) throw new Error(`runtime carrier dependencies remain missing: ${missing.join(', ')}`) /* 判断条件并选择处理分支。 */

const entry = join(nodeModules, '@deepseek-ai', 'dsh', 'lib', 'bin.js') /* 声明 entry。 */
if (!existsSync(entry)) throw new Error(`runtime carrier entry is missing: ${entry}`) /* 判断条件并选择处理分支。 */

process.stdout.write( /* 执行当前语句并推进处理流程。 */
  `DeepSeek Harness runtime carrier prepared: ${restored.length} hoists restored, ${materialized} links materialized\n`, /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */
