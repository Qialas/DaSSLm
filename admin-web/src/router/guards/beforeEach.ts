import type { Router } from 'vue-router'
import NProgress from 'nprogress'
import { useUserStore } from '@/store/modules/user'
import { useSettingStore } from '@/store/modules/setting'
import { useMenuStore } from '@/store/modules/menu'
import { useWorktabStore } from '@/store/modules/worktab'
import { fetchGetUserInfo } from '@/api/auth'
import { ApiError, getToken } from '@/services/client.mjs'
import { setPageTitle } from '@/utils/router'
import { setWorktab } from '@/utils/navigation'
import { RouteRegistry, MenuProcessor } from '../core'

let registry: RouteRegistry | undefined
let initialization: Promise<void> | undefined
let generation = 0

// Compatibility with the official afterEach/layout integrations.
export function getPendingLoading() { return false }
export function resetPendingLoading() {}
export function getRouteInitFailed() { return false }
export function resetRouteInitState() { initialization = undefined }

export function resetRouterState(_delay = 0): void {
  generation++
  initialization = undefined
  registry?.unregister()
  const menu = useMenuStore()
  menu.clearRemoveRouteFns()
  menu.setMenuList([])
  menu.setHomePath('/dashboard')
}

async function initializeRoutes(router: Router) {
  if (registry?.isRegistered()) return
  if (initialization) return initialization
  const currentGeneration = generation
  initialization = (async () => {
    const user = await fetchGetUserInfo()
    const menus = await new MenuProcessor().getMenuList()
    // A late user response must not restore routes after logout.
    if (currentGeneration !== generation || !getToken()) return
    const store = useUserStore()
    store.setUserInfo(user)
    store.checkAndClearWorktabs()
    registry?.register(menus)
    useMenuStore().setMenuList(menus)
    useWorktabStore().validateWorktabs(router)
  })()
  try { await initialization } finally { initialization = undefined }
}

export function setupBeforeEachGuard(router: Router): void {
  registry = new RouteRegistry(router)
  router.beforeEach(async (to) => {
    if (useSettingStore().showNprogress) NProgress.start()
    const user = useUserStore()
    if (to.name === 'Login') {
      if (getToken() && user.isLogin) return '/dashboard'
      setPageTitle(to)
      return true
    }
    if (!getToken()) {
      user.setLoginStatus(false)
      return { name: 'Login', query: { redirect: to.fullPath }, replace: true }
    }
    if (!registry?.isRegistered()) {
      try {
        await initializeRoutes(router)
        if (!getToken()) return { name: 'Login', query: { redirect: to.fullPath }, replace: true }
        return { path: to.fullPath, replace: true }
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) {
          return { name: 'Login', query: { redirect: to.fullPath }, replace: true }
        }
        if (to.name === 'Exception500') return true
        return { name: 'Exception500', replace: true }
      }
    }
    if (to.path === '/') return '/dashboard'
    setWorktab(to)
    setPageTitle(to)
    return true
  })
}
