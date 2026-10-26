<!-- 通知设置：提醒行为与通知渠道绑定 -->
<template>
  <section>
    <div class="mb-4 flex-cb gap-3 max-sm:flex-col max-sm:items-start">
      <div>
        <h3 class="text-base font-medium text-g-900">通知设置</h3>
        <p class="text-sm text-g-500">选择需要提醒的行为，并绑定一个或多个通知渠道</p>
      </div>
      <div class="flex-c gap-2">
        <ElButton :loading="loading" @click="load">刷新</ElButton>
        <ElButton type="primary" :loading="saving" @click="saveAll">保存设置</ElButton>
      </div>
    </div>

    <ElTable v-loading="loading" :data="rules" row-key="event" border>
      <ElTableColumn label="提醒行为" min-width="200">
        <template #default="{ row }">
          <div class="leading-5">
            <div class="font-medium text-g-900">{{ row.label }}</div>
            <div class="text-xs text-g-500">{{ row.event }}</div>
          </div>
        </template>
      </ElTableColumn>
      <ElTableColumn label="提醒" width="110">
        <template #default="{ row }">
          <ElSwitch
            :model-value="!!row.enabled"
            inline-prompt
            active-text="启用"
            inactive-text="停用"
            :loading="!!savingEvents[row.event]"
            @change="toggleRule(row.event, Boolean($event))"
          />
        </template>
      </ElTableColumn>
      <ElTableColumn label="通知渠道" min-width="320">
        <template #default="{ row }">
          <ElSelect v-model="row.channelIds" multiple collapse-tags collapse-tags-tooltip placeholder="选择通知渠道" class="w-full">
            <ElOption v-for="item in channelOptions" :key="item.value" :label="item.label" :value="item.value" />
          </ElSelect>
        </template>
      </ElTableColumn>
      <ElTableColumn label="模板" width="100">
        <template #default="{ row }">
          <TableAction icon="ri:pencil-line" type="info" @click="openTemplate(row)">编辑</TableAction>
        </template>
      </ElTableColumn>
    </ElTable>

    <ElDrawer v-model="templateOpen" :title="`${templateRule?.label || ''} 通知模板`" :size="drawerSize" destroy-on-close>
      <div class="mb-4 flex-c gap-3 max-sm:flex-col max-sm:items-start">
        <span class="shrink-0 text-sm font-medium text-g-900">发送渠道</span>
        <ElSelect v-model="draft.channelIds" multiple collapse-tags collapse-tags-tooltip placeholder="选择通知渠道" class="w-full">
          <ElOption v-for="item in channelOptions" :key="item.value" :label="item.label" :value="item.value" />
        </ElSelect>
      </div>
      <ElTabs v-model="activeChannel">
        <ElTabPane v-for="item in templateChannels" :key="item.key" :name="item.key">
          <template #label>
            <span class="inline-flex items-center gap-1.5">
              <img :src="item.icon" alt="" class="size-4" />{{ item.label }}
            </span>
          </template>
          <TemplatePanel v-model="draft.templates[item.key]" :channel="item" :variables="variables" :sample="sample" />
        </ElTabPane>
      </ElTabs>
      <template #footer>
        <ElButton @click="templateOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="saveTemplate">保存模板</ElButton>
      </template>
    </ElDrawer>
  </section>
</template>

<script setup lang="ts">
  import { ElMessage } from 'element-plus'
  import { getNotificationRules, listAll, updateNotificationRules, type Row } from '@/api/dasslm'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { notificationChannelNames, notificationChannels, useResponsiveSize } from '@/components/dasslm/meta'
  import TemplatePanel from './template-panel.vue'

  defineOptions({ name: 'SettingsNoticeRules' })

  const channelIcon = (value: string) => notificationChannels.find((item) => item.value === value)?.image || ''

  const templateChannels = [
    { key: 'wecom', label: '企业微信', icon: channelIcon('wecom_webhook') },
    { key: 'feishu', label: '飞书', icon: channelIcon('feishu_webhook') },
    { key: 'dingtalk', label: '钉钉', icon: channelIcon('dingtalk_webhook') },
    { key: 'custom', label: '自定义', icon: channelIcon('custom_webhook'), custom: true }
  ]

  const commonVariables = [
    { label: '通知标题', value: '{{title}}' },
    { label: '通知内容', value: '{{content}}' },
    { label: '事件类型', value: '{{event}}' },
    { label: '消息等级', value: '{{level}}' }
  ]

  const eventVariables: Record<string, { label: string; value: string }[]> = {
    admin_login_success: [
      { label: '管理员账号', value: '{{username}}' },
      { label: '登录 IP', value: '{{ip}}' }
    ],
    certificate_issued: [
      { label: '证书域名', value: '{{domain}}' },
      { label: '到期时间', value: '{{expiresAt}}' }
    ],
    deploy_success: [
      { label: '证书域名', value: '{{domain}}' },
      { label: '部署目标', value: '{{target}}' },
      { label: '部署数量', value: '{{targetCount}}' }
    ]
  }

  const eventSamples: Record<string, Row> = {
    admin_login_success: {
      title: '管理员登录成功',
      content: '账号 admin 已登录 DaSSLm 管理后台，来源 IP：127.0.0.1',
      event: 'admin_login_success',
      level: 'info',
      variables: { username: 'admin', ip: '127.0.0.1' }
    },
    certificate_issued: {
      title: 'SSL证书签发成功',
      content: '证书 example.com 已签发成功，到期时间：2026-09-01 12:00:00。',
      event: 'certificate_issued',
      level: 'info',
      variables: { domain: 'example.com', expiresAt: '2026-09-01 12:00:00' }
    },
    deploy_success: {
      title: 'SSL证书部署成功',
      content: '证书 example.com 已部署到 宝塔面板。',
      event: 'deploy_success',
      level: 'info',
      variables: { domain: 'example.com', target: '宝塔面板', targetCount: '2' }
    }
  }

  const defaultSample: Row = {
    title: 'SSL证书签发成功',
    content: 'example.com 的证书已签发完成，可以继续部署。',
    event: 'certificate_issued',
    level: 'success',
    variables: {}
  }

  const defaultTextTemplate = '标题：{{title}}\n内容：{{content}}\n事件类型：{{event}}\n消息等级：{{level}}'

  function normalizeTemplate(item: Row = {}): Row {
    return {
      messageType: item.messageType || 'card',
      textContent: item.textContent || defaultTextTemplate,
      cardTitle: item.cardTitle || '{{title}}',
      cardContent: item.cardContent || '{{content}}',
      cardMode: item.cardMode || 'visual',
      cardJson: item.cardJson || '',
      bodyTemplate: item.bodyTemplate || ''
    }
  }

  const rules = ref<Row[]>([])
  const channels = ref<Row[]>([])
  const loading = ref(false)
  const saving = ref(false)
  const savingEvents = reactive<Record<string, boolean>>({})

  const channelOptions = computed(() =>
    channels.value
      .filter((item) => item.status !== 'disabled')
      .map((item) => ({ label: `${item.name}（${notificationChannelNames[item.channel] || item.channel || '-'}）`, value: item.id }))
  )

  async function load() {
    loading.value = true
    try {
      const [ruleRes, channelRows] = await Promise.all([getNotificationRules(), listAll('notification-settings')])
      rules.value = (ruleRes.data || []).map((item) => ({ ...item, channelIds: item.channelIds || [] }))
      channels.value = channelRows
    } finally {
      loading.value = false
    }
  }

  onMounted(load)

  async function saveRules(nextRules: Row[], silent = false) {
    const res = await updateNotificationRules(nextRules)
    rules.value = (res.data || []).map((item) => ({ ...item, channelIds: item.channelIds || [] }))
    if (!silent) ElMessage.success('保存成功')
  }

  async function saveAll() {
    saving.value = true
    try {
      await saveRules(rules.value)
    } finally {
      saving.value = false
    }
  }

  /** 开关即时保存，失败时回滚 */
  async function toggleRule(event: string, enabled: boolean) {
    const previous = rules.value
    const next = rules.value.map((item) => (item.event === event ? { ...item, enabled } : item))
    rules.value = next
    savingEvents[event] = true
    try {
      await saveRules(next, true)
    } catch {
      rules.value = previous
    } finally {
      savingEvents[event] = false
    }
  }

  // ---------------- 模板 ----------------
  const drawerSize = useResponsiveSize(760)
  const templateOpen = ref(false)
  const templateRule = ref<Row | null>(null)
  const activeChannel = ref('wecom')
  const draft = reactive<{ channelIds: number[]; templates: Record<string, Row> }>({ channelIds: [], templates: {} })
  const variables = computed(() => [...commonVariables, ...(eventVariables[templateRule.value?.event] || [])])
  const sample = computed(() => eventSamples[templateRule.value?.event] || defaultSample)

  function openTemplate(rule: Row) {
    templateRule.value = rule
    draft.channelIds = [...(rule.channelIds || [])]
    draft.templates = Object.fromEntries(templateChannels.map((item) => [item.key, normalizeTemplate(rule.templates?.[item.key])]))
    activeChannel.value = 'wecom'
    templateOpen.value = true
  }

  async function saveTemplate() {
    const event = templateRule.value?.event
    if (!event) return
    saving.value = true
    try {
      const next = rules.value.map((item) =>
        item.event === event ? { ...item, channelIds: draft.channelIds, templates: draft.templates } : item
      )
      await saveRules(next)
      templateOpen.value = false
      templateRule.value = null
    } finally {
      saving.value = false
    }
  }
</script>
