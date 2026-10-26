<!-- 通知渠道：企业微信 / 飞书 / 钉钉 / 自定义 Webhook -->
<template>
  <div class="art-full-height">
    <ElCard class="art-table-card" shadow="never">
      <ArtTableHeader v-model:columns="columnChecks" :loading="loading" @refresh="refreshData">
        <template #left>
          <ElButton type="primary" v-ripple @click="openForm()">
            <ArtSvgIcon icon="ri:add-line" class="mr-1" />新建渠道
          </ElButton>
        </template>
      </ArtTableHeader>

      <ArtTable
        :loading="loading"
        :data="data"
        :columns="columns"
        :pagination="pagination"
        @pagination:size-change="handleSizeChange"
        @pagination:current-change="handleCurrentChange"
      >
        <template #name="{ row }">
          <div class="leading-5">
            <div class="font-medium text-g-900">{{ row.name }}</div>
            <div class="text-xs text-g-500">ID: {{ row.id }}</div>
          </div>
        </template>
        <template #channel="{ row }">
          <ElTag :type="channelMeta(row.channel).tag" disable-transitions>
            <span class="inline-flex items-center gap-1">
              <img v-if="channelMeta(row.channel).image" :src="channelMeta(row.channel).image" alt="" class="size-3.5" />
              {{ channelMeta(row.channel).label }}
            </span>
          </ElTag>
        </template>
        <template #status="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction icon="ri:send-plane-line" :loading="testingId === row.id" @click="testChannel(row)">测试</TableAction>
          <TableAction icon="ri:pencil-line" @click="openForm(row)">编辑</TableAction>
          <ElPopconfirm title="确认删除这个通知渠道？" @confirm="remove(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ArtTable>
    </ElCard>

    <ElDrawer v-model="formOpen" :title="`${current ? '编辑' : '新建'}通知渠道`" :size="drawerSize" destroy-on-close>
      <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
        <ElFormItem label="渠道名称" prop="name">
          <ElInput v-model="form.name" />
        </ElFormItem>
        <ElFormItem label="渠道类型" prop="channel">
          <CardRadio v-model="form.channel" :options="notificationChannels" aria-label="渠道类型" />
        </ElFormItem>
        <ElFormItem label="Webhook 地址" prop="webhookUrl">
          <ElInput v-model="form.webhookUrl" placeholder="https://..." />
        </ElFormItem>
        <template v-if="form.channel === 'custom_webhook'">
          <ElFormItem label="请求方法" prop="method">
            <ElSelect v-model="form.method" class="!w-1/2 max-sm:!w-full">
              <ElOption v-for="method in ['POST', 'PUT', 'PATCH']" :key="method" :label="method" :value="method" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="请求头" prop="headers">
            <ElInput v-model="form.headers" type="textarea" :rows="3" placeholder="Authorization: Bearer xxx" />
          </ElFormItem>
          <ElFormItem label="请求体模板" prop="bodyTemplate">
            <ElInput
              v-model="form.bodyTemplate"
              type="textarea"
              :rows="6"
              class="font-mono"
              placeholder='{"title":"{{title}}","content":"{{content}}","event":"{{event}}","level":"{{level}}"}'
            />
          </ElFormItem>
        </template>
        <ElFormItem v-else-if="form.channel !== 'wecom_webhook'" label="签名密钥（可选）" prop="secret">
          <ElInput v-model="form.secret" type="password" show-password placeholder="机器人安全设置中的加签密钥" v-bind="secretInputAttrs" />
        </ElFormItem>
        <ElFormItem label="状态" prop="status">
          <ElSelect v-model="form.status" class="!w-1/2 max-sm:!w-full">
            <ElOption v-for="item in enabledStatusOptions" :key="item.value" :label="item.label" :value="item.value" />
          </ElSelect>
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="formOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
      </template>
    </ElDrawer>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { createResource, deleteResource, testNotificationSetting, updateResource, type Row } from '@/api/dasslm'
  import CardRadio from '@/components/dasslm/CardRadio.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResourceTable } from '@/components/dasslm/useResourceTable'
  import {
    enabledStatusOptions,
    notificationChannels,
    parseJSON,
    secretInputAttrs,
    statusTag,
    useResponsiveSize
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmNotifications' })

  const RESOURCE = 'notification-settings'

  const {
    columns,
    columnChecks,
    data,
    loading,
    pagination,
    handleSizeChange,
    handleCurrentChange,
    refreshData,
    refreshCreate,
    refreshUpdate,
    refreshRemove
  } = useResourceTable(RESOURCE, () => [
    { prop: 'name', label: '渠道名称', minWidth: 200, useSlot: true },
    { prop: 'channel', label: '渠道类型', width: 170, useSlot: true },
    { prop: 'status', label: '状态', width: 90, useSlot: true },
    { prop: 'operation', label: '操作', width: 230, fixed: 'right', useSlot: true }
  ])

  function channelMeta(channel: string) {
    return (
      notificationChannels.find((item) => item.value === channel) || {
        value: channel,
        label: channel || '-',
        tag: 'info' as const,
        image: ''
      }
    )
  }

  const testingId = ref<number>()

  async function testChannel(row: Row) {
    testingId.value = row.id
    try {
      const res = await testNotificationSetting(row.id)
      ElMessage.success(res.data?.message || '发送成功')
    } finally {
      testingId.value = undefined
    }
  }

  async function remove(row: Row) {
    await deleteResource(RESOURCE, row.id)
    ElMessage.success('已删除')
    refreshRemove()
  }

  // ---------------- 表单 ----------------
  const drawerSize = useResponsiveSize(580)
  const formOpen = ref(false)
  const saving = ref(false)
  const current = ref<Row | null>(null)
  const formRef = ref<FormInstance>()
  const form = ref<Row>({})
  const rules: FormRules = {
    name: [{ required: true, message: '请输入渠道名称', trigger: 'blur' }],
    channel: [{ required: true, message: '请选择渠道类型', trigger: 'change' }],
    webhookUrl: [{ required: true, message: '请输入 Webhook 地址', trigger: 'blur' }]
  }

  /** 渠道配置以 JSON 字符串保存在 config 字段中，编辑时展开到表单 */
  function openForm(row?: Row) {
    current.value = row || null
    form.value = {
      status: 'enabled',
      channel: 'wecom_webhook',
      method: 'POST',
      ...(row || {}),
      ...parseJSON<Row>(row?.config, {})
    }
    formOpen.value = true
  }

  function toPayload(values: Row) {
    const { webhookUrl, secret, method, headers, bodyTemplate, ...base } = values
    // 只保存当前渠道类型用到的配置，避免切换类型后残留旧字段
    const isCustom = base.channel === 'custom_webhook'
    const usesSecret = base.channel === 'feishu_webhook' || base.channel === 'dingtalk_webhook'
    return {
      ...(current.value || {}),
      ...base,
      config: JSON.stringify({
        webhookUrl: webhookUrl || '',
        secret: usesSecret ? secret || '' : '',
        method: isCustom ? method || 'POST' : 'POST',
        headers: isCustom ? headers || '' : '',
        bodyTemplate: isCustom ? bodyTemplate || '' : ''
      }),
      events: current.value?.events || '[]'
    }
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      const payload = toPayload(form.value)
      if (current.value?.id) {
        await updateResource(RESOURCE, current.value.id, payload)
        refreshUpdate()
      } else {
        await createResource(RESOURCE, payload)
        refreshCreate()
      }
      ElMessage.success('保存成功')
      formOpen.value = false
    } finally {
      saving.value = false
    }
  }
</script>
