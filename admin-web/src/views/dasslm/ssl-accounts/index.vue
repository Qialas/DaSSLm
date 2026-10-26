<!-- SSL 账号：ACME / 云厂商免费证书账号 -->
<template>
  <div class="art-full-height">
    <ElCard class="art-table-card" shadow="never">
      <ArtTableHeader v-model:columns="columnChecks" :loading="loading" @refresh="refreshData">
        <template #left>
          <ElButton type="primary" v-ripple @click="openForm()">
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
        <template #provider="{ row }">
          <span class="inline-flex items-center gap-2">
            <ProviderIcon v-bind="providerIconProps(row.provider)" />
            {{ sslProviderNames[row.provider] || row.provider || '-' }}
          </span>
        </template>
        <template #useProxy="{ row }">
          <ElTag :type="proxyTag(row).type" disable-transitions>{{ proxyTag(row).text }}</ElTag>
        </template>
        <template #status="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction v-if="row.provider === 'tencent_free'" icon="ri:file-shield-2-line" @click="openCertificates(row)">证书</TableAction>
          <TableAction icon="ri:pencil-line" @click="openForm(row)">编辑</TableAction>
          <ElPopconfirm title="确认删除？" @confirm="remove(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ArtTable>
    </ElCard>

    <!-- 新建 / 编辑 -->
    <ElDrawer v-model="formOpen" :title="`${current ? '编辑' : '新建'}SSL账号`" :size="formSize" destroy-on-close>
      <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
        <ElFormItem label="账号名称" prop="name">
          <ElInput v-model="form.name" />
        </ElFormItem>
        <ElFormItem label="证书服务" prop="provider">
          <CardRadio v-model="form.provider" :options="sslProviders" aria-label="证书服务" @change="formRef?.clearValidate()" />
        </ElFormItem>
        <ElFormItem
          v-if="needsContactEmail"
          :label="form.provider === 'tencent_free' ? '联系人邮箱' : '注册邮箱'"
          prop="email"
          :rules="[{ required: true, message: form.provider === 'tencent_free' ? '请输入腾讯云证书联系人邮箱' : '请输入ACME注册邮箱', trigger: 'blur' }]"
        >
          <ElInput v-model="form.email" />
        </ElFormItem>
        <ElFormItem
          v-if="form.provider === 'custom_acme'"
          label="ACME目录地址"
          prop="directoryUrl"
          :rules="[{ required: true, message: '请输入ACME目录地址', trigger: 'blur' }]"
        >
          <ElInput v-model="form.directoryUrl" placeholder="https://acme.example.com/directory" />
        </ElFormItem>
        <div v-if="needsEAB" class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
          <ElFormItem label="EAB Key ID" prop="eabKid" :rules="eabRules('请输入LiteSSL EAB Key ID')">
            <ElInput v-model="form.eabKid" v-bind="secretInputAttrs" />
          </ElFormItem>
          <ElFormItem label="EAB HMAC Key" prop="eabHmacKey" :rules="eabRules('请输入LiteSSL EAB HMAC Key')">
            <ElInput v-model="form.eabHmacKey" type="password" show-password v-bind="secretInputAttrs" />
          </ElFormItem>
        </div>
        <template v-if="isCloudProvider">
          <ElFormItem
            :label="form.provider === 'tencent_free' ? 'SecretId' : 'AccessKey ID'"
            prop="accessKey"
            :rules="[{ required: true, message: '请输入云厂商AccessKey或SecretId', trigger: 'blur' }]"
          >
            <ElInput v-model="form.accessKey" v-bind="secretInputAttrs" />
          </ElFormItem>
          <ElFormItem
            :label="form.provider === 'tencent_free' ? 'SecretKey' : 'AccessKey Secret'"
            prop="secretKey"
            :rules="[{ required: true, message: '请输入云厂商SecretKey', trigger: 'blur' }]"
          >
            <ElInput v-model="form.secretKey" type="password" show-password v-bind="secretInputAttrs" />
          </ElFormItem>
        </template>
        <ElFormItem label="状态" prop="status">
          <ElSelect v-model="form.status" class="!w-1/2 max-sm:!w-full">
            <ElOption v-for="item in enabledStatusOptions" :key="item.value" :label="item.label" :value="item.value" />
          </ElSelect>
        </ElFormItem>
        <ProxyFields v-model="form" />
        <ElFormItem label="备注" prop="remark">
          <ElInput v-model="form.remark" type="textarea" :rows="3" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="formOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
      </template>
    </ElDrawer>

    <!-- 腾讯云证书资源 -->
    <ElDrawer v-model="certOpen" :title="`${certAccount?.name || ''} 证书资源`" :size="certSize" destroy-on-close>
      <template #header>
        <div class="flex items-center justify-between gap-3 pr-2">
          <span class="text-base font-medium text-g-900">{{ certAccount?.name || '' }} 证书资源</span>
          <ElButton type="primary" :loading="importingAll" :disabled="!certAccount?.id" @click="importAll">
            <ArtSvgIcon icon="ri:save-3-line" class="mr-1" />一键保存到本地
          </ElButton>
        </div>
      </template>
      <RemoteTable v-if="certAccount" ref="certTableRef" :fetcher="fetchCertificates" row-key="certificateId">
        <ElTableColumn prop="domain" label="域名" min-width="200" />
        <ElTableColumn prop="certificateId" label="证书ID" min-width="160" show-overflow-tooltip />
        <ElTableColumn label="颁发者" min-width="140">
          <template #default="{ row }">{{ row.productZhName || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="100">
          <template #default="{ row }">
            <ElTag :type="tencentStatus(row).type" disable-transitions>{{ tencentStatus(row).text }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="验证方式" width="100">
          <template #default="{ row }">{{ row.verifyType || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="过期时间" width="170">
          <template #default="{ row }">{{ row.certEndTime || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="备注" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.alias || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="96" fixed="right">
          <template #default="{ row }">
            <TableAction icon="ri:save-3-line" :loading="importingId === row.certificateId" @click="importOne(row)">导入</TableAction>
          </template>
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
    importSSLAccountCertificate,
    importSSLAccountCertificates,
    listSSLAccountCertificates,
    updateResource,
    type Row
  } from '@/api/dasslm'
  import CardRadio from '@/components/dasslm/CardRadio.vue'
  import ProviderIcon from '@/components/dasslm/ProviderIcon.vue'
  import ProxyFields from '@/components/dasslm/ProxyFields.vue'
  import RemoteTable from '@/components/dasslm/RemoteTable.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResourceTable } from '@/components/dasslm/useResourceTable'
  import {
    ACME_PROVIDERS,
    acmeDirectoryMap,
    enabledStatusOptions,
    proxyTag,
    secretInputAttrs,
    sslProviderNames,
    sslProviders,
    statusTag,
    useResponsiveSize,
    type TagMeta
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmSSLAccounts' })

  const RESOURCE = 'ssl-accounts'

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
    { prop: 'name', label: '账号名称', minWidth: 160 },
    { prop: 'provider', label: '证书服务', minWidth: 170, useSlot: true },
    { prop: 'email', label: '邮箱', minWidth: 180, formatter: (row) => row.email || '-' },
    { prop: 'directoryUrl', label: 'ACME目录', minWidth: 220, showOverflowTooltip: true, formatter: (row) => row.directoryUrl || '-' },
    { prop: 'useProxy', label: '代理', width: 100, useSlot: true },
    { prop: 'status', label: '状态', width: 90, useSlot: true },
    { prop: 'remark', label: '备注', minWidth: 140, showOverflowTooltip: true },
    { prop: 'operation', label: '操作', width: 230, fixed: 'right', useSlot: true }
  ])

  function providerIconProps(provider: string) {
    const meta = sslProviders.find((item) => item.value === provider)
    return { image: meta?.image, icon: meta?.icon || (meta?.image ? undefined : 'ri:shield-check-line') }
  }

  async function remove(row: Row) {
    await deleteResource(RESOURCE, row.id)
    ElMessage.success('已删除')
    refreshRemove()
  }

  // ---------------- 表单 ----------------
  const formSize = useResponsiveSize(620)
  const formOpen = ref(false)
  const saving = ref(false)
  const current = ref<Row | null>(null)
  const formRef = ref<FormInstance>()
  const form = ref<Row>({})
  const rules: FormRules = {
    name: [{ required: true, message: '请输入账号名称', trigger: 'blur' }],
    provider: [{ required: true, message: '请选择证书服务', trigger: 'change' }],
    status: [{ required: true, message: '请选择状态', trigger: 'change' }]
  }

  const isCloudProvider = computed(() => ['tencent_free', 'aliyun_free'].includes(form.value.provider))
  const needsContactEmail = computed(() => ACME_PROVIDERS.includes(form.value.provider) || form.value.provider === 'tencent_free')
  const needsEAB = computed(() => ['zerossl', 'litessl', 'custom_acme'].includes(form.value.provider))
  const eabRules = (message: string) =>
    form.value.provider === 'litessl' ? [{ required: true, message, trigger: 'blur' }] : []

  function openForm(row?: Row) {
    current.value = row || null
    form.value = row ? { ...row } : { status: 'enabled', provider: 'letsencrypt', useProxy: false, proxyMode: 'manual' }
    formOpen.value = true
  }

  /** 按证书服务清理无关字段，内置 ACME 服务使用固定目录地址 */
  function normalizePayload(values: Row) {
    const payload = { ...values }
    if (payload.provider !== 'custom_acme') payload.directoryUrl = acmeDirectoryMap[payload.provider] || ''
    if (!['zerossl', 'litessl', 'custom_acme'].includes(payload.provider)) {
      payload.eabKid = ''
      payload.eabHmacKey = ''
    }
    if (![...ACME_PROVIDERS, 'tencent_free'].includes(payload.provider)) payload.email = ''
    if (!['tencent_free', 'aliyun_free'].includes(payload.provider)) {
      payload.accessKey = ''
      payload.secretKey = ''
    }
    return payload
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      const payload = normalizePayload(form.value)
      if (current.value?.id) {
        await updateResource(RESOURCE, current.value.id, { ...current.value, ...payload })
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

  // ---------------- 腾讯云证书资源 ----------------
  const certSize = useResponsiveSize(920)
  const certOpen = ref(false)
  const certAccount = ref<Row | null>(null)
  const certTableRef = ref<InstanceType<typeof RemoteTable>>()
  const importingAll = ref(false)
  const importingId = ref<string>()

  const tencentStatusTags: Record<string, TagMeta> = {
    '0': { type: 'primary', text: '审核中' },
    '1': { type: 'success', text: '已通过' },
    '2': { type: 'danger', text: '审核失败' },
    '3': { type: 'danger', text: '已过期' },
    '4': { type: 'primary', text: '已加DNS' },
    '6': { type: 'info', text: '取消中' },
    '7': { type: 'info', text: '已取消' },
    '8': { type: 'primary', text: '待确认' },
    '9': { type: 'warning', text: '吊销中' },
    '10': { type: 'info', text: '已吊销' }
  }

  function tencentStatus(row: Row): TagMeta {
    const code = row.status == null ? '' : String(row.status)
    const meta = tencentStatusTags[code] || { type: 'info', text: row.statusMsg || code || '-' }
    return { type: meta.type, text: row.statusName || meta.text }
  }

  function openCertificates(row: Row) {
    certAccount.value = row
    certOpen.value = true
  }

  async function fetchCertificates(page: number, pageSize: number) {
    if (!certAccount.value?.id) return { items: [], total: 0 }
    const res = await listSSLAccountCertificates(certAccount.value.id, { page, pageSize })
    return res.data
  }

  async function importOne(row: Row) {
    if (!certAccount.value?.id) return
    importingId.value = row.certificateId
    try {
      await importSSLAccountCertificate(certAccount.value.id, row.certificateId)
      ElMessage.success('已导入到本地')
      certTableRef.value?.load()
    } finally {
      importingId.value = undefined
    }
  }

  async function importAll() {
    if (!certAccount.value?.id) return
    importingAll.value = true
    try {
      const res = await importSSLAccountCertificates(certAccount.value.id)
      ElMessage.success(`已保存 ${res.data?.saved || 0} 张证书`)
      certTableRef.value?.load()
    } finally {
      importingAll.value = false
    }
  }
</script>
