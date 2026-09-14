import { createApp } from 'vue'
import App from './App.vue'
import 'element-plus/theme-chalk/el-message-box.css'
import 'element-plus/theme-chalk/el-message.css'
import './styles.css'
import { permissionDirective } from './permissions'

createApp(App).directive('permission',permissionDirective).mount('#app')
