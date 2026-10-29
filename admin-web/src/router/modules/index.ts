import type { AppRouteRecord } from '@/types/router'

const page = (path: string, name: string, title: string, view: string, icon: string): AppRouteRecord => ({
  path, name, component: `/dasslm/${view}`,
  meta: { title, icon, keepAlive: false }
})

export const routeModules: AppRouteRecord[] = [
  { ...page('/dashboard', 'Dashboard', '仪表盘', 'dashboard', 'ri:dashboard-line'), meta: { title: '仪表盘', icon: 'ri:dashboard-line', fixedTab: true, keepAlive: false } },
  page('/domains', 'Domains', '域名管理', 'domains', 'ri:global-line'),
  { ...page('/domains/:id(\\d+)', 'DomainRecords', 'DNS 记录', 'domain-records', 'ri:list-check'), meta: { title: 'DNS 记录', isHide: true, activePath: '/domains', keepAlive: false } },
  page('/certificates', 'Certificates', 'SSL 证书', 'certificates', 'ri:shield-keyhole-line'),
  page('/tasks', 'Tasks', '自动任务', 'tasks', 'ri:time-line'),
  {
    path: '/accounts', name: 'Accounts', component: '/index/index', redirect: '/accounts/domain',
    meta: { title: '账号管理', icon: 'ri:team-line', roles: ['admin'] },
    children: [
      page('domain', 'DomainAccounts', '域名账号', 'domain-accounts', 'ri:cloud-line'),
      page('ssl', 'SSLAccounts', 'SSL 账号', 'ssl-accounts', 'ri:file-shield-line'),
      page('deploy', 'DeployAccounts', '部署账号', 'deploy-accounts', 'ri:server-line')
    ]
  },
  page('/logs', 'Logs', '日志管理', 'logs', 'ri:file-list-3-line'),
  {
    path: '/settings', name: 'Settings', component: '/index/index', redirect: '/settings/base',
    meta: { title: '系统设置', icon: 'ri:settings-3-line', roles: ['admin'] },
    children: [
      page('base', 'BaseSettings', '基础设置', 'settings', 'ri:settings-line'),
      page('profile', 'Users', '后台用户', 'users', 'ri:user-settings-line'),
      page('notice', 'Notifications', '通知渠道', 'notifications', 'ri:notification-3-line')
    ]
  },
  { ...page('/manage', 'Manage', '管理', 'manage', 'ri:apps-line'), meta: { title: '管理', isHide: true, keepAlive: false } }
]
