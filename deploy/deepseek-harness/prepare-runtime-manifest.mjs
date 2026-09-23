// The upstream Python runtime manifest owns the official runtime closure. The
// IoT gateway also needs the source-built JavaScript SDK client, so include it
// in the same deploy carrier instead of copying the development workspace.
import { readFile, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { resolve } from 'node:path' /* 引入当前代码需要的依赖。 */

const manifestPath = resolve(process.argv[2] ?? '/harness/python/sdk-runtime/package.json') /* 声明 manifestPath。 */
const manifest = JSON.parse(await readFile(manifestPath, 'utf8')) /* 声明 manifest。 */
if (manifest === null || typeof manifest !== 'object' || Array.isArray(manifest)) { /* 判断条件并选择处理分支。 */
  throw new Error(`runtime manifest must be an object: ${manifestPath}`) /* 抛出当前错误。 */
} /* 结束当前表达式或代码块。 */
if (manifest.dependencies === null || typeof manifest.dependencies !== 'object' || Array.isArray(manifest.dependencies)) { /* 判断条件并选择处理分支。 */
  throw new Error(`runtime manifest dependencies must be an object: ${manifestPath}`) /* 抛出当前错误。 */
} /* 结束当前表达式或代码块。 */

const dependency = '@deepseek-ai/dsh-sdk-client' /* 声明 dependency。 */
const current = manifest.dependencies[dependency] /* 声明 current。 */
if (current !== undefined && current !== 'workspace:^') { /* 判断条件并选择处理分支。 */
  throw new Error(`${dependency} has an unexpected runtime manifest version: ${String(current)}`) /* 抛出当前错误。 */
} /* 结束当前表达式或代码块。 */
manifest.dependencies[dependency] = 'workspace:^' /* 更新 manifest.dependencies[dependency] 的值。 */
await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`) /* 等待异步操作完成。 */
process.stdout.write(`DeepSeek Harness runtime manifest includes ${dependency}\n`) /* 执行当前语句并推进处理流程。 */
