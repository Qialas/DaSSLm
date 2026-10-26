<!-- 域名管理 -->
<template>
  <div class="art-full-height">
    <ElCard class="art-table-card" shadow="never">
      <ArtTableHeader v-model:columns="columnChecks" :loading="loading" @refresh="refreshData">
        <template #left>
          <ElButton type="primary" v-ripple @click="openCreate">
            <ArtSvgIcon icon="ri:add-line" class="mr-1" />新建
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
          <span class="inline-flex items-center gap-2">
            <ProviderIcon :provider="row.dnsProvider" />
            <ElLink type="primary" underline="never" @click="openRecords(row)">{{ row.name }}</ElLink>
          </span>
        </template>
        <template #expiresAt="{ row }">
          <span class="inline-flex items-center gap-1">
            {{ formatDateTime(row.expiresAt, '未查询') }}
            <ElTooltip content="刷新到期时间" placement="top">
              <ElButton
                link
                size="small"
                :loading="refreshingId === row.id"
                aria-label="刷新到期时间"
                @click="refreshExpires(row)"
              >
                <ArtSvgIcon v-if="refreshingId !== row.id" icon="ri:refresh-line" />
              </ElButton>
            </ElTooltip>
          </span>
        </template>
        <template #status="{ row }">
          <ElTag :type="row.status === 'disabled' ? 'info' : 'success'" disable-transitions>
            {{ row.status === 'disabled' ? '停用' : '启用' }}
          </ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction icon="ri:list-check" @click="openRecords(row)">记录</TableAction>
          <TableAction icon="ri:file-search-line" type="success" @click="openWhois(row)">WHOIS</TableAction>
          <TableAction icon="ri:history-line" type="info" @click="logDomain = row">日志</TableAction>
          <ElPopconfirm title="确认删除？" @confirm="remove(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ArtTable>
    </ElCard>

    <!-- 新建域名 -->
    <ElDrawer v-model="createOpen" title="新建域名" :size="createSize" destroy-on-close>
      <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
        <ElFormItem label="域名账号" prop="domainAccountId">
          <ElSelect v-model="form.domainAccountId" placeholder="请选择域名账号" class="w-full" @change="loadProviderDomains">
            <ElOption v-for="item in accounts" :key="item.id" :label="`${item.name}（${providerNames[item.provider] || item.provider}）`" :value="item.id">
              <span class="inline-flex items-center gap-2">
                <ProviderIcon :provider="item.provider" />
                {{ item.name }}（{{ providerNames[item.provider] || item.provider }}）
              </span>
            </ElOption>
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="账号下域名" prop="domainNames">
          <ElSelect
            v-model="form.domainNames"
            multiple
            filterable
            class="w-full"
            :loading="providerLoading"
            :placeholder="form.domainAccountId ? '请选择账号下的域名' : '请先选择域名账号'"
          >
            <ElOption
              v-for="item in providerDomains"
              :key="item.name"
              :label="`${item.name}${item.disabled ? '（已添加）' : ''}`"
              :value="item.name"
              :disabled="item.disabled"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="状态" prop="status">
          <ElSelect v-model="form.status" class="w-full">
            <ElOption v-for="item in enabledStatusOptions" :key="item.value" :label="item.label" :value="item.value" />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="备注" prop="remark">
          <ElInput v-model="form.remark" type="textarea" :rows="4" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="createOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
      </template>
    </ElDrawer>

    <!-- WHOIS 查询 -->
    <ElDialog v-model="whoisOpen" :title="`${whoisDomain?.name || ''} WHOIS查询`" :width="whoisWidth" align-center destroy-on-close>
      <div v-loading="whoisLoading" class="min-h-40">
        <ElDescriptions :column="1" border>
          <ElDescriptionsItem label="域名">{{ whois?.domain || whoisDomain?.name || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="注册商">{{ whois?.registrar || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="WHOIS服务器">{{ whois?.whoisServer || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="注册时间">{{ formatDateTime(whois?.createdAt, '未查询') }}</ElDescriptionsItem>
          <ElDescriptionsItem label="更新时间">{{ formatDateTime(whois?.updatedAt, '未查询') }}</ElDescriptionsItem>
          <ElDescriptionsItem label="到期时间">{{ formatDateTime(whois?.expiresAt, '未查询') }}</ElDescriptionsItem>
          <ElDescriptionsItem label="状态"><CopyBlock :value="listText(whois?.status)" :pretty="false" /></ElDescriptionsItem>
          <ElDescriptionsItem label="NS服务器"><CopyBlock :value="listText(whois?.nameServers)" :pretty="false" /></ElDescriptionsItem>
          <ElDescriptionsItem label="注册人">{{ whois?.registrant || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="注册组织">{{ whois?.registrantOrg || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="原始内容"><CopyBlock :value="whois?.raw" :pretty="false" /></ElDescriptionsItem>
        </ElDescriptions>
      </div>
    </ElDialog>

    <!-- 解析日志 -->
    <ElDrawer v-model="logOpen" :title="`${logDomain?.name || ''} 解析日志`" :size="logSize" destroy-on-close>
      <RemoteTable v-if="logDomain" :fetcher="fetchLogs">
        <ElTableColumn label="操作" width="110">
          <template #default="{ row }">{{ domainRecordActionNames[row.action] || row.action || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="90">
          <template #default="{ row }">
            <ElTag :type="row.status === 'success' ? 'success' : 'danger'" disable-transitions>{{ row.status === 'success' ? '成功' : '失败' }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn prop="message" label="内容" min-width="240" show-overflow-tooltip />
        <ElTableColumn label="时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </ElTableColumn>
      </RemoteTable>
    </ElDrawer>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import {
    createResource,
    deleteResource,
    listAll,
    listProviderDomains,
    listResource,
    queryDomainWhois,
    refreshDomainExpires,
    type Row
  } from '@/api/dasslm'
  import CopyBlock from '@/components/dasslm/CopyBlock.vue'
  import ProviderIcon from '@/components/dasslm/ProviderIcon.vue'
  import RemoteTable from '@/components/dasslm/RemoteTable.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResourceTable } from '@/components/dasslm/useResourceTable'
  import {
    domainRecordActionNames,
    enabledStatusOptions,
    formatDateTime,
    providerNames,
    useResponsiveSize
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmDomains' })

  const RESOURCE = 'domains'
  const router = useRouter()

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
    { prop: 'name', label: '域名', minWidth: 200, useSlot: true },
    { prop: 'domainAccountId', label: '域名账号', width: 100 },
    { prop: 'recordCount', label: '记录数', width: 90 },
    { prop: 'expiresAt', label: '到期时间', width: 210, useSlot: true },
    { prop: 'status', label: '状态', width: 90, useSlot: true },
    { prop: 'remark', label: '备注', minWidth: 140, showOverflowTooltip: true },
    { prop: 'operation', label: '操作', width: 320, fixed: 'right', useSlot: true }
  ])

  const openRecords = (row: Row) => router.push(`/domains/${row.id}`)
  const listText = (values?: string[]) => (Array.isArray(values) && values.length ? values.join('\n') : '')

  const refreshingId = ref<number>()
  async function refreshExpires(row: Row) {
    refreshingId.value = row.id
    try {
      await refreshDomainExpires(row.id)
      ElMessage.success('到期时间已刷新')
      refreshUpdate()
    } finally {
      refreshingId.value = undefined
    }
  }

  async function remove(row: Row) {
    await deleteResource(RESOURCE, row.id)
    ElMessage.success('已删除')
    refreshRemove()
  }

  // ---------------- 新建域名 ----------------
  const createSize = useResponsiveSize(560)
  const createOpen = ref(false)
  const saving = ref(false)
  const formRef = ref<FormInstance>()
  const form = ref<Row>({})
  const accounts = ref<Row[]>([])
  const providerDomains = ref<Row[]>([])
  const providerLoading = ref(false)
  const rules: FormRules = {
    domainAccountId: [{ required: true, message: '请选择域名账号', trigger: 'change' }],
    domainNames: [{ required: true, type: 'array', min: 1, message: '请选择域名', trigger: 'change' }],
    status: [{ required: true, message: '请选择状态', trigger: 'change' }]
  }

  async function openCreate() {
    form.value = { status: 'enabled', domainNames: [] }
    providerDomains.value = []
    accounts.value = await listAll('domain-accounts')
    createOpen.value = true
  }

  let providerSeq = 0
  async function loadProviderDomains(accountId?: number) {
    const seq = ++providerSeq
    form.value.domainNames = []
    providerDomains.value = []
    if (!accountId) {
      providerLoading.value = false
      return
    }
    providerLoading.value = true
    try {
      const res = await listProviderDomains(accountId)
      if (seq !== providerSeq) return
      const selected = res.data?.selected || {}
      providerDomains.value = (res.data?.items || []).map((item) => ({ ...item, disabled: Boolean(selected[item.name]) }))
    } finally {
      if (seq === providerSeq) providerLoading.value = false
    }
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      await createResource(RESOURCE, form.value)
      ElMessage.success('保存成功')
      createOpen.value = false
      refreshCreate()
    } finally {
      saving.value = false
    }
  }

  // ---------------- WHOIS ----------------
  const whoisWidth = useResponsiveSize(760)
  const whoisOpen = ref(false)
  const whoisLoading = ref(false)
  const whoisDomain = ref<Row | null>(null)
  const whois = ref<Row | null>(null)

  async function openWhois(row: Row) {
    whoisDomain.value = row
    whois.value = null
    whoisOpen.value = true
    whoisLoading.value = true
    try {
      const res = await queryDomainWhois(row.id)
      whois.value = res.data
      if (res.data?.expiresAt) refreshUpdate()
    } finally {
      whoisLoading.value = false
    }
  }

  // ---------------- 解析日志 ----------------
  const logSize = useResponsiveSize(860)
  const logDomain = ref<Row | null>(null)
  const logOpen = computed({
    get: () => !!logDomain.value,
    set: (open: boolean) => {
      if (!open) logDomain.value = null
    }
  })

  async function fetchLogs(page: number, pageSize: number) {
    const res = await listResource('domain-record-logs', { domainId: logDomain.value?.id, page, pageSize })
    return res.data
  }
</script>
