<!-- 代理池 -->
<template>
  <section>
    <div class="mb-4 flex-cb gap-3 max-sm:flex-col max-sm:items-start">
      <div>
        <h3 class="text-base font-medium text-g-900">代理池</h3>
        <p class="text-sm text-g-500">统一维护 HTTP、HTTPS、SOCK4、SOCK5、SOCK5H 代理节点</p>
      </div>
      <ElButton type="primary" @click="openForm()">
        <ArtSvgIcon icon="ri:add-line" class="mr-1" />新增代理
      </ElButton>
    </div>

    <ElRow :gutter="12" class="mb-2">
      <ElCol v-for="item in stats" :key="item.label" :xs="24" :md="8">
        <div class="mb-3 flex-c gap-3 rounded-lg border border-[var(--default-border)] bg-g-100 p-3">
          <span class="size-10 flex-cc rounded-lg bg-theme/10 text-lg text-theme"><ArtSvgIcon :icon="item.icon" /></span>
          <span>
            <span class="block text-xs text-g-500">{{ item.label }}</span>
            <strong class="text-xl text-g-900">{{ item.value }}<small class="ml-0.5 text-xs text-g-500">{{ item.suffix }}</small></strong>
          </span>
        </div>
      </ElCol>
    </ElRow>

    <RemoteTable ref="tableRef" :fetcher="fetchProxies">
      <ElTableColumn label="代理名称" min-width="180">
        <template #default="{ row }">
          <div class="leading-5">
            <div class="font-medium text-g-900">{{ row.name }}</div>
            <div class="text-xs text-g-500">{{ row.host }}:{{ row.port }}</div>
          </div>
        </template>
      </ElTableColumn>
      <ElTableColumn label="协议" width="100">
        <template #default="{ row }">
          <ElTag :type="protocolMeta[row.protocol]?.type || 'info'" disable-transitions>{{ protocolMeta[row.protocol]?.text || row.protocol || '-' }}</ElTag>
        </template>
      </ElTableColumn>
      <ElTableColumn label="账号" width="130">
        <template #default="{ row }">
          <span v-if="row.username">{{ row.username }}</span>
          <span v-else class="text-g-500">无认证</span>
        </template>
      </ElTableColumn>
      <ElTableColumn prop="weight" label="权重" width="80" />
      <ElTableColumn label="状态" width="90">
        <template #default="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
      </ElTableColumn>
      <ElTableColumn label="最近检测" width="170">
        <template #default="{ row }">
          <span :class="{ 'text-g-500': !row.lastTestAt }">{{ formatDateTime(row.lastTestAt, '未检测') }}</span>
        </template>
      </ElTableColumn>
      <ElTableColumn prop="remark" label="备注" min-width="120" show-overflow-tooltip />
      <ElTableColumn label="操作" width="230" fixed="right">
        <template #default="{ row }">
          <TableAction icon="ri:flask-line" :loading="testingId === row.id" @click="testProxy(row)">测试</TableAction>
          <TableAction icon="ri:pencil-line" type="info" @click="openForm(row)">编辑</TableAction>
          <ElPopconfirm title="确认删除这个代理？" @confirm="remove(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ElTableColumn>
    </RemoteTable>

    <ElDrawer v-model="formOpen" :title="`${current ? '编辑' : '新增'}代理`" :size="drawerSize" destroy-on-close>
      <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
        <ElFormItem label="代理名称" prop="name">
          <ElInput v-model="form.name" />
        </ElFormItem>
        <ElFormItem label="代理协议" prop="protocol">
          <ElSelect v-model="form.protocol">
            <ElOption v-for="(meta, value) in protocolMeta" :key="value" :label="meta.text" :value="value" />
          </ElSelect>
        </ElFormItem>
        <div class="grid grid-cols-[2fr_1fr] gap-3 max-sm:grid-cols-1">
          <ElFormItem label="代理主机" prop="host">
            <ElInput v-model="form.host" />
          </ElFormItem>
          <ElFormItem label="端口" prop="port">
            <ElInputNumber v-model="form.port" :min="1" :max="65535" :precision="0" controls-position="right" class="!w-full" />
          </ElFormItem>
          <ElFormItem label="用户名" prop="username">
            <ElInput v-model="form.username" autocomplete="off" />
          </ElFormItem>
          <ElFormItem label="密码" prop="password">
            <ElInput v-model="form.password" type="password" show-password v-bind="secretInputAttrs" />
          </ElFormItem>
          <ElFormItem label="权重" prop="weight">
            <ElInputNumber v-model="form.weight" :min="1" :max="100" :precision="0" controls-position="right" class="!w-full" />
          </ElFormItem>
          <ElFormItem label="状态" prop="status">
            <ElSelect v-model="form.status">
              <ElOption v-for="item in enabledStatusOptions" :key="item.value" :label="item.label" :value="item.value" />
            </ElSelect>
          </ElFormItem>
        </div>
        <ElFormItem label="备注" prop="remark">
          <ElInput v-model="form.remark" type="textarea" :rows="3" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="formOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
      </template>
    </ElDrawer>
  </section>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { createResource, deleteResource, listResource, testProxySetting, updateResource, type Row } from '@/api/dasslm'
  import RemoteTable from '@/components/dasslm/RemoteTable.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import {
    enabledStatusOptions,
    formatDateTime,
    secretInputAttrs,
    statusTag,
    useResponsiveSize,
    type TagMeta
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'SettingsProxyPool' })

  const RESOURCE = 'proxy-settings'
  const PROTOCOLS = ['http', 'https', 'sock4', 'sock5', 'sock5h']
  const protocolMeta: Record<string, TagMeta> = {
    http: { type: 'primary', text: 'HTTP' },
    https: { type: 'success', text: 'HTTPS' },
    sock4: { type: 'warning', text: 'SOCK4' },
    sock5: { type: 'info', text: 'SOCK5' },
    sock5h: { type: 'danger', text: 'SOCK5H' }
  }

  const tableRef = ref<InstanceType<typeof RemoteTable>>()
  const rows = ref<Row[]>([])

  const stats = computed(() => {
    const protocols = new Set(rows.value.map((item) => item.protocol))
    return [
      { label: '池内代理', value: rows.value.length, icon: 'ri:links-line', suffix: '' },
      { label: '启用节点', value: rows.value.filter((item) => item.status === 'enabled').length, icon: 'ri:checkbox-circle-line', suffix: '' },
      { label: '协议覆盖', value: PROTOCOLS.filter((item) => protocols.has(item)).length, icon: 'ri:global-line', suffix: '/5' }
    ]
  })

  async function fetchProxies(page: number, pageSize: number) {
    const res = await listResource(RESOURCE, { page, pageSize })
    rows.value = res.data?.items || []
    return res.data
  }

  const testingId = ref<number>()
  async function testProxy(row: Row) {
    testingId.value = row.id
    try {
      const res = await testProxySetting(row.id)
      ElMessage.success(res.data?.message || '检测通过')
      tableRef.value?.load()
    } finally {
      testingId.value = undefined
    }
  }

  async function remove(row: Row) {
    await deleteResource(RESOURCE, row.id)
    ElMessage.success('已删除')
    tableRef.value?.load()
  }

  const drawerSize = useResponsiveSize(520)
  const formOpen = ref(false)
  const saving = ref(false)
  const current = ref<Row | null>(null)
  const formRef = ref<FormInstance>()
  const form = ref<Row>({})
  const rules: FormRules = {
    name: [{ required: true, message: '请输入代理名称', trigger: 'blur' }],
    protocol: [{ required: true, message: '请选择代理协议', trigger: 'change' }],
    host: [{ required: true, message: '请输入代理主机', trigger: 'blur' }],
    port: [{ required: true, message: '请输入端口', trigger: 'change' }]
  }

  function openForm(row?: Row) {
    current.value = row || null
    form.value = row ? { ...row } : { protocol: 'http', status: 'enabled', weight: 1 }
    formOpen.value = true
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      const payload = { ...(current.value || {}), ...form.value }
      if (current.value?.id) await updateResource(RESOURCE, current.value.id, payload)
      else await createResource(RESOURCE, payload)
      ElMessage.success('保存成功')
      formOpen.value = false
      tableRef.value?.load()
    } finally {
      saving.value = false
    }
  }
</script>
