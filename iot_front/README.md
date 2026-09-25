# 炬联 TorchLink 管理端

Vue 3 + Vite 前端；完整环境准备见 [技术详情](../docs/TECHNICAL_DETAILS.md)。以下命令在 `iot_front` 目录执行，Node.js 版本要求见 [package.json](package.json)。

```bash
npm ci
npm run dev
```

Windows PowerShell 遇到执行策略限制时改用 `npm.cmd`。开发地址为 `http://localhost:5173`；[vite.config.js](vite.config.js) 将 `/api`、`/health`、`/mcp` 代理到 `http://localhost:8081`，可设置 `VITE_API_PROXY_TARGET` 修改。生产镜像以本目录为构建上下文。

页面通过 `src/api.js` 使用同源接口。界面采用 Naive UI、Tailwind CSS v4 和 Lucide 图标；`src/ui` 统一处理双向绑定、列表选择和弹窗事件。缓存按租户与用户隔离，分页和异步列表约定见 [列表与分页](../docs/TECHNICAL_DETAILS.md#列表与分页)。

```bash
npm test
npm run build
```

测试使用 Node test runner，覆盖列表分页与请求竞态、身份隔离、告警提醒、协议生成与映射、SSE 和 Markdown 安全等行为。构建检查不等于浏览器交互验收。

本地合成数据界面验收：先执行 `npm run build`，再从本目录运行 `node tests/browser/ui-preview.mjs`；另开终端运行 `node tests/browser/naive-pages-check.mjs`、`node tests/browser/onboarding-modes-check.mjs` 与 `node tests/browser/protocol-actions-check.mjs`，用 `IOT_TEST_BROWSER` 指定 Chrome 或 Edge 可执行文件。第一个覆盖侧栏全部主页面、弹层、窄屏与账户菜单；第二个覆盖添加设备向导的共享监听与标准上报；第三个覆盖协议版本操作。这些脚本只连接本机夹具，不写真实业务数据。每条浏览器命令超过 30 秒未响应会直接报错；`IOT_UI_SKIP_SCREENSHOTS=1` 可跳过截图，`IOT_UI_SKIP_OVERLAYS=页面/操作,…` 可跳过指定弹层，仅用于定位环境问题。macOS 会在约 30 秒后结束不允许后台运行的无头 Chrome（退出码 0），浏览器脚本随之中断；需在“系统设置 → 通用 → 登录项与扩展 → 允许在后台”中允许 Google Chrome，或在 Linux 环境运行。

## 页面与权限

`src/views/` 保存业务页面，`src/components/` 保存共享业务组件。16 个主菜单按“运行监控、设备与接入、智能助手、系统”四组排列，菜单名称以 `src/App.vue` 为准；有设备模板菜单权限时，“平台接入点”不在侧栏单列，改在设备模板详情的“接入点”中使用。设备管理的“添加设备”向导见 `src/components/DeviceOnboarding.vue`，请求体与设备端配置说明在 `src/onboardingPlan.js`。

## 样式规范

- `src/theme/tokens.css` 是颜色、字号、间距、圆角和阴影的唯一来源；页面样式只引用这里的变量，不直接写十六进制颜色。
- `src/theme/naive.js` 在构建时读取 `tokens.css` 生成 Naive UI 主题（`src/theme/naiveTheme.js` 负责解析，`tests/theme.test.mjs` 保证变量都能解析）；组件外观通过主题配置调整，不用 `!important` 覆盖。
- `src/styles/base.css` 放元素默认样式与包装控件的布局补充，`src/styles/shell.css` 放侧栏、顶栏、页头和登录页，`src/styles/motion.css` 处理“减少动态效果”。
- 列表页统一使用 `src/components/layout/` 下的 `FilterBar`（筛选与操作）、`DataTableCard`（表格、分页、空与错误状态）、`StatusDot`（状态圆点加中文）和 `RowActions`（最多两个操作，其余收进“更多”）。窄屏下侧栏改为抽屉，设备列表改为卡片。
- 深蓝 `--primary` 用于主操作和选中；火焰橙 `--flame` 只用于当前位置和告警强调。
- `src/styles/patterns.css` 只放多个页面共用的卡片、分页、表格操作和技术详情样式；单个页面或组件的样式写在各自 `.vue` 文件内，由 `v-html` 或传送门渲染的组件（Markdown、全局告警）使用带组件前缀的非 scoped 样式。
- 图表按数据用途取色：设备状态与告警等级使用状态色，单一度量的柱条只用 `--primary`，统计卡片保持中性，数值与图例文字使用文字色。
- `tests/style-rules.test.mjs` 静态检查：引用的变量都已定义、颜色值只出现在 `tokens.css`、除 `motion.css` 外不用 `!important`、不保留 `.el-` 选择器与过渡样式文件。

`src/permissions.js` 根据服务端有效权限控制菜单和按钮，实际访问仍由后端校验。`src/views/AccessView.vue` 展示用户所属租户、角色、单独权限及设备范围。普通用户每3秒通过 `src/realtime.js` 读取授权范围内的事件，不签发浏览器 MQTT 凭据；初始快照不重播历史告警。内置管理员继续使用 MQTT。完整规则见 [用户权限](../docs/USER_ACCESS_CONTROL.md)。

## 浏览器验证

从仓库根目录按需执行以下脚本，需先启动前端5173和后端8081，并按脚本提供本机管理员环境配置。用户管理与设备范围脚本会创建并清理临时账户；设备范围验证需有可选的设备及告警数据。

```bash
node iot_front/tests/browser/access-management-check.mjs
node iot_front/tests/browser/device-scope-check.mjs
```

其他专项浏览器回归保留在 `tests/browser/`，按脚本中的 `IOT_TEST_*` 参数准备浏览器、服务和数据后运行；不包含在 `npm test` 中。历史演示数据专用脚本及仅匹配页面源码文案的断言已移除。更多文档见 [文档索引](../docs/README.md)。
