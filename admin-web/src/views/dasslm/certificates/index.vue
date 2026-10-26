<!-- SSL 证书 -->
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
        <template #remaining="{ row }">
          <ElTag v-if="remaining(row)" :type="remaining(row)!.type" :style="remaining(row)!.style" disable-transitions>
            {{ remaining(row)!.text }}
          </ElTag>
          <span v-else>-</span>
        </template>
        <template #status="{ row }">
          <ElTag :type="certificateStatusTag(row.status).type" disable-transitions>{{ certificateStatusTag(row.status).text }}</ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction
            :icon="flowAction(row).icon"
            :type="flowAction(row).type"
            :disabled="flowAction(row).disabled"
            :loading="flowAction(row).busy"
            @click="submitOrVerify(row)"
          >
            {{ flowAction(row).text }}
          </TableAction>
          <TableAction icon="ri:share-forward-line" @click="openDeploy(row)">自动部署</TableAction>
          <TableAction icon="ri:eye-line" type="info" @click="showDetail(row)">详情</TableAction>
          <ElDropdown trigger="click" @command="handleMore($event, row)">
            <TableAction icon="ri:more-2-fill" type="info">更多</TableAction>
            <template #dropdown>
              <ElDropdownMenu>
                <ElDropdownItem command="renew">修改续期天数</ElDropdownItem>
                <ElDropdownItem command="logs">证书日志</ElDropdownItem>
                <ElDropdownItem command="revoke" divided>吊销证书</ElDropdownItem>
                <ElDropdownItem command="delete" class="!text-danger">删除</ElDropdownItem>
              </ElDropdownMenu>
            </template>
          </ElDropdown>
        </template>
      </ArtTable>
    </ElCard>

    <!-- 新建证书 -->
    <ElDrawer v-model="createOpen" title="新建SSL证书" :size="createSize" destroy-on-close>
      <ElForm ref="createFormRef" :model="createForm" :rules="createRules" label-position="top">
        <ElFormItem label="SSL账号" prop="sslAccountId">
          <ElSelect v-model="createForm.sslAccountId" filterable class="w-full" placeholder="请选择SSL账号">
            <ElOption
              v-for="item in enabledSSLAccounts"
              :key="item.id"
              :label="`${item.name} / ${sslProviderNames[item.provider] || item.provider}`"
              :value="item.id"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="域名" prop="domainId">
          <ElSelect v-model="createForm.domainId" filterable class="w-full" placeholder="请选择域名" @change="onDomainChange">
            <ElOption v-for="item in domains" :key="item.id" :label="item.name" :value="item.id" />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="主域名" prop="commonName">
          <ElInput v-model="createForm.commonName">
            <template #prefix><ArtSvgIcon icon="ri:shield-keyhole-line" /></template>
          </ElInput>
        </ElFormItem>
        <ElFormItem label="备用域名" prop="sans">
          <ElInput v-model="createForm.sans" type="textarea" :rows="4" placeholder="每行一个，或使用逗号分隔" />
        </ElFormItem>
        <ElFormItem label="提前续期天数" prop="renewBeforeDay">
          <ElInputNumber v-model="createForm.renewBeforeDay" :min="1" :max="90" :precision="0" controls-position="right" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="createOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="creating" @click="submitCreate">保存</ElButton>
      </template>
    </ElDrawer>

    <!-- 修改提前续期天数 -->
    <ElDialog v-model="renewOpen" title="修改提前续期天数" width="400px" align-center destroy-on-close>
      <ElForm label-position="top" @submit.prevent>
        <ElFormItem label="提前续期天数" required>
          <ElInputNumber v-model="renewDays" :min="1" :max="90" :precision="0" controls-position="right" class="!w-full" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="renewOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="renewSaving" @click="submitRenew">保存</ElButton>
      </template>
    </ElDialog>

    <!-- 自动部署任务 -->
    <ElDrawer v-model="deployOpen" :size="deploySize" destroy-on-close @closed="deployCertificate = null">
      <template #header>
        <div class="flex items-center justify-between gap-3 pr-2">
          <span class="truncate text-base font-medium text-g-900">{{ deployCertificate?.commonName || '' }} 自动部署任务</span>
          <ElButton type="primary" @click="openDeployTask()">
            <ArtSvgIcon icon="ri:add-line" class="mr-1" />添加任务
          </ElButton>
        </div>
      </template>
      <ElTable v-loading="deployLoading" :data="deployTasks" row-key="id" border>
        <ElTableColumn prop="name" label="任务名称" min-width="180" />
        <ElTableColumn label="部署目标" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ targetSummary(row.targets) }}</template>
        </ElTableColumn>
        <ElTableColumn label="目标数" width="80">
          <template #default="{ row }">{{ row.targets?.length || 0 }}</template>
        </ElTableColumn>
        <ElTableColumn label="最近状态" width="100">
          <template #default="{ row }">
            <ElTag :type="statusTag(row.lastRunStatus).type" disable-transitions>{{ statusTag(row.lastRunStatus).text }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="最近执行" width="170">
          <template #default="{ row }">{{ formatDateTime(row.lastRunAt) }}</template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="80">
          <template #default="{ row }">
            <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="230" fixed="right">
          <template #default="{ row }">
            <TableAction
              icon="ri:play-circle-line"
              :disabled="deployCertificate?.status !== 'issued' || row.lastRunStatus === 'running' || row.status === 'disabled'"
              :loading="runningTaskId === row.id"
              @click="runDeployTask(row)"
            >
              执行
            </TableAction>
            <TableAction icon="ri:pencil-line" type="info" @click="openDeployTask(row)">编辑</TableAction>
            <ElPopconfirm title="确认删除这个自动部署任务？" @confirm="removeDeployTask(row)">
              <template #reference>
                <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
              </template>
            </ElPopconfirm>
          </template>
        </ElTableColumn>
        <template #empty>
          <ElEmpty description="还没有自动部署任务" :image-size="72" />
        </template>
      </ElTable>
    </ElDrawer>

    <DeployTaskDrawer
      v-model="deployTaskOpen"
      :task="deployTaskCurrent"
      :deploy-accounts="deployAccounts"
      :fixed-certificate="deployCertificate"
      @saved="loadDeployTasks"
    />

    <!-- 证书详情 -->
    <ElDrawer v-model="detailOpen" :size="detailSize" destroy-on-close>
      <template #header>
        <div class="flex items-center justify-between gap-3 pr-2">
          <span class="text-base font-medium text-g-900">SSL证书详情</span>
          <ElButton :loading="detailUpdating" @click="updateDetail">
            <ArtSvgIcon v-if="!detailUpdating" icon="ri:refresh-line" class="mr-1" />更新
          </ElButton>
        </div>
      </template>
      <template v-if="detail">
        <ElDescriptions :column="1" border>
          <ElDescriptionsItem label="主域名">{{ detail.commonName || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="备用域名">
            <span class="whitespace-pre-wrap">{{ parseSANs(detail.sans).join('\n') || '-' }}</span>
          </ElDescriptionsItem>
          <ElDescriptionsItem label="状态">
            <ElTag :type="certificateStatusTag(detail.status).type" disable-transitions>{{ certificateStatusTag(detail.status).text }}</ElTag>
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="detailLabels.certificateId">
            <span class="break-all">{{ detail.providerCertificateId || '-' }}</span>
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="detailLabels.orderId">
            <span class="break-all">{{ detail.providerOrderId || '-' }}</span>
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="detailLabels.status">{{ detail.providerStatusMsg || detail.providerStatus || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="验证方式">{{ detail.verifyType || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="验证记录">{{ detail.authRecordName || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="验证值">
            <span class="inline-flex items-center gap-1 break-all">
              {{ detail.authRecordValue || '-' }}
              <ElButton v-if="detail.authRecordValue" link type="primary" @click="copyText(detail.authRecordValue, '验证值')">
                <ArtSvgIcon icon="ri:file-copy-line" />
              </ElButton>
            </span>
          </ElDescriptionsItem>
          <ElDescriptionsItem label="签发机构">{{ detail.issuer || '-' }}</ElDescriptionsItem>
          <ElDescriptionsItem label="过期时间">{{ formatDateTime(detail.expiresAt) }}</ElDescriptionsItem>
        </ElDescriptions>
        <template v-if="detailCertPem || detail.keyPem">
          <ElDivider content-position="left">服务器配置证书</ElDivider>
          <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
            <CopyBlock v-if="detailCertPem" title="证书文件" :value="detailCertPem" :pretty="false" max-height="260px" />
            <CopyBlock v-if="detail.keyPem" title="私钥文件" :value="detail.keyPem" :pretty="false" max-height="260px" />
          </div>
          <div class="mt-4 flex justify-end">
            <ElButton @click="downloadZip">
              <ArtSvgIcon icon="ri:download-2-line" class="mr-1" />打包下载
            </ElButton>
          </div>
        </template>
      </template>
    </ElDrawer>

    <!-- 证书日志 -->
    <ElDrawer v-model="logOpen" :title="`${logCertificate?.commonName || ''} 证书日志`" :size="logSize" destroy-on-close>
      <RemoteTable v-if="logCertificate" :fetcher="fetchLogs">
        <ElTableColumn label="动作" width="90">
          <template #default="{ row }">{{ certificateActionNames[row.action] || row.action || '-' }}</template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="90">
          <template #default="{ row }">
            <ElTag :type="row.status === 'success' ? 'success' : 'danger'" disable-transitions>{{ row.status === 'success' ? '成功' : '失败' }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn prop="message" label="说明" min-width="240" show-overflow-tooltip />
        <ElTableColumn label="时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="90" fixed="right">
          <template #default="{ row }">
            <TableAction icon="ri:file-search-line" type="info" @click="openLogDetail(row)">详情</TableAction>
          </template>
        </ElTableColumn>
      </RemoteTable>
    </ElDrawer>

    <DetailDrawer v-model="logDetailOpen" title="证书日志详情" :items="logDetailItems" />
  </div>
</template>

<script setup lang="ts">
  import JSZip from 'jszip'
  import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
  import {
    createResource,
    deleteResource,
    getCertificateDetail,
    listAll,
    listResource,
    revokeCertificate,
    runAutoTask,
    submitCertificate,
    updateResource,
    type Row
  } from '@/api/dasslm'
  import CopyBlock from '@/components/dasslm/CopyBlock.vue'
  import DeployTaskDrawer from '@/components/dasslm/DeployTaskDrawer.vue'
  import DetailDrawer from '@/components/dasslm/DetailDrawer.vue'
  import RemoteTable from '@/components/dasslm/RemoteTable.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResourceTable } from '@/components/dasslm/useResourceTable'
  import {
    ACME_PROVIDERS,
    certificateActionNames,
    certificateStatusTag,
    copyText,
    daysUntil,
    formatDateTime,
    parseSANs,
    sslProviderNames,
    statusTag,
    tencentDeployProductNames,
    terminalCertificateStatuses,
    toMap,
    useResponsiveSize,
    type DetailItem,
    type TagType
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmCertificates' })

  const RESOURCE = 'certificates'

  // 证书列表中显示 SSL 账号与域名名称所需的映射
  const sslAccounts = ref<Row[]>([])
  const domains = ref<Row[]>([])
  const sslAccountMap = computed(() => toMap(sslAccounts.value))
  const domainMap = computed(() => toMap(domains.value))
  const enabledSSLAccounts = computed(() => sslAccounts.value.filter((item) => item.status !== 'disabled'))

  async function loadOptions() {
    const [ssl, domainRows] = await Promise.all([listAll('ssl-accounts'), listAll('domains')])
    sslAccounts.value = ssl
    domains.value = domainRows
  }

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
  } = useResourceTable(
    RESOURCE,
    () => [
      { prop: 'commonName', label: '主域名', minWidth: 200 },
      {
        prop: 'sslAccountId',
        label: 'SSL账号',
        minWidth: 180,
        formatter: (row) => {
          const account = sslAccountMap.value[row.sslAccountId]
          return account ? `${account.name} / ${sslProviderNames[account.provider] || account.provider}` : '-'
        }
      },
      { prop: 'domainId', label: '关联域名', minWidth: 160, formatter: (row) => domainMap.value[row.domainId]?.name || row.commonName || '-' },
      { prop: 'issuer', label: '签发机构', minWidth: 140, formatter: (row) => row.issuer || '-' },
      { prop: 'expiresAt', label: '过期时间', width: 170, formatter: (row) => formatDateTime(row.expiresAt) },
      { prop: 'remaining', label: '剩余时间', width: 120, useSlot: true },
      { prop: 'renewBeforeDay', label: '提前续期天数', width: 110 },
      { prop: 'status', label: '状态', width: 90, useSlot: true },
      { prop: 'operation', label: '操作', width: 320, fixed: 'right', useSlot: true }
    ],
    { onLoaded: () => loadOptions().catch(() => undefined) }
  )

  /** 剩余天数标签：30 天内按临近程度加深红色 */
  function remaining(row: Row): { type: TagType; text: string; style?: Record<string, string> } | null {
    const days = daysUntil(row.expiresAt)
    if (days === null) return null
    if (days < 0) return { type: 'danger', text: `已过期 ${Math.abs(days)} 天` }
    if (days === 0) return { type: 'danger', text: '今天到期' }
    if (days <= 30) {
      const opacity = Math.max(0.12, Math.min(0.9, (31 - days) / 30))
      return {
        type: 'danger',
        text: `剩余 ${days} 天`,
        style: {
          backgroundColor: `rgba(245, 108, 108, ${opacity})`,
          borderColor: `rgba(245, 108, 108, ${Math.min(1, opacity + 0.18)})`,
          color: opacity > 0.45 ? '#fff' : 'var(--el-color-danger)'
        }
      }
    }
    return { type: 'success', text: `剩余 ${days} 天` }
  }

  // ---------------- 申请 / 验证流程 ----------------
  const verifyingId = ref<number | null>(null)
  const flowActionId = ref<number | null>(null)
  let pollTimer: ReturnType<typeof setInterval> | null = null

  interface FlowAction {
    text: string
    icon: string
    type: 'primary' | 'success' | 'warning' | 'info'
    disabled?: boolean
    busy?: boolean
  }

  function flowAction(row: Row): FlowAction {
    if (verifyingId.value === row.id || flowActionId.value === row.id) {
      return { text: '验证中', icon: 'ri:loader-4-line', type: 'primary', disabled: true, busy: true }
    }
    switch (row.status) {
      case 'queued':
      case 'applying':
        return { text: '申请中', icon: 'ri:loader-4-line', type: 'primary', disabled: true, busy: true }
      case 'issued':
        return { text: '已签发', icon: 'ri:checkbox-circle-line', type: 'success', disabled: true }
      case 'submitted':
      case 'dns_added':
        return { text: '验证', icon: 'ri:refresh-line', type: 'primary' }
      case 'failed':
      case 'canceled':
        return { text: '重试', icon: 'ri:send-plane-line', type: 'warning' }
      case 'expired':
        return { text: '续期', icon: 'ri:send-plane-line', type: 'warning' }
      case 'pending':
        return { text: '申请', icon: 'ri:send-plane-line', type: 'primary' }
      default:
        if (terminalCertificateStatuses.has(row.status)) return { text: '提交', icon: 'ri:send-plane-line', type: 'info', disabled: true }
        return { text: '提交', icon: 'ri:send-plane-line', type: 'primary' }
    }
  }

  function stopPolling() {
    if (pollTimer) clearInterval(pollTimer)
    pollTimer = null
    verifyingId.value = null
  }

  onBeforeUnmount(stopPolling)

  async function verifyCertificate(row: Row, silent = false) {
    const res = await getCertificateDetail(row.id, { refresh: 1 })
    const next = res.data
    if (detailOpen.value && detail.value?.id === row.id) detail.value = next
    refreshUpdate()
    if (next.status === 'issued') {
      stopPolling()
      detail.value = next
      detailOpen.value = true
      ElMessage.success('证书已签发，证书内容已下载到本地')
      return next
    }
    if (terminalCertificateStatuses.has(next.status)) {
      stopPolling()
      if (!silent) ElMessage.warning(next.providerStatusMsg || '证书已进入终止状态')
      return next
    }
    if (!silent) ElMessage.info(next.providerStatusMsg || '暂未签发，已开始自动验证')
    return next
  }

  async function startPolling(row: Row) {
    stopPolling()
    verifyingId.value = row.id
    const checked = await verifyCertificate(row).catch((error) => {
      stopPolling()
      throw error
    })
    if (terminalCertificateStatuses.has(checked.status)) return
    pollTimer = setInterval(() => {
      verifyCertificate(row, true).catch((error) => {
        stopPolling()
        ElMessage.error(error?.message || '验证失败')
      })
    }, 5000)
  }

  async function submitOrVerify(row: Row) {
    flowActionId.value = row.id
    try {
      if (row.status === 'submitted') {
        await startPolling(row)
        return
      }
      const res = await submitCertificate(row.id)
      const next = res.data || row
      if (next.status === 'queued' || next.status === 'applying') {
        ElMessage.success('已进入后台申请队列')
        refreshUpdate()
        return
      }
      ElMessage.success('已提交，开始自动验证')
      refreshUpdate()
      await startPolling({ ...row, ...next, status: next.status || 'submitted' })
    } catch {
      // 错误提示已由接口层统一弹出
    } finally {
      flowActionId.value = null
    }
  }

  async function handleMore(value: string | number | object, row: Row) {
    const command = String(value)
    if (command === 'renew') {
      renewTarget.value = row
      renewDays.value = Number(row.renewBeforeDay || 30)
      renewOpen.value = true
    } else if (command === 'logs') {
      logCertificate.value = row
      logOpen.value = true
    } else if (command === 'revoke') {
      const ok = await ElMessageBox.confirm(`确认吊销 ${row.commonName} 的证书？`, '吊销证书', {
        type: 'warning',
        confirmButtonText: '吊销',
        cancelButtonText: '取消'
      }).catch(() => false)
      if (!ok) return
      await revokeCertificate(row.id)
      ElMessage.success('已提交吊销，后台将自动验证并检查吊销状态')
      refreshUpdate()
    } else if (command === 'delete') {
      const ok = await ElMessageBox.confirm(`确认删除 ${row.commonName}？`, '删除证书', {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger'
      }).catch(() => false)
      if (!ok) return
      await deleteResource(RESOURCE, row.id)
      ElMessage.success('已删除')
      refreshRemove()
    }
  }

  // ---------------- 新建证书 ----------------
  const createSize = useResponsiveSize(620)
  const createOpen = ref(false)
  const creating = ref(false)
  const createFormRef = ref<FormInstance>()
  const createForm = ref<Row>({})
  const createRules: FormRules = {
    sslAccountId: [{ required: true, message: '请选择SSL账号', trigger: 'change' }],
    domainId: [{ required: true, message: '请选择域名', trigger: 'change' }],
    commonName: [{ required: true, message: '请输入主域名', trigger: 'blur' }],
    renewBeforeDay: [{ required: true, message: '请输入提前续期天数', trigger: 'change' }]
  }

  async function openCreate() {
    await loadOptions()
    createForm.value = { status: 'pending', renewBeforeDay: 30 }
    createOpen.value = true
  }

  function onDomainChange(domainId: number) {
    const name = domainMap.value[domainId]?.name
    if (name) createForm.value.commonName = name
  }

  function normalizeSANs(value: unknown) {
    const items = String(value || '')
      .split(/[\n,]/)
      .map((item) => item.trim())
      .filter(Boolean)
    return JSON.stringify([...new Set(items)])
  }

  async function submitCreate() {
    if (!(await createFormRef.value?.validate().catch(() => false))) return
    creating.value = true
    try {
      const values = createForm.value
      const payload = {
        ...values,
        domainId: Number(values.domainId),
        sslAccountId: Number(values.sslAccountId),
        renewBeforeDay: Number(values.renewBeforeDay || 30),
        sans: normalizeSANs(values.sans),
        commonName: values.commonName || domainMap.value[values.domainId]?.name || ''
      }
      const res = await createResource(RESOURCE, payload)
      const status = res.data?.status
      if (status === 'pending') ElMessage.success('已创建证书任务，可等待后台自动检查或手动开始申请')
      else if (['queued', 'applying', 'dns_added', 'submitted'].includes(status)) ElMessage.success('已有相同证书任务正在进行，已为你打开当前任务')
      else ElMessage.success('证书任务已创建')
      createOpen.value = false
      refreshCreate()
    } finally {
      creating.value = false
    }
  }

  // ---------------- 提前续期天数 ----------------
  const renewOpen = ref(false)
  const renewSaving = ref(false)
  const renewTarget = ref<Row | null>(null)
  const renewDays = ref(30)

  async function submitRenew() {
    if (!renewTarget.value?.id) return
    renewSaving.value = true
    try {
      await updateResource(RESOURCE, renewTarget.value.id, { renewBeforeDay: Number(renewDays.value || 30) })
      ElMessage.success('提前续期天数已保存')
      renewOpen.value = false
      refreshUpdate()
    } finally {
      renewSaving.value = false
    }
  }

  // ---------------- 自动部署 ----------------
  const deploySize = useResponsiveSize(900)
  const deployOpen = ref(false)
  const deployLoading = ref(false)
  const deployCertificate = ref<Row | null>(null)
  const deployTasks = ref<Row[]>([])
  const deployAccounts = ref<Row[]>([])
  const deployAccountMap = computed(() => toMap(deployAccounts.value))
  const deployTaskOpen = ref(false)
  const deployTaskCurrent = ref<Row | null>(null)
  const runningTaskId = ref<number>()

  function targetSummary(targets?: Row[]) {
    if (!targets?.length) return '-'
    return targets
      .map((target) => {
        const account = deployAccountMap.value[target.deployAccountId]
        const name = account ? account.name : `账号#${target.deployAccountId}`
        const product =
          account?.provider === 'tencentcloud' ? ` / ${tencentDeployProductNames[target.productType || 'ssl'] || target.productType}` : ''
        return `${name}${product} / ${target.siteName}`
      })
      .join('；')
  }

  let deploySeq = 0
  async function loadDeployTasks() {
    const certificateId = deployCertificate.value?.id
    if (!certificateId) return
    const seq = ++deploySeq
    deployLoading.value = true
    try {
      const rows = await listAll('tasks', { certificateId })
      if (seq === deploySeq && deployCertificate.value?.id === certificateId) deployTasks.value = rows
    } finally {
      if (seq === deploySeq) deployLoading.value = false
    }
  }

  async function openDeploy(row: Row) {
    deployCertificate.value = row
    deployTasks.value = []
    deployOpen.value = true
    const [accounts] = await Promise.all([listAll('deploy-accounts'), loadDeployTasks()])
    if (deployCertificate.value?.id === row.id) deployAccounts.value = accounts.filter((item) => item.status !== 'disabled')
  }

  function openDeployTask(task?: Row) {
    deployTaskCurrent.value = task || null
    deployTaskOpen.value = true
  }

  async function runDeployTask(task: Row) {
    runningTaskId.value = task.id
    try {
      await runAutoTask(task.id)
      ElMessage.success('已执行自动部署')
      loadDeployTasks()
    } finally {
      runningTaskId.value = undefined
    }
  }

  async function removeDeployTask(task: Row) {
    await deleteResource('tasks', task.id)
    ElMessage.success('已删除')
    loadDeployTasks()
  }

  // ---------------- 证书详情 ----------------
  const detailSize = useResponsiveSize(760)
  const detailOpen = ref(false)
  const detailUpdating = ref(false)
  const detail = ref<Row | null>(null)
  const detailCertPem = computed(() =>
    [detail.value?.certPem, detail.value?.chainPem]
      .map((item) => String(item || '').trim())
      .filter(Boolean)
      .join('\n')
  )
  const detailLabels = computed(() => {
    const provider = sslAccountMap.value[detail.value?.sslAccountId]?.provider
    if (provider === 'tencent_free') return { certificateId: '腾讯云证书ID', orderId: '腾讯云订单ID', status: '腾讯云状态' }
    if (ACME_PROVIDERS.includes(provider)) return { certificateId: '证书URL', orderId: 'ACME订单URL', status: 'ACME状态' }
    return { certificateId: '服务商证书ID', orderId: '服务商订单ID', status: '服务商状态' }
  })

  async function showDetail(row: Row) {
    const res = await getCertificateDetail(row.id)
    detail.value = res.data
    detailOpen.value = true
  }

  async function updateDetail() {
    if (!detail.value?.id) return
    detailUpdating.value = true
    try {
      const res = await getCertificateDetail(detail.value.id, { refresh: 1 })
      detail.value = res.data
      refreshUpdate()
      ElMessage.success('证书信息已更新')
    } finally {
      detailUpdating.value = false
    }
  }

  async function downloadZip() {
    const baseName = String(detail.value?.commonName || 'certificate').replace(/[\\/:*?"<>|\s]+/g, '_')
    const zip = new JSZip()
    if (detailCertPem.value) zip.file(`${baseName}.pem`, detailCertPem.value)
    if (detail.value?.keyPem) zip.file(`${baseName}.key`, detail.value.keyPem)
    const blob = await zip.generateAsync({ type: 'blob' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${baseName}.zip`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  // ---------------- 证书日志 ----------------
  const logSize = useResponsiveSize(860)
  const logOpen = ref(false)
  const logCertificate = ref<Row | null>(null)
  const logDetailOpen = ref(false)
  const logDetail = ref<Row | null>(null)

  async function fetchLogs(page: number, pageSize: number) {
    const res = await listResource('certificate-logs', { page, pageSize, certificateId: logCertificate.value?.id })
    return res.data
  }

  function openLogDetail(row: Row) {
    logDetail.value = row
    logDetailOpen.value = true
  }

  const logDetailItems = computed<DetailItem[]>(() => {
    const row = logDetail.value || {}
    return [
      { label: '证书域名', value: row.commonName },
      { label: '动作', value: certificateActionNames[row.action] || row.action },
      { label: '状态', tag: { type: row.status === 'success' ? 'success' : 'danger', text: row.status === 'success' ? '成功' : '失败' } },
      { label: '说明', value: row.message },
      { label: '请求URL', value: row.requestUrl, block: true },
      { label: '请求方法', value: row.requestMethod },
      { label: '请求头', value: row.requestHeaders, block: true },
      { label: '请求体', value: row.requestBody, block: true },
      { label: '响应体', value: row.responseBody, block: true }
    ]
  })
</script>
