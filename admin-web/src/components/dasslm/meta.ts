import { computed } from 'vue'
import { useWindowSize } from '@vueuse/core'
import { ElMessage } from 'element-plus'
import aliyunIcon from '@/assets/aliyun.svg'
import btpanelIcon from '@/assets/btpanel.svg'
import cloudflareIcon from '@/assets/cloudflare.svg'
import dingtalkIcon from '@/assets/dingtalk.svg'
import dnspodIcon from '@/assets/dnspod.svg'
import feishuIcon from '@/assets/feishu.svg'
import letsEncryptIcon from '@/assets/letsencrypt.svg'
import litesslIcon from '@/assets/litessl.svg'
import tencentcloudIcon from '@/assets/tencentcloud.svg'
import webhookIcon from '@/assets/webhook.svg'
import wecomIcon from '@/assets/wecom.svg'
import westcnIcon from '@/assets/westcn.svg'
import zerosslIcon from '@/assets/zerossl.svg'
import type { Row } from '@/api/dasslm'

/** Element Plus Tag 类型；空字符串表示默认灰色 */
export type TagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

export interface TagMeta {
  type: TagType
  text: string
}

/** 详情抽屉中的一行 */
export interface DetailItem {
  label: string
  value?: unknown
  /** 以标签形式展示 */
  tag?: TagMeta
  /** 以可复制代码块展示 */
  block?: boolean
  hidden?: boolean
}

/** 卡片单选项（服务商、渠道等） */
export interface CardOption {
  value: string
  label: string
  desc?: string
  /** 图片地址 */
  image?: string
  /** Remix 图标名，例如 ri:links-line */
  icon?: string
  /** 文字缩写，没有图标时显示 */
  letter?: string
}

// ---------------------------------------------------------------------------
// 服务商图标
// ---------------------------------------------------------------------------

export const providerImages: Record<string, string> = {
  aliyun: aliyunIcon,
  cloudflare: cloudflareIcon,
  dnspod: dnspodIcon,
  tencentcloud: tencentcloudIcon,
  tencent_eo: tencentcloudIcon,
  westcn: westcnIcon,
  btpanel: btpanelIcon,
  baota: btpanelIcon
}

export const dnsProviders: CardOption[] = [
  { value: 'aliyun', label: '阿里云', desc: 'Alibaba Cloud DNS', image: aliyunIcon },
  { value: 'dnspod', label: 'DNSPod', desc: '腾讯云 DNSPod', image: dnspodIcon },
  { value: 'tencentcloud', label: '腾讯云', desc: 'Tencent Cloud DNS', image: tencentcloudIcon },
  { value: 'tencent_eo', label: '腾讯云 EO', desc: 'EdgeOne NS DNS', image: tencentcloudIcon },
  { value: 'westcn', label: '西部数码', desc: 'West.cn DNS', image: westcnIcon },
  { value: 'cloudflare', label: 'Cloudflare', desc: 'Global DNS', image: cloudflareIcon }
]

export const providerNames: Record<string, string> = dnsProviders.reduce<Record<string, string>>(
  (acc, item) => {
    acc[item.value] = item.label
    return acc
  },
  { btpanel: '宝塔面板', baota: '宝塔面板' }
)

export const sslProviders: (CardOption & { directoryUrl?: string })[] = [
  {
    value: 'letsencrypt',
    label: "Let's Encrypt",
    desc: 'ACME 免费证书',
    directoryUrl: 'https://acme-v02.api.letsencrypt.org/directory',
    image: letsEncryptIcon
  },
  {
    value: 'zerossl',
    label: 'ZeroSSL',
    desc: 'ACME / EAB',
    directoryUrl: 'https://acme.zerossl.com/v2/DV90',
    image: zerosslIcon
  },
  {
    value: 'litessl',
    label: 'LiteSSL',
    desc: 'FreeSSL ACME / EAB',
    directoryUrl: 'https://acme.litessl.com/acme/v2/directory',
    image: litesslIcon
  },
  { value: 'custom_acme', label: '自定义 ACME', desc: '兼容 ACME 目录', icon: 'ri:links-line' },
  { value: 'tencent_free', label: '腾讯云免费证书', desc: 'Tencent Cloud SSL', image: tencentcloudIcon },
  { value: 'aliyun_free', label: '阿里云免费证书', desc: 'Alibaba Cloud SSL', image: aliyunIcon }
]

export const sslProviderNames: Record<string, string> = Object.fromEntries(
  sslProviders.map((item) => [item.value, item.label])
)

export const acmeDirectoryMap: Record<string, string> = Object.fromEntries(
  sslProviders.filter((item) => item.directoryUrl).map((item) => [item.value, item.directoryUrl as string])
)

export const ACME_PROVIDERS = ['letsencrypt', 'zerossl', 'litessl', 'custom_acme']

export const deployProviders: CardOption[] = [
  { value: 'btpanel', label: '宝塔面板', desc: 'BT Panel Website SSL', image: btpanelIcon },
  { value: 'tencentcloud', label: '腾讯云', desc: 'SSL Certificate Upload', image: tencentcloudIcon }
]

export function deployProviderLabel(provider?: string) {
  if (provider === 'baota' || provider === 'btpanel') return '宝塔面板'
  if (provider === 'tencentcloud') return '腾讯云'
  return provider || '-'
}

export function isBTPanelProvider(provider?: string) {
  return provider === 'btpanel' || provider === 'baota'
}

/** 宝塔部署类型：网站走面板站点接口；Docker 网站没有列表接口，需手动填写域名 */
export const btDeployTypes = [
  { label: '网站', value: 'site' },
  { label: 'Docker 网站', value: 'docker' }
]

export const btDeployTypeNames: Record<string, string> = Object.fromEntries(
  btDeployTypes.map((item) => [item.value, item.label])
)

/**
 * 腾讯云部署产品，取值与 SSL 接口 DeployCertificateInstance 的 ResourceType 一致（ssl 表示只上传）。
 * 只有官方要求传地域的产品（CLB、WAF、API网关、COS）才需要填写地域。
 */
export const tencentDeployProducts = [
  { label: '仅上传到证书管理', value: 'ssl', desc: '只把证书上传到腾讯云 SSL 证书管理，不部署到具体产品。' },
  { label: 'CDN', value: 'cdn', desc: '部署到 CDN 加速域名，可一次添加多个域名。' },
  { label: 'EdgeOne', value: 'teo', desc: '部署到 EdgeOne 加速域名，可一次添加多个域名。' },
  { label: 'WAF', value: 'waf', desc: '部署到 WAF 防护域名，可一次添加多个域名。' },
  { label: 'CLB 负载均衡', value: 'clb', desc: '部署到 CLB 的 HTTPS 监听器；监听器开启 SNI 时还需填写域名。' },
  { label: 'API 网关', value: 'apigateway', desc: '部署到 API 网关服务的自定义域名。' },
  { label: 'COS 对象存储', value: 'cos', desc: '部署到存储桶绑定的自定义域名。' }
]

export const tencentDeployProductNames: Record<string, string> = {
  ...Object.fromEntries(tencentDeployProducts.map((item) => [item.value, item.label])),
  eo: 'EdgeOne'
}

/** 旧数据中的 eo 即 EdgeOne，官方取值为 teo */
export function normalizeTencentProduct(value?: string) {
  if (!value || value === 'ssl') return 'ssl'
  return value === 'eo' ? 'teo' : value
}

/** 官方要求传地域的产品 */
export const tencentRegionProducts = new Set(['clb', 'waf', 'apigateway', 'cos'])

/** 可一次添加多个域名的产品 */
export const tencentMultiDomainProducts = new Set(['cdn', 'teo', 'waf'])

/** WAF 只区分中国大陆与非中国大陆 */
export const tencentWafRegions = [
  { label: '中国大陆', value: 'ap-guangzhou' },
  { label: '非中国大陆', value: 'ap-seoul' }
]

/** 常用地域，也可手动输入其他地域 */
export const tencentRegions = [
  { label: '广州', value: 'ap-guangzhou' },
  { label: '上海', value: 'ap-shanghai' },
  { label: '北京', value: 'ap-beijing' },
  { label: '成都', value: 'ap-chengdu' },
  { label: '重庆', value: 'ap-chongqing' },
  { label: '南京', value: 'ap-nanjing' },
  { label: '中国香港', value: 'ap-hongkong' },
  { label: '新加坡', value: 'ap-singapore' },
  { label: '东京', value: 'ap-tokyo' },
  { label: '首尔', value: 'ap-seoul' },
  { label: '曼谷', value: 'ap-bangkok' },
  { label: '雅加达', value: 'ap-jakarta' },
  { label: '硅谷', value: 'na-siliconvalley' },
  { label: '弗吉尼亚', value: 'na-ashburn' },
  { label: '法兰克福', value: 'eu-frankfurt' },
  { label: '圣保罗', value: 'sa-saopaulo' }
]

export const notificationChannels: (CardOption & { tag: TagType })[] = [
  { value: 'wecom_webhook', label: '企业微信', desc: 'WeCom Bot', image: wecomIcon, tag: 'success' },
  { value: 'feishu_webhook', label: '飞书', desc: 'Feishu Bot', image: feishuIcon, tag: 'primary' },
  { value: 'dingtalk_webhook', label: '钉钉', desc: 'DingTalk Bot', image: dingtalkIcon, tag: 'warning' },
  { value: 'custom_webhook', label: '自定义 Webhook', desc: 'HTTP Callback', image: webhookIcon, tag: 'info' }
]

export const notificationChannelNames: Record<string, string> = Object.fromEntries(
  notificationChannels.map((item) => [item.value, item.label])
)

export const notificationEventNames: Record<string, string> = {
  admin_login_success: '管理员登录成功提醒',
  certificate_issued: 'SSL证书签发成功',
  deploy_success: 'SSL部署成功'
}

// ---------------------------------------------------------------------------
// 状态与动作文案
// ---------------------------------------------------------------------------

export const enabledStatusOptions = [
  { label: '启用', value: 'enabled' },
  { label: '停用', value: 'disabled' }
]

const statusTags: Record<string, TagMeta> = {
  enabled: { type: 'success', text: '启用' },
  disabled: { type: 'info', text: '停用' },
  running: { type: 'primary', text: '执行中' },
  success: { type: 'success', text: '成功' },
  failed: { type: 'danger', text: '失败' }
}

/** 通用状态标签：启用/停用、执行结果 */
export function statusTag(status?: string): TagMeta {
  return statusTags[status || ''] || { type: 'info', text: status || '-' }
}

export const certificateStatusTags: Record<string, TagMeta> = {
  queued: { type: 'warning', text: '队列中' },
  pending: { type: 'warning', text: '待申请' },
  applying: { type: 'primary', text: '申请中' },
  dns_added: { type: 'primary', text: '已加验证' },
  submitted: { type: 'primary', text: '已提交' },
  issued: { type: 'success', text: '已签发' },
  failed: { type: 'danger', text: '失败' },
  expired: { type: 'danger', text: '已过期' },
  canceled: { type: 'info', text: '已取消' },
  revoked: { type: 'info', text: '已吊销' },
  revoking: { type: 'warning', text: '吊销中' }
}

export function certificateStatusTag(status?: string): TagMeta {
  return certificateStatusTags[status || ''] || { type: 'info', text: status || '-' }
}

export const terminalCertificateStatuses = new Set(['issued', 'failed', 'expired', 'canceled', 'revoked'])

export const certificateActionNames: Record<string, string> = {
  apply: '申请',
  submit: '提交',
  verify: '验证',
  revoke: '吊销',
  revoke_verify: '吊销验证',
  revoke_check: '验证检查',
  revoke_status: '吊销状态',
  detail: '详情',
  cleanup: '清理'
}

export const domainRecordActionNames: Record<string, string> = {
  create_record: '新增记录',
  update_record: '更新记录',
  delete_record: '删除记录'
}

export const domainAccountActionNames: Record<string, string> = {
  test: '检测',
  domains: '域名列表',
  records: '解析记录',
  record_lines: '解析线路',
  ...domainRecordActionNames
}

export function proxyTag(row: Row): TagMeta {
  if (!row.useProxy) return { type: 'info', text: '直连' }
  return { type: 'primary', text: row.proxyMode === 'auto' ? '自动轮询' : '指定代理' }
}

// ---------------------------------------------------------------------------
// 工具函数
// ---------------------------------------------------------------------------

const pad = (value: number) => String(value).padStart(2, '0')

/** 统一的时间格式：YYYY-MM-DD HH:mm:ss */
export function formatDateTime(value?: string | number | Date | null, fallback = '-') {
  if (!value) return fallback
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return fallback
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(
    date.getMinutes()
  )}:${pad(date.getSeconds())}`
}

export function daysUntil(value?: string | null): number | null {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return Math.ceil((date.getTime() - Date.now()) / 86400000)
}

export function parseJSON<T>(raw: unknown, fallback: T): T {
  if (!raw || typeof raw !== 'string') return fallback
  try {
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

/** 日志正文：能解析为 JSON 的内容格式化输出 */
export function formatLogText(value: unknown) {
  if (value === undefined || value === null || value === '') return ''
  const text = String(value)
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

export function parseSANs(value: unknown): string[] {
  if (!value) return []
  if (Array.isArray(value)) return value.filter(Boolean).map(String)
  const parsed = parseJSON<unknown>(value, null)
  if (Array.isArray(parsed)) return parsed.filter(Boolean).map(String)
  return String(value)
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function normalizeSiteNames(value: unknown): string[] {
  const source = Array.isArray(value) ? value : String(value || '').split(/[\n,]/)
  return [...new Set(source.map((item) => String(item || '').trim()).filter(Boolean))]
}

export function toMap<T extends Row>(items: T[], key = 'id'): Record<string, T> {
  return items.reduce<Record<string, T>>((acc, item) => {
    acc[item[key]] = item
    return acc
  }, {})
}

export function durationText(value?: number | null) {
  return value || value === 0 ? `${value}ms` : '-'
}

/** 复制文本；非 HTTPS 环境下 navigator.clipboard 不可用，退回 execCommand */
export async function copyText(value: string, title = '内容') {
  if (!value) return
  try {
    if (window.isSecureContext && navigator.clipboard) {
      await navigator.clipboard.writeText(value)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = value
      textarea.setAttribute('readonly', '')
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      const ok = document.execCommand('copy')
      document.body.removeChild(textarea)
      if (!ok) throw new Error('copy failed')
    }
    ElMessage.success(`${title}已复制`)
  } catch {
    ElMessage.error('复制失败，请手动选择文本复制')
  }
}

/** 输入框关闭浏览器与密码管理器的自动填充 */
export const secretInputAttrs = {
  autocomplete: 'new-password',
  spellcheck: false,
  'data-lpignore': 'true',
  'data-1p-ignore': 'true',
  'data-form-type': 'other'
}

/** 抽屉/弹窗宽度：窄屏时占满屏幕 */
export function useResponsiveSize(size: number) {
  const { width } = useWindowSize()
  return computed(() => (width.value < size + 40 ? '100%' : `${size}px`))
}
