import { defineConfig, loadEnv } from 'vite' /* 引入当前代码需要的依赖。 */
import { fileURLToPath, URL } from 'node:url' /* 引入当前代码需要的依赖。 */
import vue from '@vitejs/plugin-vue' /* 引入当前代码需要的依赖。 */
import tailwindcss from '@tailwindcss/vite' /* 引入当前代码需要的依赖。 */

export default defineConfig(({ mode }) => { /* 执行当前语句并推进处理流程。 */
  const env = loadEnv(mode, process.cwd(), '') /* 声明 env。 */
  const apiTarget = env.VITE_API_PROXY_TARGET || 'http://localhost:8081' /* 声明 apiTarget。 */
  return { /* 返回当前处理结果。 */
    plugins: [ /* 执行当前语句并推进处理流程。 */
      vue(), /* 编译 Vue 页面。 */
      tailwindcss() /* 保留业务页面已有的原子样式。 */
    ], /* 结束当前表达式或代码块。 */
    resolve: { /* 执行当前语句并推进处理流程。 */
      alias: { /* 执行当前语句并推进处理流程。 */
        '@': fileURLToPath(new URL('./src', import.meta.url)) /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
    }, /* 结束当前表达式或代码块。 */
    server: { /* 执行当前语句并推进处理流程。 */
      proxy: { /* 执行当前语句并推进处理流程。 */
        '/api': apiTarget, /* 执行当前语句并推进处理流程。 */
        '/health': apiTarget, /* 执行当前语句并推进处理流程。 */
        '/mcp': apiTarget /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
    }, /* 结束当前表达式或代码块。 */
    build: { /* 执行当前语句并推进处理流程。 */
      outDir: 'dist', /* 执行当前语句并推进处理流程。 */
      emptyOutDir: true, /* 执行当前语句并推进处理流程。 */
      rollupOptions: { /* 执行当前语句并推进处理流程。 */
        output: { /* 执行当前语句并推进处理流程。 */
          entryFileNames: 'app.js', /* 执行当前语句并推进处理流程。 */
          chunkFileNames: 'assets/[name]-[hash].js', /* 执行当前语句并推进处理流程。 */
          assetFileNames: 'assets/[name]-[hash][extname]' /* 执行当前语句并推进处理流程。 */
        } /* 结束当前表达式或代码块。 */
      } /* 结束当前表达式或代码块。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
