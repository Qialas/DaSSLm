import { ElMessage } from 'element-plus'
import { request } from '@/services/client.mjs'

/** 业务接口返回的通用行数据 */
export type Row = Record<string, any>

/** 分页列表接口的数据结构 */
export interface ListResult<T = Row> {
  items: T[]
  total: number
  page?: number
  pageSize?: number
  [key: string]: any
}

export interface ApiResult<T = any> {
  code: number
  message: string
  data: T
}

type ApiOptions = RequestInit & { silentError?: boolean }
type Params = Record<string, unknown>

let lastErrorText = ''
let lastErrorAt = 0

function notifyApiError(text: string) {
  const content = text || '请求失败'
  const now = Date.now()
  if (content === lastErrorText && now - lastErrorAt < 1200) return
  lastErrorText = content
  lastErrorAt = now
  ElMessage.error(content)
}

/** 调用 Go 服务端接口；失败时统一弹出错误提示（401 由登录态处理，不重复提示） */
export async function api<T = any>(path: string, options: ApiOptions = {}): Promise<ApiResult<T>> {
  try {
    return (await request(path, options)) as ApiResult<T>
  } catch (error: any) {
    if (!options.silentError && error?.status !== 401) notifyApiError(error?.message || '网络请求失败')
    throw error
  }
}

function toQuery(params: Params = {}) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    search.append(key, String(value))
  })
  const text = search.toString()
  return text ? `?${text}` : ''
}

const json = (data: unknown) => JSON.stringify(data)

export function listResource<T = Row>(resource: string, params: Params = {}) {
  return api<ListResult<T>>(`/${resource}${toQuery(params)}`)
}

/** 拉取一个资源的前 100 条，用于下拉选项与 ID 映射 */
export async function listAll<T = Row>(resource: string, params: Params = {}): Promise<T[]> {
  const res = await listResource<T>(resource, { page: 1, pageSize: 100, ...params })
  return res.data?.items || []
}

export function getResource<T = Row>(resource: string, id: number | string) {
  return api<T>(`/${resource}/${id}`)
}

export function createResource<T = Row>(resource: string, data: unknown) {
  return api<T>(`/${resource}`, { method: 'POST', body: json(data) })
}

export function updateResource<T = Row>(resource: string, id: number | string, data: unknown) {
  return api<T>(`/${resource}/${id}`, { method: 'PUT', body: json(data) })
}

export function deleteResource(resource: string, id: number | string) {
  return api(`/${resource}/${id}`, { method: 'DELETE' })
}

export const testDomainAccount = (id: number) => api<Row>(`/domain-accounts/${id}/test`, { method: 'POST' })
export const listProviderDomains = (accountId: number) =>
  api<{ items: Row[]; selected?: Record<string, boolean> }>(`/domain-accounts/${accountId}/provider-domains`)

export const listDomainRecords = (domainId: number | string) => api<ListResult>(`/domains/${domainId}/records`)
export const listDomainRecordLines = (domainId: number | string) => api<Row>(`/domains/${domainId}/record-lines`)
export const refreshDomainExpires = (domainId: number) => api<Row>(`/domains/${domainId}/refresh-expires`, { method: 'POST' })
export const queryDomainWhois = (domainId: number) => api<Row>(`/domains/${domainId}/whois`)
export const createDomainRecord = (domainId: number | string, data: unknown) =>
  api<Row>(`/domains/${domainId}/records`, { method: 'POST', body: json(data) })
export const updateDomainRecord = (domainId: number | string, recordId: number | string, data: unknown) =>
  api<Row>(`/domains/${domainId}/records/${recordId}`, { method: 'PUT', body: json(data) })
export const deleteDomainRecord = (domainId: number | string, recordId: number | string) =>
  api(`/domains/${domainId}/records/${recordId}`, { method: 'DELETE' })

export const submitCertificate = (id: number) => api<Row>(`/certificates/${id}/submit`, { method: 'POST' })
export const revokeCertificate = (id: number) => api<Row>(`/certificates/${id}/revoke`, { method: 'POST' })
export const resetCertificate = (id: number) => api<Row>(`/certificates/${id}/reset`, { method: 'POST' })
export const deployCertificate = (id: number, data: unknown) =>
  api<Row>(`/certificates/${id}/deploy`, { method: 'POST', body: json(data) })
export const getCertificateDetail = (id: number, params: Params = {}) => api<Row>(`/certificates/${id}/detail${toQuery(params)}`)

export const runAutoTask = (id: number) => api<Row>(`/tasks/${id}/run`, { method: 'POST' })
export const runLogCleanup = (data: { retentionDays: number }) =>
  api<Row>('/log-cleanup', { method: 'POST', body: json(data) })

export const listDeployAccountSites = (id: number, params: Params = {}) =>
  api<ListResult>(`/deploy-accounts/${id}/sites${toQuery(params)}`)
export const testDeployAccount = (id: number) => api<Row>(`/deploy-accounts/${id}/test`, { method: 'POST' })

export const listSSLAccountCertificates = (id: number, params: Params = {}) =>
  api<ListResult>(`/ssl-accounts/${id}/certificates${toQuery(params)}`)
export const importSSLAccountCertificates = (id: number) =>
  api<Row>(`/ssl-accounts/${id}/certificates/import`, { method: 'POST' })
export const importSSLAccountCertificate = (id: number, certificateId: string) =>
  api<Row>(`/ssl-accounts/${id}/certificates/${encodeURIComponent(certificateId)}/import`, { method: 'POST' })

export const testProxySetting = (id: number) => api<Row>(`/proxy-settings/${id}/test`, { method: 'POST' })
export const testNotificationSetting = (id: number) => api<Row>(`/notification-settings/${id}/test`, { method: 'POST' })
export const getNotificationRules = () => api<Row[]>('/notification-rules')
export const updateNotificationRules = (data: unknown) =>
  api<Row[]>('/notification-rules', { method: 'PUT', body: json(data) })
export const changePassword = (data: { oldPassword: string; newPassword: string }) =>
  api<{ token: string; expiresAt: string }>('/auth/password', { method: 'PUT', body: json(data) })
export const getSystemTasks = () => api<Row[]>('/system-tasks')
export const updateSystemTask = (key: string, data: unknown) =>
  api<Row>(`/system-tasks/${encodeURIComponent(key)}`, { method: 'PUT', body: json(data) })

/** useTable 使用的分页字段，对应 Go 服务端的 page / pageSize */
export const PAGE_KEYS = { current: 'page', size: 'pageSize' }

/** 生成 useTable 可直接使用的分页请求函数 */
export function pagedFetcher<T = Row>(resource: string, extra?: () => Params) {
  return async (params: Params = {}): Promise<Api.Common.PaginatedResponse<T>> => {
    const res = await listResource<T>(resource, { ...(extra?.() || {}), ...params })
    const data = res.data || ({} as ListResult<T>)
    return {
      records: data.items || [],
      total: data.total || 0,
      current: Number(data.page || params.page || 1),
      size: Number(data.pageSize || params.pageSize || 10)
    }
  }
}
