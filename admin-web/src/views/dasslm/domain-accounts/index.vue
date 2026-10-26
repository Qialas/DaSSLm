<!-- 域名账号：DNS 服务商接入凭证 -->
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
            <ProviderIcon :provider="row.provider" />
            {{ providerNames[row.provider] || row.provider || '-' }}
          </span>
        </template>
        <template #useProxy="{ row }">
          <ElTag :type="proxyTag(row).type" disable-transitions>{{ proxyTag(row).text }}</ElTag>
        </template>
        <template #status="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction icon="ri:flask-line" :loading="testingId === row.id" @click="testAccount(row)">测试</TableAction>
          <TableAction icon="ri:pencil-line" @click="openForm(row)">编辑</TableAction>
          <ElPopconfirm title="确认删除？" @confirm="remove(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ArtTable>
    </ElCard>

    <ElDrawer v-model="formOpen" :title="`${current ? '编辑' : '新建'}域名账号`" :size="drawerSize" destroy-on-close>
      <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
        <ElFormItem label="账号名称" prop="name">
          <ElInput v-model="form.name" />
        </ElFormItem>
        <ElFormItem label="DNS渠道商" prop="provider">
          <CardRadio v-model="form.provider" :options="dnsProviders" aria-label="DNS渠道商" @change="formRef?.clearValidate(['provider', 'accessKey'])" />
        </ElFormItem>
        <ElFormItem :label="isCloudflare ? '账号标识（可选）' : 'AccessKey / SecretId'" prop="accessKey" :rules="accessKeyRules">
          <ElInput v-model="form.accessKey" v-bind="secretInputAttrs" />
        </ElFormItem>
        <ElFormItem :label="isCloudflare ? 'API Token' : 'SecretKey / SecretAccessKey'" prop="secretKey">
          <ElInput v-model="form.secretKey" type="password" show-password v-bind="secretInputAttrs" />
        </ElFormItem>
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
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { createResource, deleteResource, testDomainAccount, updateResource, type Row } from '@/api/dasslm'
  import CardRadio from '@/components/dasslm/CardRadio.vue'
  import ProviderIcon from '@/components/dasslm/ProviderIcon.vue'
  import ProxyFields from '@/components/dasslm/ProxyFields.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResourceTable } from '@/components/dasslm/useResourceTable'
  import {
    dnsProviders,
    enabledStatusOptions,
    formatDateTime,
    providerNames,
    proxyTag,
    secretInputAttrs,
    statusTag,
    useResponsiveSize
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmDomainAccounts' })

  const RESOURCE = 'domain-accounts'

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
    { prop: 'provider', label: 'DNS服务商', minWidth: 150, useSlot: true },
    { prop: 'accessKey', label: 'AccessKey', minWidth: 180, showOverflowTooltip: true },
    { prop: 'lastTestAt', label: '最近检测', width: 170, formatter: (row) => formatDateTime(row.lastTestAt) },
    { prop: 'useProxy', label: '代理', width: 100, useSlot: true },
    { prop: 'status', label: '状态', width: 90, useSlot: true },
    { prop: 'remark', label: '备注', minWidth: 140, showOverflowTooltip: true },
    { prop: 'operation', label: '操作', width: 230, fixed: 'right', useSlot: true }
  ])

  const testingId = ref<number>()

  async function testAccount(row: Row) {
    testingId.value = row.id
    try {
      const res = await testDomainAccount(row.id)
      ElMessage.success(res.data?.message || '检测通过')
      refreshUpdate()
    } finally {
      testingId.value = undefined
    }
  }

  async function remove(row: Row) {
    await deleteResource(RESOURCE, row.id)
    ElMessage.success('已删除')
    refreshRemove()
  }

  // 表单
  const drawerSize = useResponsiveSize(560)
  const formOpen = ref(false)
  const saving = ref(false)
  const current = ref<Row | null>(null)
  const formRef = ref<FormInstance>()
  const form = ref<Row>({})
  const isCloudflare = computed(() => form.value.provider === 'cloudflare')
  const rules: FormRules = {
    name: [{ required: true, message: '请输入账号名称', trigger: 'blur' }],
    provider: [{ required: true, message: '请选择DNS渠道商', trigger: 'change' }],
    secretKey: [{ required: true, message: '请输入授权密钥', trigger: 'blur' }],
    status: [{ required: true, message: '请选择状态', trigger: 'change' }]
  }
  const accessKeyRules = computed(() =>
    isCloudflare.value ? [] : [{ required: true, message: '请输入AccessKey或SecretId', trigger: 'blur' }]
  )

  function openForm(row?: Row) {
    current.value = row || null
    form.value = row ? { ...row } : { status: 'enabled', provider: 'aliyun', useProxy: false, proxyMode: 'manual' }
    formOpen.value = true
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      if (current.value?.id) {
        await updateResource(RESOURCE, current.value.id, { ...current.value, ...form.value })
        refreshUpdate()
      } else {
        await createResource(RESOURCE, form.value)
        refreshCreate()
      }
      ElMessage.success('保存成功')
      formOpen.value = false
    } finally {
      saving.value = false
    }
  }
</script>
