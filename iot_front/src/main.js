import { createApp } from 'vue' /* 引入当前代码需要的依赖。 */
import App from './App.vue' /* 引入当前代码需要的依赖。 */
import './styles.css' /* 引入当前代码需要的依赖。 */
import './naive-admin.css' /* 应用基于参考模板的全站管理界面主题。 */
import { permissionDirective } from './permissions' /* 引入当前代码需要的依赖。 */
import { installUi } from './ui' /* 注册全部由 Naive UI 绘制的业务控件。 */

const app = createApp(App) /* 创建平台前端应用。 */
installUi(app) /* 安装 Naive UI 业务控件和加载指令。 */
app.directive('permission', permissionDirective).mount('#app') /* 挂载权限指令和应用。 */
