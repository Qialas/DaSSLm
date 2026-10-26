import type { FastEnterConfig } from '@/types/config'
const config: FastEnterConfig = {
  minWidth: 1200,
  applications: [
    { name: '域名管理', description: 'DNS 解析与域名资产', icon: 'ri:global-line', iconColor: '#5D87FF', enabled: true, order: 1, routeName: 'Domains' },
    { name: 'SSL 证书', description: '申请、续签与证书部署', icon: 'ri:shield-keyhole-line', iconColor: '#13DEB9', enabled: true, order: 2, routeName: 'Certificates' },
    { name: '自动任务', description: '证书自动部署任务', icon: 'ri:time-line', iconColor: '#F9901F', enabled: true, order: 3, routeName: 'Tasks' }
  ],
  quickLinks: [
    { name: '基础设置', enabled: true, order: 1, routeName: 'BaseSettings' },
    { name: '日志管理', enabled: true, order: 2, routeName: 'Logs' }
  ]
}
export default Object.freeze(config)
