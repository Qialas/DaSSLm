<!-- 自动任务：证书自动部署 -->
<template>
  <div class="art-full-height">
    <ElCard class="art-table-card" shadow="never">
      <ArtTableHeader v-model:columns="columnChecks" :loading="loading" @refresh="refreshData">
        <template #left>
          <ElButton type="primary" v-ripple @click="openForm()">
            <ArtSvgIcon icon="ri:add-line" class="mr-1" />新建自动部署
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
        <template #lastRunStatus="{ row }">
          <ElTag :type="statusTag(row.lastRunStatus).type" disable-transitions>{{ statusTag(row.lastRunStatus).text }}</ElTag>
        </template>
        <template #status="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction
            icon="ri:play-circle-line"
            :disabled="row.status === 'disabled' || row.lastRunStatus === 'running'"
            :loading="runningId === row.id"
            @click="runTask(row)"
          >
            执行
          </TableAction>
          <TableAction icon="ri:pencil-line" @click="openForm(row)">编辑</TableAction>
          <ElPopconfirm title="确认删除？" @confirm="remove(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ArtTable>
    </ElCard>

    <DeployTaskDrawer
      v-model="formOpen"
      :task="current"
      :deploy-accounts="enabledDeployAccounts"
      :certificates="certificates"
      :ssl-account-map="sslAccountMap"
      @saved="current ? refreshUpdate() : refreshCreate()"
    />
  </div>
</template>

<script setup lang="ts">
  import { ElMessage } from 'element-plus'
  import { deleteResource, listAll, runAutoTask, type Row } from '@/api/dasslm'
  import DeployTaskDrawer from '@/components/dasslm/DeployTaskDrawer.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResourceTable } from '@/components/dasslm/useResourceTable'
  import { formatDateTime, statusTag, tencentDeployProductNames, toMap } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmTasks' })

  const RESOURCE = 'tasks'

  const deployAccounts = ref<Row[]>([])
  const certificates = ref<Row[]>([])
  const sslAccounts = ref<Row[]>([])
  const enabledDeployAccounts = computed(() => deployAccounts.value.filter((item) => item.status !== 'disabled'))
  const deployAccountMap = computed(() => toMap(deployAccounts.value))
  const certificateMap = computed(() => toMap(certificates.value))
  const sslAccountMap = computed(() => toMap(sslAccounts.value))

  function targetSummary(targets?: Row[]) {
    if (!targets?.length) return '-'
    return targets
      .map((target) => {
        const account = deployAccountMap.value[target.deployAccountId]
        const name = account ? account.name : `账号#${target.deployAccountId}`
        const product =
          account?.provider === 'tencentcloud'
            ? ` / ${tencentDeployProductNames[target.productType || 'ssl'] || target.productType}`
            : ''
        return `${name}${product} / ${target.siteName}`
      })
      .join('；')
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
  } = useResourceTable(RESOURCE, () => [
    { prop: 'name', label: '任务名称', minWidth: 180 },
    { prop: 'certificateId', label: 'SSL证书', minWidth: 180, formatter: (row) => certificateMap.value[row.certificateId]?.commonName || '-' },
    { prop: 'targets', label: '部署目标', minWidth: 240, showOverflowTooltip: true, formatter: (row) => targetSummary(row.targets) },
    { prop: 'targetCount', label: '目标数', width: 80, formatter: (row) => row.targets?.length || 0 },
    { prop: 'lastRunStatus', label: '最近状态', width: 100, useSlot: true },
    { prop: 'lastRunAt', label: '最近执行', width: 170, formatter: (row) => formatDateTime(row.lastRunAt) },
    { prop: 'status', label: '状态', width: 90, useSlot: true },
    { prop: 'remark', label: '备注', minWidth: 140, showOverflowTooltip: true },
    { prop: 'operation', label: '操作', width: 230, fixed: 'right', useSlot: true }
  ])

  /** 加载证书、部署账号、SSL 账号，用于显示名称和表单选择 */
  async function loadOptions() {
    const [deploy, certs, ssl] = await Promise.all([
      listAll('deploy-accounts'),
      listAll('certificates'),
      listAll('ssl-accounts')
    ])
    deployAccounts.value = deploy
    certificates.value = certs
    sslAccounts.value = ssl
  }

  onMounted(() => {
    loadOptions().catch(() => undefined)
  })

  const runningId = ref<number>()
  async function runTask(row: Row) {
    runningId.value = row.id
    try {
      await runAutoTask(row.id)
      ElMessage.success('已执行自动部署任务')
      refreshUpdate()
    } finally {
      runningId.value = undefined
    }
  }

  async function remove(row: Row) {
    await deleteResource(RESOURCE, row.id)
    ElMessage.success('已删除')
    refreshRemove()
  }

  const formOpen = ref(false)
  const current = ref<Row | null>(null)

  async function openForm(row?: Row) {
    await loadOptions()
    current.value = row || null
    formOpen.value = true
  }
</script>
