# 炬联 TorchLink 管理端

Vue 3 + Vite 前端；完整环境准备见 [技术详情](../docs/TECHNICAL_DETAILS.md)。以下命令在 `iot_front` 目录执行，Node.js 版本要求见 [package.json](package.json)。

```bash
npm ci
npm run dev
```

Windows PowerShell 遇到执行策略限制时改用 `npm.cmd`。开发地址为 `http://localhost:5173`；[vite.config.js](vite.config.js) 将 `/api`、`/health`、`/mcp` 代理到 `http://localhost:8081`，可设置 `VITE_API_PROXY_TARGET` 修改。生产镜像以本目录为构建上下文。

页面通过 `src/api.js` 使用同源接口。界面以 [vue-naive-admin](https://github.com/zclzone/vue-naive-admin) 的后台布局为参考，采用 Naive UI、Tailwind CSS v4 和 Lucide 图标；业务页面的表单、表格、弹窗、选择器和反馈控件均由 Naive UI 绘制。`src/ui` 适配原有业务组件接口，统一处理双向绑定、列表选择和弹窗事件。缓存按租户与用户隔离，分页和异步列表约定见 [列表分页](../docs/LIST_PAGINATION.md)。

```bash
npm test
npm run build
```

测试使用 Node test runner；构建检查不等于浏览器交互验收。接口调用不保存管理员密码，协议上传沿用后端源码校验与发布流程。

本地合成数据界面验收：先执行 `npm run build`，再从本目录运行 `node tests/browser/ui-preview.mjs`；另开终端运行 `node tests/browser/naive-pages-check.mjs` 与 `node tests/browser/protocol-actions-check.mjs`。前者覆盖全部 16 个主页面、产品表单与账户菜单，后者覆盖协议版本操作。这些脚本只连接本机夹具，不写真实业务数据。

## 页面与权限

当前16个菜单包括运行总览、协议管理、产品管理、设备管理、接入网关、接入测试、摄像头映射、告警中心、智能巡检、原始报文、告警规则、模型管理、智能助手、知识库、备份中心和用户与权限。菜单分组可分别折叠，整个侧栏的收起状态保存在本机。登录页适配桌面和窄屏，用户下拉菜单提供退出登录。

设备管理使用独立设备、主设备、子设备三个标签，并支持设备类型筛选；实时数据到达时显示提示，手动刷新列表。协议生成使用字段映射输入表单；列表固定操作按钮并按能力禁用，版本显示与操作分开，协议条目支持分页。

`src/permissions.js` 根据服务端有效权限控制菜单和按钮，实际访问仍由后端校验。`src/views/AccessView.vue` 展示用户所属租户、角色、单独权限及设备范围。普通用户每3秒通过 `src/realtime.js` 读取授权范围内的事件，不签发浏览器 MQTT 凭据；初始快照不重播历史告警。内置管理员继续使用 MQTT。完整规则见 [用户权限](../docs/USER_ACCESS_CONTROL.md)。

## 浏览器验证

从仓库根目录按需执行以下脚本，需先启动前端5173和后端8081，并按脚本提供本机管理员环境配置。用户管理与设备范围脚本会创建并清理临时账户；设备范围验证需有可选的设备及告警数据。

```bash
node iot_front/tests/browser/access-management-check.mjs
node iot_front/tests/browser/shell-management-check.mjs
node iot_front/tests/browser/device-scope-check.mjs
```

界面分页和弹窗的浏览器内样本与真实数据库验证范围分别记录在 [权限与界面验收](../docs/testing/2026-09-14-权限与界面验收.md)。更多文档见 [文档索引](../docs/README.md)。
