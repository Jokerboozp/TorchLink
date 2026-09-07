import { defineConfig, loadEnv } from 'vite'
import { existsSync, readdirSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

const elementPlusComponentsDir = fileURLToPath(new URL('./node_modules/element-plus/es/components/', import.meta.url))
const elementPlusStyleDeps = readdirSync(elementPlusComponentsDir, { withFileTypes: true })
  .filter(entry => entry.isDirectory() && existsSync(`${elementPlusComponentsDir}/${entry.name}/style/css.mjs`))
  .map(entry => `element-plus/es/components/${entry.name}/style/css`)

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = env.VITE_API_PROXY_TARGET || 'http://localhost:8081'
  return {
    plugins: [
      vue(),
      tailwindcss(),
      AutoImport({ resolvers: [ElementPlusResolver()] }),
      Components({ resolvers: [ElementPlusResolver({ importStyle: 'css' })] })
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    optimizeDeps: {
      include: [
        'element-plus/es',
        ...elementPlusStyleDeps
      ]
    },
    server: {
      proxy: {
        '/api': apiTarget,
        '/health': apiTarget,
        '/mcp': apiTarget
      }
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      rollupOptions: {
        output: {
          entryFileNames: 'app.js',
          chunkFileNames: 'assets/[name]-[hash].js',
          assetFileNames: 'assets/[name]-[hash][extname]'
        }
      }
    }
  }
})
