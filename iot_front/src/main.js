import { createApp } from 'vue'
import App from './App.vue'
import './styles.css'
import { permissionDirective } from './permissions'
import { installUi } from './ui'
import { pinia } from './stores/index.ts'
import { router } from './router/index.ts'

const app = createApp(App)
app.use(pinia)
app.use(router)
installUi(app)
app.directive('permission', permissionDirective).mount('#app')
