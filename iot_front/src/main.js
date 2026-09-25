import { createApp } from 'vue'
import App from './App.vue'
import './styles.css'
import { permissionDirective } from './permissions'
import { installUi } from './ui'

const app = createApp(App)
installUi(app)
app.directive('permission', permissionDirective).mount('#app')
