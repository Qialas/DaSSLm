import type { AppRouteRecordRaw } from '@/utils/router'
export const staticRoutes: AppRouteRecordRaw[] = [
  { path: '/auth/login', alias: '/login', name: 'Login', component: () => import('@views/auth/login/index.vue'), meta: { title: '登录', isHideTab: true } },
  { path: '/403', name: 'Exception403', component: () => import('@views/exception/403/index.vue'), meta: { title: '无权访问', isHideTab: true } },
  { path: '/500', name: 'Exception500', component: () => import('@views/exception/500/index.vue'), meta: { title: '服务异常', isHideTab: true } },
  { path: '/:pathMatch(.*)*', name: 'Exception404', component: () => import('@views/exception/404/index.vue'), meta: { title: '页面不存在', isHideTab: true } }
]
