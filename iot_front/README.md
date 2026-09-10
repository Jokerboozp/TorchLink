# 炬联 TorchLink 管理端

Vue 3 + Vite 前端；完整环境准备见 [技术详情](../docs/TECHNICAL_DETAILS.md)。以下命令在 `iot_front` 目录执行，Node.js 版本要求见 [package.json](package.json)。

```bash
npm ci
npm run dev
```

Windows PowerShell 遇到执行策略限制时改用 `npm.cmd`。开发地址为 `http://localhost:5173`；[vite.config.js](vite.config.js) 将 `/api`、`/health`、`/mcp` 代理到 `http://localhost:8081`，可设置 `VITE_API_PROXY_TARGET` 修改。生产镜像以本目录为构建上下文。

页面通过 `src/api.js` 使用同源接口。样式使用 Tailwind CSS v4、Lucide 图标及 `src/components/ui` 基础组件，复杂业务控件沿用 Element Plus；复用既有交互和样式。缓存按租户与用户隔离，分页和异步列表约定见 [列表分页](../docs/LIST_PAGINATION.md)。

```bash
npm test
npm run build
```

测试使用 Node test runner；构建检查不等于浏览器交互验收。接口调用不保存管理员密码，协议上传沿用后端源码校验与发布流程。
