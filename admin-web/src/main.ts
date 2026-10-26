import { createApp } from 'vue'
import App from './App.vue'
import { initStore } from './store'
import { initRouter } from './router'
import { useUserStore } from './store/modules/user'
import language from './locales'
import { getToken, TOKEN_KEY, UNAUTHORIZED_EVENT } from './services/client.mjs'
import { setupGlobDirectives } from './directives'
import { setupErrorHandle } from './utils/sys/error-handle'
import '@styles/core/tailwind.css'
import '@styles/index.scss'
import './plugins/icons'

const app = createApp(App)
initStore(app)
const user = useUserStore()
user.setToken(getToken())
user.setLoginStatus(Boolean(getToken()))
window.addEventListener(UNAUTHORIZED_EVENT, () => {
  if (user.isLogin) user.logOut()
})
window.addEventListener('storage', (event) => {
  if (event.key === TOKEN_KEY && !event.newValue && user.isLogin) user.logOut()
})
initRouter(app)
setupGlobDirectives(app)
setupErrorHandle(app)
app.use(language)
app.mount('#app')
