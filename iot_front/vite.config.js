import { defineConfig, loadEnv } from 'vite'
import { fileURLToPath, URL } from 'node:url'
import net from 'node:net'
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
        } catch {
          /* API 不可用时拒绝 */
        }
        res.statusCode = 403
        res.end()
      })
    }
  }
}

// 本地调试时 API（尤其是 Go 调试构建）通常比 Vite 晚就绪：接口请求先等待 API 端口可连接，
// 避免启动阶段页面报错、终端刷出代理错误；等待超时后照常转发，由代理报告真实故障。
function apiStartupGate(apiTarget, timeoutMs = 90000) {
  const { hostname, port, protocol } = new URL(apiTarget)
  const address = { host: hostname, port: Number(port) || (protocol === 'https:' ? 443 : 80) }
  let state = 'unknown' // unknown：需要等待；ready：已连通；down：等待超时，直接转发
  let waiting = null
  const reachable = () =>
    new Promise(resolve => {
      const socket = net.connect(address)
      const done = ok => {
        socket.destroy()
        resolve(ok)
      }
      socket.setTimeout(1000, () => done(false))
      socket.once('connect', () => done(true))
      socket.once('error', () => done(false))
    })
  const waitReady = async logger => {
    const deadline = Date.now() + timeoutMs
    let announced = false
    while (!(await reachable())) {
      if (Date.now() >= deadline) {
        logger.warn(`API 在 ${timeoutMs / 1000} 秒内未就绪：${apiTarget}`, { timestamp: true })
        return 'down'
      }
      if (!announced) logger.info(`等待 API 就绪：${apiTarget}`, { timestamp: true })
      announced = true
      await new Promise(resolve => setTimeout(resolve, 500))
    }
    if (announced) logger.info('API 已就绪', { timestamp: true })
    return 'ready'
  }
  return {
    configure(proxy) {
      proxy.on('proxyRes', () => {
        state = 'ready'
      })
      proxy.on('error', () => {
        if (state === 'ready') state = 'unknown'
      }) // API 重启后重新等待
    },
    plugin: {
      name: 'torchlink-api-startup-gate',
      configureServer(server) {
        server.middlewares.use(async (req, res, next) => {
          if (state !== 'unknown' || !/^\/(api|health|mcp)(\/|\?|$)/.test(req.url || '')) return next()
          waiting ??= waitReady(server.config.logger)
            .then(result => {
              state = result
            })
            .finally(() => {
              waiting = null
            })
          await waiting
          next()
        })
      }
    }
  }
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = env.VITE_API_PROXY_TARGET || 'http://localhost:8081'
  const mediaTarget = env.VITE_MEDIA_PROXY_TARGET || 'http://127.0.0.1:18580'
  const apiGate = apiStartupGate(apiTarget)
  const apiProxy = { target: apiTarget, changeOrigin: true, configure: apiGate.configure }
  return {
    plugins: [
      vue() /* 编译 Vue 页面。 */,
      tailwindcss() /* 保留业务页面已有的原子样式。 */,
      mediaAuth(apiTarget) /* 本地开发时校验直播 HLS 播放凭证。 */,
      apiGate.plugin /* 本地开发时等待 API 启动完成再转发接口请求。 */
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    server: {
      proxy: {
        '/api': apiProxy,
        '/health': apiProxy,
        '/mcp': apiProxy,
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
