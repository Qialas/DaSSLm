<!-- 部署账号：宝塔面板 / 腾讯云 -->
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
            {{ deployProviderLabel(row.provider) }}
          </span>
        </template>
        <template #endpoint="{ row }">
          <template v-if="row.provider === 'tencentcloud'">默认</template>
          <template v-else>{{ row.endpoint || '-' }}</template>
        </template>
        <template #useProxy="{ row }">
          <ElTag :type="proxyTag(row).type" disable-transitions>{{ proxyTag(row).text }}</ElTag>
        </template>
        <template #status="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction
            v-if="['btpanel', 'baota', 'tencentcloud'].includes(row.provider)"
            icon="ri:flask-line"
            :loading="testingId === row.id"
            @click="testAccount(row)"
          >
            检测
          </TableAction>
          <TableAction v-if="isBTPanelProvider(row.provider)" icon="ri:list-unordered" @click="openSites(row)">站点</TableAction>
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
    <ElDrawer v-model="formOpen" :title="`${current ? '编辑' : '新建'}部署账号`" :size="formSize" destroy-on-close>
      <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
        <ElFormItem label="账号名称" prop="name">
          <ElInput v-model="form.name" />
        </ElFormItem>
        <ElFormItem label="部署服务" prop="provider">
          <CardRadio v-model="form.provider" :options="deployProviders" aria-label="部署服务" @change="formRef?.clearValidate()" />
        </ElFormItem>
        <ElFormItem
          v-if="!isTencent"
          label="面板地址"
          prop="endpoint"
          :rules="[{ required: true, message: '请输入宝塔面板地址', trigger: 'blur' }]"
        >
          <div class="w-full">
            <ElInput v-model="form.endpoint" placeholder="https://panel.example.com:8888" />
            <div class="mt-1 text-xs leading-5 text-g-500">已自动忽略宝塔面板 HTTPS 自签名证书校验。</div>
          </div>
        </ElFormItem>
        <ElFormItem
          :label="isTencent ? 'SecretId' : 'API密钥'"
          prop="accessKey"
          :rules="[{ required: true, message: isTencent ? '请输入腾讯云SecretId' : '请输入宝塔面板API密钥', trigger: 'blur' }]"
        >
          <ElInput v-model="form.accessKey" type="password" show-password v-bind="secretInputAttrs" />
        </ElFormItem>
        <ElFormItem
          v-if="isTencent"
          label="SecretKey"
          prop="secretKey"
          :rules="[{ required: true, message: '请输入腾讯云SecretKey', trigger: 'blur' }]"
        >
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

    <!-- 宝塔站点列表 -->
    <ElDrawer v-model="sitesOpen" :title="`${siteAccount?.name || ''} 站点列表`" :size="sitesSize" destroy-on-close @closed="siteAccount = null">
      <ElTabs v-model="siteType" @tab-change="siteTableRef?.reload()">
        <ElTabPane v-for="tab in siteTypeTabs" :key="tab.key" :label="tab.label" :name="tab.key" />
      </ElTabs>
      <RemoteTable v-if="siteAccount" ref="siteTableRef" :fetcher="fetchSites" :row-key="siteRowKey">
        <ElTableColumn prop="name" label="站点名称" min-width="200" />
        <ElTableColumn label="类型" width="90">
          <template #default="{ row }">{{ row.type || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="根目录" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ row.path || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="90">
          <template #default="{ row }">{{ row.status || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="备注" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.ps || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="到期时间" width="130">
          <template #default="{ row }">{{ row.expire || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="创建时间" width="170">
          <template #default="{ row }">{{ row.addTime || '-' }}</template>
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
    listDeployAccountSites,
    testDeployAccount,
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
    deployProviderLabel,
    deployProviders,
    enabledStatusOptions,
    isBTPanelProvider,
    proxyTag,
    secretInputAttrs,
    statusTag,
    useResponsiveSize
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmDeployAccounts' })

  const RESOURCE = 'deploy-accounts'

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
    { prop: 'provider', label: '部署服务', minWidth: 140, useSlot: true },
    {
      prop: 'endpoint',
      label: '服务配置',
      minWidth: 200,
      showOverflowTooltip: true,
      useSlot: true
    },
    { prop: 'useProxy', label: '代理', width: 100, useSlot: true },
    { prop: 'status', label: '状态', width: 90, useSlot: true },
    { prop: 'remark', label: '备注', minWidth: 140, showOverflowTooltip: true },
    { prop: 'operation', label: '操作', width: 290, fixed: 'right', useSlot: true }
  ])

  const testingId = ref<number>()

  async function testAccount(row: Row) {
    testingId.value = row.id
    try {
      const res = await testDeployAccount(row.id)
      ElMessage.success(res.data?.message || '检测通过')
    } catch {
      // 错误提示已由接口层统一弹出
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
  const formSize = useResponsiveSize(620)
  const formOpen = ref(false)
  const saving = ref(false)
  const current = ref<Row | null>(null)
  const formRef = ref<FormInstance>()
  const form = ref<Row>({})
  const isTencent = computed(() => form.value.provider === 'tencentcloud')
  const rules: FormRules = {
    name: [{ required: true, message: '请输入账号名称', trigger: 'blur' }],
    provider: [{ required: true, message: '请选择部署服务', trigger: 'change' }],
    status: [{ required: true, message: '请选择状态', trigger: 'change' }]
  }

  function openForm(row?: Row) {
    current.value = row || null
    form.value = row ? { ...row } : { status: 'enabled', provider: 'btpanel', useProxy: false, proxyMode: 'manual' }
    formOpen.value = true
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      const payload = {
        ...form.value,
        endpoint: isTencent.value ? '' : String(form.value.endpoint || '').replace(/\/+$/, '')
      }
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

  // ---------------- 宝塔站点 ----------------
  const siteTypeTabs = [
    { key: 'all', label: '全部' },
    { key: 'PHP', label: 'PHP' },
    { key: 'Go', label: 'Go' },
    { key: 'Node', label: 'Node' },
    { key: 'Java', label: 'Java' },
    { key: 'Python', label: 'Python' },
    { key: 'net', label: '.NET' },
    { key: 'Proxy', label: '反代' },
    { key: 'HTML', label: 'HTML' },
    { key: 'Other', label: '其他' }
  ]
  const sitesSize = useResponsiveSize(920)
  const sitesOpen = ref(false)
  const siteAccount = ref<Row | null>(null)
  const siteType = ref('all')
  const siteTableRef = ref<InstanceType<typeof RemoteTable>>()
  const siteRowKey = (row: Row) => String(row.id || row.name)

  function openSites(row: Row) {
    siteType.value = 'all'
    siteAccount.value = row
    sitesOpen.value = true
  }

  async function fetchSites(page: number, pageSize: number) {
    if (!siteAccount.value?.id) return { items: [], total: 0 }
    const res = await listDeployAccountSites(siteAccount.value.id, { page, pageSize, type: siteType.value })
    return res.data
  }
</script>
