<!-- 日志管理 -->
<template>
  <div class="art-card logs-page p-4">
    <ElTabs v-model="activeTab" class="logs-page__tabs">
      <ElTabPane v-for="tab in tabs" :key="tab.key" :name="tab.key" lazy>
        <template #label>
          <span class="inline-flex items-center gap-1.5">
            <ArtSvgIcon :icon="tab.icon" />{{ tab.label }}
          </span>
        </template>
        <RemoteTable :key="`${tab.key}-${refreshKey}`" :fetcher="tableFetcher(tab.resource)" max-height="calc(100vh - 330px)">
          <ElTableColumn
            v-for="column in tab.columns"
            :key="column.label"
            :label="column.label"
            :width="column.width"
            :min-width="column.minWidth"
            :show-overflow-tooltip="column.ellipsis"
          >
            <template #default="{ row }">
              <ElTag v-if="column.tag" :type="column.tag(row).type" disable-transitions>{{ column.tag(row).text }}</ElTag>
              <template v-else>{{ cellText(column, row) }}</template>
            </template>
          </ElTableColumn>
          <ElTableColumn v-if="tab.detail" label="操作" width="90" fixed="right">
            <template #default="{ row }">
              <TableAction icon="ri:eye-line" type="info" @click="openDetail(tab, row)">详情</TableAction>
            </template>
          </ElTableColumn>
        </RemoteTable>
      </ElTabPane>
    </ElTabs>
    <ElButton class="logs-page__cleanup" type="danger" plain @click="cleanupOpen = true">
      <ArtSvgIcon icon="ri:delete-bin-6-line" class="mr-1" />清理
    </ElButton>

    <DetailDrawer v-model="detailOpen" :title="detailTitle" :items="detailItems" />

    <ElDialog v-model="cleanupOpen" title="清理日志" width="420px" align-center destroy-on-close>
      <ElForm label-position="top" @submit.prevent>
        <ElFormItem label="清理多少天前的日志" required>
          <ElInputNumber v-model="retentionDays" :min="1" :max="3650" :precision="0" controls-position="right" class="!w-48" />
          <span class="ml-2 text-g-600">天前</span>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="cleanupOpen = false">取消</ElButton>
        <ElButton type="danger" :loading="cleaning" @click="runCleanup">清理</ElButton>
      </template>
    </ElDialog>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage } from 'element-plus'
  import { listResource, runLogCleanup, type Row } from '@/api/dasslm'
  import DetailDrawer from '@/components/dasslm/DetailDrawer.vue'
  import RemoteTable from '@/components/dasslm/RemoteTable.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import {
    certificateActionNames,
    domainAccountActionNames,
    domainRecordActionNames,
    durationText,
    formatDateTime,
    notificationChannelNames,
    notificationEventNames,
    statusTag,
    type DetailItem,
    type TagMeta
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmLogs' })

  interface LogColumn {
    label: string
    prop?: string
    width?: number
    minWidth?: number
    ellipsis?: boolean
    /** 自定义文本 */
    text?: (row: Row) => unknown
    /** 以标签显示 */
    tag?: (row: Row) => TagMeta
    /** 时间格式 */
    time?: boolean
  }

  interface LogTab {
    key: string
    label: string
    icon: string
    resource: string
    columns: LogColumn[]
    detail?: { title: string; items: (row: Row) => DetailItem[] }
  }

  const taskTypeNames: Record<string, string> = {
    certificate_apply: '后台证书申请',
    certificate_issue: '后台证书验证签发'
  }

  const statusColumn: LogColumn = { label: '状态', width: 90, tag: (row) => statusTag(row.status) }
  const timeColumn: LogColumn = { label: '时间', prop: 'createdAt', width: 170, time: true }
  const statusItem = (row: Row): DetailItem => ({ label: '状态', tag: statusTag(row.status) })

  /** 请求链路字段：URL / 方法 / 请求头 / 请求体 / 响应体 */
  const traceItems = (row: Row, methodLabel = '请求方式'): DetailItem[] => [
    { label: '请求URL', value: row.requestUrl, block: true },
    { label: methodLabel, value: row.requestMethod },
    { label: '请求头', value: row.requestHeaders, block: true },
    { label: '请求体', value: row.requestBody, block: true },
    { label: '响应体', value: row.responseBody, block: true }
  ]

  const tabs: LogTab[] = [
    {
      key: 'login',
      label: '登录日志',
      icon: 'ri:login-box-line',
      resource: 'login-logs',
      columns: [
        { label: '用户名', prop: 'username', width: 130 },
        { label: '登录IP', prop: 'ip', width: 140 },
        statusColumn,
        { label: '说明', prop: 'message', width: 160 },
        { label: 'User-Agent', prop: 'userAgent', minWidth: 240, ellipsis: true },
        { label: '登录时间', prop: 'createdAt', width: 170, time: true }
      ]
    },
    {
      key: 'operation',
      label: '操作日志',
      icon: 'ri:list-unordered',
      resource: 'logs',
      columns: [
        { label: '用户', prop: 'username', width: 120 },
        { label: '动作', prop: 'action', width: 110 },
        { label: '资源', prop: 'resource', width: 150 },
        { label: '方法', prop: 'method', width: 80 },
        { label: '路径', prop: 'path', minWidth: 220, ellipsis: true },
        { label: 'IP', prop: 'ip', width: 140 },
        statusColumn,
        timeColumn
      ]
    },
    {
      key: 'certificate',
      label: '证书日志',
      icon: 'ri:shield-keyhole-line',
      resource: 'certificate-logs',
      columns: [
        { label: '证书域名', prop: 'commonName', minWidth: 180 },
        { label: '动作', width: 90, text: (row) => certificateActionNames[row.action] || row.action },
        { label: '服务商', prop: 'provider', width: 120 },
        { label: '证书ID', prop: 'providerCertificateId', width: 170, ellipsis: true },
        statusColumn,
        { label: '说明', prop: 'message', minWidth: 220, ellipsis: true },
        timeColumn
      ],
      detail: {
        title: '证书日志详情',
        items: (row) => [
          { label: '证书域名', value: row.commonName },
          { label: '动作', value: certificateActionNames[row.action] || row.action },
          statusItem(row),
          { label: '说明', value: row.message },
          ...traceItems(row, '请求方法')
        ]
      }
    },
    {
      key: 'domain',
      label: '域名账号',
      icon: 'ri:cloud-line',
      resource: 'domain-account-logs',
      columns: [
        { label: '目标', prop: 'targetName', minWidth: 180 },
        { label: '动作', width: 100, text: (row) => domainAccountActionNames[row.action] || row.action },
        { label: 'DNS服务商', prop: 'provider', width: 120 },
        statusColumn,
        { label: '说明', prop: 'message', minWidth: 220, ellipsis: true },
        timeColumn
      ],
      detail: {
        title: '域名账号日志详情',
        items: (row) => [
          { label: '目标', value: row.targetName },
          { label: '动作', value: domainAccountActionNames[row.action] || row.action },
          { label: 'DNS服务商', value: row.provider },
          statusItem(row),
          { label: '说明', value: row.message },
          ...traceItems(row)
        ]
      }
    },
    {
      key: 'domain-record',
      label: '解析日志',
      icon: 'ri:global-line',
      resource: 'domain-record-logs',
      columns: [
        { label: '域名', prop: 'targetName', minWidth: 180 },
        { label: '操作', width: 100, text: (row) => domainRecordActionNames[row.action] || row.action },
        { label: 'DNS服务商', prop: 'provider', width: 120 },
        statusColumn,
        { label: '内容', prop: 'message', minWidth: 220, ellipsis: true },
        timeColumn
      ],
      detail: {
        title: '解析日志详情',
        items: (row) => [
          { label: '域名', value: row.targetName },
          { label: '操作', value: domainRecordActionNames[row.action] || row.action },
          { label: 'DNS服务商', value: row.provider },
          statusItem(row),
          { label: '内容', value: row.message },
          ...traceItems(row)
        ]
      }
    },
    {
      key: 'deploy',
      label: '部署账号',
      icon: 'ri:server-line',
      resource: 'certificate-deploy-logs',
      columns: [
        { label: '证书域名', prop: 'commonName', minWidth: 180 },
        { label: '部署服务', prop: 'provider', width: 120 },
        { label: '目标站点', prop: 'targetName', width: 180 },
        statusColumn,
        { label: '说明', prop: 'message', minWidth: 220, ellipsis: true },
        timeColumn
      ],
      detail: {
        title: '部署账号日志详情',
        items: (row) => [
          { label: '证书域名', value: row.commonName },
          { label: '部署服务', value: row.provider },
          { label: '目标站点', value: row.targetName },
          statusItem(row),
          { label: '说明', value: row.message },
          ...traceItems(row)
        ]
      }
    },
    {
      key: 'notification',
      label: '通知日志',
      icon: 'ri:notification-3-line',
      resource: 'notification-logs',
      columns: [
        { label: '通知事件', width: 170, text: (row) => row.eventName || notificationEventNames[row.event] || row.event },
        { label: '通知渠道', prop: 'channelName', width: 160 },
        { label: '渠道类型', width: 130, text: (row) => notificationChannelNames[row.channel] || row.channel },
        { label: '标题', prop: 'title', minWidth: 180, ellipsis: true },
        statusColumn,
        { label: '说明', prop: 'message', minWidth: 180, ellipsis: true },
        { label: '耗时', width: 90, text: (row) => durationText(row.durationMs) },
        timeColumn
      ],
      detail: {
        title: '通知日志详情',
        items: (row) => [
          { label: '通知事件', value: row.eventName || notificationEventNames[row.event] || row.event },
          { label: '通知渠道', value: row.channelName },
          { label: '渠道类型', value: notificationChannelNames[row.channel] || row.channel },
          statusItem(row),
          { label: '说明', value: row.message },
          { label: '耗时', value: durationText(row.durationMs) },
          { label: '标题', value: row.title },
          { label: '内容', value: row.content, block: true },
          { label: '请求URL', value: row.requestUrl, block: true },
          { label: '请求方式', value: row.requestMethod },
          { label: '请求头', value: row.requestHeaders, block: true },
          { label: '请求体', value: row.requestBody, block: true },
          { label: '响应状态', value: row.responseStatus },
          { label: '响应体', value: row.responseBody, block: true }
        ]
      }
    },
    {
      key: 'task',
      label: '任务日志',
      icon: 'ri:time-line',
      resource: 'task-logs',
      columns: [
        { label: '任务名称', prop: 'taskName', width: 170 },
        { label: '任务类型', width: 150, text: (row) => taskTypeNames[row.taskType] || row.taskType },
        { label: '目标', prop: 'targetName', minWidth: 180 },
        statusColumn,
        { label: '说明', prop: 'message', minWidth: 200, ellipsis: true },
        { label: '耗时', width: 90, text: (row) => durationText(row.durationMs) },
        { label: '开始时间', prop: 'startedAt', width: 170, time: true },
        { label: '结束时间', prop: 'finishedAt', width: 170, time: true }
      ],
      detail: {
        title: '任务日志详情',
        items: (row) => [
          { label: '任务名称', value: row.taskName },
          { label: '任务类型', value: taskTypeNames[row.taskType] || row.taskType },
          { label: '目标类型', value: row.targetType },
          { label: '目标ID', value: row.targetId },
          { label: '目标名称', value: row.targetName },
          statusItem(row),
          { label: '说明', value: row.message },
          { label: '耗时', value: durationText(row.durationMs) },
          { label: '开始时间', value: formatDateTime(row.startedAt) },
          { label: '结束时间', value: formatDateTime(row.finishedAt) },
          { label: '任务详情', value: row.detail, block: true }
        ]
      }
    },
    {
      key: 'system',
      label: '系统日志',
      icon: 'ri:computer-line',
      resource: 'system-logs',
      columns: [
        { label: '来源', prop: 'source', width: 100 },
        { label: '模块', prop: 'module', width: 120 },
        {
          label: '级别',
          width: 90,
          tag: (row) => ({
            type: row.level === 'error' ? 'danger' : row.level === 'warning' ? 'warning' : 'primary',
            text: row.level || '-'
          })
        },
        { label: '事件', prop: 'event', minWidth: 200, ellipsis: true },
        statusColumn,
        { label: '说明', prop: 'message', minWidth: 200, ellipsis: true },
        timeColumn
      ],
      detail: {
        title: '系统日志详情',
        items: (row) => [
          { label: '来源', value: row.source },
          { label: '模块', value: row.module },
          { label: '级别', value: row.level },
          { label: '事件', value: row.event },
          statusItem(row),
          { label: '说明', value: row.message },
          { label: '详情', value: row.detail, block: true },
          { label: '时间', value: formatDateTime(row.createdAt) }
        ]
      }
    }
  ]

  const activeTab = ref('login')
  const refreshKey = ref(0)

  const fetchers = new Map<string, (page: number, pageSize: number) => Promise<any>>()
  function tableFetcher(resource: string) {
    if (!fetchers.has(resource)) {
      fetchers.set(resource, async (page: number, pageSize: number) => (await listResource(resource, { page, pageSize })).data)
    }
    return fetchers.get(resource)!
  }

  function cellText(column: LogColumn, row: Row) {
    const value = column.text ? column.text(row) : column.prop ? row[column.prop] : undefined
    if (column.time) return formatDateTime(value as string)
    return value === undefined || value === null || value === '' ? '-' : String(value)
  }

  // 详情
  const detailOpen = ref(false)
  const detailTitle = ref('')
  const detailItems = ref<DetailItem[]>([])

  function openDetail(tab: LogTab, row: Row) {
    if (!tab.detail) return
    detailTitle.value = tab.detail.title
    detailItems.value = tab.detail.items(row)
    detailOpen.value = true
  }

  // 清理
  const cleanupOpen = ref(false)
  const cleaning = ref(false)
  const retentionDays = ref<number | undefined>(30)

  async function runCleanup() {
    if (!retentionDays.value) {
      ElMessage.warning('请输入保留天数')
      return
    }
    cleaning.value = true
    try {
      const res = await runLogCleanup({ retentionDays: Number(retentionDays.value) })
      ElMessage.success(`已清理 ${res.data?.total || 0} 条日志`)
      cleanupOpen.value = false
      refreshKey.value += 1
    } finally {
      cleaning.value = false
    }
  }
</script>

<style scoped lang="scss">
  .logs-page {
    position: relative;
  }

  .logs-page__cleanup {
    position: absolute;
    top: 16px;
    right: 16px;
  }

  .logs-page__tabs :deep(.el-tabs__header) {
    padding-right: 96px;
  }
</style>
