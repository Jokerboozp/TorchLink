import { defineConfig, loadEnv } from 'vite'
import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// 摄像头直播 HLS：与生产 nginx 的 auth_request 一致，每个请求先由 API 校验路径中的播放凭证，
// 通过后才转发到媒体服务；API 不搬运视频数据。
const hlsPath = /^\/media\/hls\/([A-Za-z0-9]{32,128})\/(src|tc)\/([A-Za-z0-9_]{1,64})\/([A-Za-z0-9_./-]+\.(?:m3u8|ts))(?:\?.*)?$/
function mediaAuth(apiTarget) {
  return {
    name: 'torchlink-media-auth',
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        if (!req.url?.startsWith('/media/')) return next()
        try {
          if (hlsPath.test(req.url) && !req.url.includes('..')) {
            const response = await fetch(`${apiTarget}/api/v1/video/media-auth`, { headers: { 'X-Original-URI': req.url } })
            if (response.status === 204) return next()
          }
        } catch { /* API 不可用时拒绝 */ }
        res.statusCode = 403
        res.end()
      })
    }
  }
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = env.VITE_API_PROXY_TARGET || 'http://localhost:8081'
  const mediaTarget = env.VITE_MEDIA_PROXY_TARGET || 'http://127.0.0.1:18580'
  return {
    plugins: [
      vue(), /* 编译 Vue 页面。 */
      tailwindcss(), /* 保留业务页面已有的原子样式。 */
      mediaAuth(apiTarget) /* 本地开发时校验直播 HLS 播放凭证。 */
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    server: {
      proxy: {
        '/api': apiTarget,
        '/health': apiTarget,
        '/mcp': apiTarget,
        '/media/hls': {
          target: mediaTarget,
          rewrite: path => path.replace(hlsPath, '/$2/$3/$4?vt=$1')
        }
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
