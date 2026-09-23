import { createApp } from 'vue' /* 引入当前代码需要的依赖。 */
import App from './App.vue' /* 引入当前代码需要的依赖。 */
import 'element-plus/theme-chalk/el-message-box.css' /* 引入当前代码需要的依赖。 */
import 'element-plus/theme-chalk/el-message.css' /* 引入当前代码需要的依赖。 */
import './styles.css' /* 引入当前代码需要的依赖。 */
import { permissionDirective } from './permissions' /* 引入当前代码需要的依赖。 */

createApp(App).directive('permission',permissionDirective).mount('#app') /* 执行当前语句并推进处理流程。 */
