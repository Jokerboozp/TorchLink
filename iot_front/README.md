# 炬联 TorchLink 管理端

Vue 3 + Vite 前端；完整环境准备见 [技术详情](../docs/TECHNICAL_DETAILS.md)。以下命令在 `iot_front` 目录执行，Node.js 版本要求见 [package.json](package.json)。

```bash
npm ci
npm run dev
```

Windows PowerShell 遇到执行策略限制时改用 `npm.cmd`。开发地址为 `http://localhost:5173`；[vite.config.js](vite.config.js) 将 `/api`、`/health`、`/mcp` 代理到 `http://localhost:8081`，可设置 `VITE_API_PROXY_TARGET` 修改。生产镜像以本目录为构建上下文。

页面通过 `src/api.js` 使用同源接口。界面以 [vue-naive-admin](https://github.com/zclzone/vue-naive-admin) 的后台布局为参考，采用 Naive UI、Tailwind CSS v4 和 Lucide 图标；`src/ui` 统一处理双向绑定、列表选择和弹窗事件。缓存按租户与用户隔离，分页和异步列表约定见 [列表与分页](../docs/TECHNICAL_DETAILS.md#列表与分页)。

```bash
npm test
npm run build
```

测试使用 Node test runner，覆盖列表分页与请求竞态、身份隔离、告警提醒、协议生成与映射、SSE 和 Markdown 安全等行为。构建检查不等于浏览器交互验收。

本地合成数据界面验收：先执行 `npm run build`，再从本目录运行 `node tests/browser/ui-preview.mjs`；另开终端运行 `node tests/browser/naive-pages-check.mjs` 与 `node tests/browser/protocol-actions-check.mjs`。前者覆盖全部 16 个主页面、产品表单与账户菜单，后者覆盖协议版本操作。这些脚本只连接本机夹具，不写真实业务数据。

## 页面与权限

`src/views/` 保存业务页面，`src/components/` 保存共享业务组件；`src/styles.css` 管理基础布局，`src/naive-admin.css` 管理当前组件主题。16 个主菜单覆盖设备接入与管理、告警运维、协议开发、AI、备份及用户权限，菜单名称以 `src/App.vue` 为准。

`src/permissions.js` 根据服务端有效权限控制菜单和按钮，实际访问仍由后端校验。`src/views/AccessView.vue` 展示用户所属租户、角色、单独权限及设备范围。普通用户每3秒通过 `src/realtime.js` 读取授权范围内的事件，不签发浏览器 MQTT 凭据；初始快照不重播历史告警。内置管理员继续使用 MQTT。完整规则见 [用户权限](../docs/USER_ACCESS_CONTROL.md)。

## 浏览器验证

从仓库根目录按需执行以下脚本，需先启动前端5173和后端8081，并按脚本提供本机管理员环境配置。用户管理与设备范围脚本会创建并清理临时账户；设备范围验证需有可选的设备及告警数据。

```bash
node iot_front/tests/browser/access-management-check.mjs
node iot_front/tests/browser/device-scope-check.mjs
```

其他专项浏览器回归保留在 `tests/browser/`，按脚本中的 `IOT_TEST_*` 参数准备浏览器、服务和数据后运行；不包含在 `npm test` 中。历史演示数据专用脚本及仅匹配页面源码文案的断言已移除。更多文档见 [文档索引](../docs/README.md)。
