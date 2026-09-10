# 消防 IoT 平台前端

这是独立的 Vue 3 + Vite 前端项目，后端 API 位于仓库根目录的 Go 服务。前端开发不需要进入后端 Go 模块：

```powershell
npm.cmd ci
npm.cmd run dev
```

开发服务器默认运行在 `http://localhost:5173`，`vite.config.js` 会把 `/api`、`/health` 和 `/mcp` 代理到 `http://localhost:8081`。生产镜像由本目录的 `Dockerfile` 构建，根目录 `compose.yaml` 的 `platform-web` 服务也以 `iot_front` 为构建上下文。

界面使用 shadcn-vue 的 CSS 变量和基础组件模式（Tailwind CSS v4 + Lucide），业务复杂控件暂时保留 Element Plus 以维持既有交互契约；新增界面组件放在 `src/components/ui`。

```powershell
npm.cmd test
npm.cmd run build
```

页面通过 `src/api.js` 使用同源 REST/MCP 接口；不要在前端保存管理员密码或跨租户 Token。协议开发页面上传 Go 源码时使用 `FormData` 调用 `/api/v2/protocols/{id}/source-releases`，平台编译并实际验证样例。
