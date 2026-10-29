<!-- 自动任务：证书自动部署 -->
<template>
  <div class="art-full-height">
    <ElCard class="art-table-card" shadow="never">
      <ArtTableHeader
        v-model:columns="columnChecks"
        :loading="loading"
        :layout="viewMode === 'card' ? 'refresh' : undefined"
        @refresh="refreshData"
      >
        <template #left>
          <div class="flex-c flex-wrap gap-3">
            <ElButton type="primary" v-ripple @click="openForm()">
              <ArtSvgIcon icon="ri:add-line" class="mr-1" />新建自动部署
            </ElButton>
            <ElRadioGroup v-model="viewMode" class="view-switch">
              <ElRadioButton value="card" title="卡片视图">
                <ArtSvgIcon icon="ri:layout-grid-line" class="mr-1" />卡片
              </ElRadioButton>
              <ElRadioButton value="table" title="列表视图">
                <ArtSvgIcon icon="ri:list-check" class="mr-1" />列表
              </ElRadioButton>
            </ElRadioGroup>
          </div>
        </template>
      </ArtTableHeader>

      <div v-if="viewMode === 'card'" v-loading="loading" class="task-cards">
        <div class="task-cards__body">
          <ElEmpty v-if="!loading && !data.length" description="还没有自动部署任务" />
          <div v-else class="task-grid">
            <div v-for="row in data" :key="row.id" class="task-card" :class="{ 'is-disabled': row.status === 'disabled' }">
              <div class="task-card__head">
                <div class="task-card__title" :title="row.name">{{ row.name }}</div>
                <ElTag size="small" :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
              </div>

              <div class="task-card__cert">
                <ArtSvgIcon icon="ri:shield-keyhole-line" class="task-card__cert-icon" />
                <span class="truncate" :title="certificateName(row)">{{ certificateName(row) }}</span>
              </div>

              <div class="task-card__targets">
                <div class="task-card__label">部署目标 · {{ row.targets?.length || 0 }} 个</div>
                <div v-for="(target, index) in (row.targets || []).slice(0, 3)" :key="index" class="task-card__target">
                  <ArtSvgIcon icon="ri:server-line" class="task-card__target-icon" />
                  <span class="truncate" :title="targetLabel(target)">{{ targetLabel(target) }}</span>
                </div>
                <div v-if="(row.targets?.length || 0) > 3" class="task-card__more" :title="targetSummary(row.targets)">
                  还有 {{ row.targets.length - 3 }} 个目标
                </div>
                <div v-if="!row.targets?.length" class="task-card__more">暂无部署目标</div>
              </div>

              <div class="task-card__meta">
                <ElTag v-if="row.lastRunStatus" size="small" effect="plain" :type="statusTag(row.lastRunStatus).type" disable-transitions>
                  {{ statusTag(row.lastRunStatus).text }}
                </ElTag>
                <span>{{ row.lastRunAt ? `最近执行：${formatDateTime(row.lastRunAt)}` : '尚未执行' }}</span>
              </div>
              <div v-if="row.remark" class="task-card__remark" :title="row.remark">{{ row.remark }}</div>

              <div class="task-card__actions">
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
              </div>
            </div>
          </div>
        </div>
        <div class="task-cards__pagination">
          <ElPagination
            background
            :layout="paginationLayout"
            :page-sizes="[10, 20, 30, 50, 100]"
            :total="pagination.total"
            :page-size="pagination.size"
            :current-page="pagination.current"
            :disabled="loading"
            @size-change="handleSizeChange"
            @current-change="handleCurrentChange"
          />
        </div>
      </div>

      <ArtTable
        v-else
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
  import { useWindowSize } from '@vueuse/core'
  import {
    btDeployTypeNames,
    formatDateTime,
    isBTPanelProvider,
    statusTag,
    tencentDeployProductNames,
    toMap
  } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmTasks' })

  const RESOURCE = 'tasks'

  const deployAccounts = ref<Row[]>([])
  const certificates = ref<Row[]>([])
  const sslAccounts = ref<Row[]>([])
  const enabledDeployAccounts = computed(() => deployAccounts.value.filter((item) => item.status !== 'disabled'))
  const deployAccountMap = computed(() => toMap(deployAccounts.value))
  const certificateMap = computed(() => toMap(certificates.value))
  const sslAccountMap = computed(() => toMap(sslAccounts.value))

  // 视图模式：默认卡片，记住用户上次的选择（仅保存在本机浏览器）
  type ViewMode = 'card' | 'table'
  const VIEW_MODE_KEY = 'dasslm.tasks.viewMode'
  function readViewMode(): ViewMode {
    try {
      return localStorage.getItem(VIEW_MODE_KEY) === 'table' ? 'table' : 'card'
    } catch {
      return 'card'
    }
  }
  const viewMode = ref<ViewMode>(readViewMode())
  watch(viewMode, (value) => {
    try {
      localStorage.setItem(VIEW_MODE_KEY, value)
    } catch {
      // 浏览器禁用本地存储时只在当前页面生效
    }
  })

  const { width } = useWindowSize()
  const paginationLayout = computed(() =>
    width.value < 640 ? 'prev, pager, next' : 'total, sizes, prev, pager, next, jumper'
  )

  function certificateName(row: Row) {
    return certificateMap.value[row.certificateId]?.commonName || '-'
  }

  function targetLabel(target: Row) {
    const account = deployAccountMap.value[target.deployAccountId]
    const name = account ? account.name : `账号#${target.deployAccountId}`
    let product = ''
    if (account?.provider === 'tencentcloud') {
      product = ` / ${tencentDeployProductNames[target.productType || 'ssl'] || target.productType}`
    } else if (isBTPanelProvider(account?.provider) && target.productType === 'docker') {
      product = ` / ${btDeployTypeNames.docker}`
    }
    return `${name}${product} / ${target.siteName}`
  }

  function targetSummary(targets?: Row[]) {
    if (!targets?.length) return '-'
    return targets.map(targetLabel).join('；')
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
    { prop: 'certificateId', label: 'SSL证书', minWidth: 180, formatter: (row) => certificateName(row) },
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

<style scoped lang="scss">
  .view-switch :deep(.el-radio-button__inner) {
    display: inline-flex;
    align-items: center;
  }

  .task-cards {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
  }

  .task-cards__body {
    flex: 1;
    min-height: 0;
    padding: 2px;
    margin-top: 12px;
    overflow-y: auto;
  }

  .task-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 16px;

    @media (max-width: 640px) {
      grid-template-columns: 1fr;
    }
  }

  .task-card {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
    padding: 16px;
    background: var(--default-box-color);
    border: 1px solid var(--default-border);
    border-radius: calc(var(--custom-radius) / 2 + 4px);
    transition:
      border-color 0.2s,
      box-shadow 0.2s;

    &:hover {
      border-color: var(--el-color-primary-light-5);
      box-shadow: 0 4px 16px rgb(0 0 0 / 6%);
    }

    &.is-disabled {
      opacity: 0.72;
    }
  }

  .task-card__head {
    display: flex;
    gap: 8px;
    align-items: center;
    justify-content: space-between;
  }

  .task-card__title {
    overflow: hidden;
    font-size: 15px;
    font-weight: 600;
    color: var(--art-gray-900);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .task-card__cert {
    display: flex;
    gap: 6px;
    align-items: center;
    min-width: 0;
    padding: 8px 10px;
    font-size: 13px;
    color: var(--art-gray-800);
    background: var(--art-gray-100);
    border-radius: 6px;
  }

  .task-card__cert-icon {
    flex: none;
    font-size: 16px;
    color: var(--el-color-primary);
  }

  .task-card__targets {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }

  .task-card__label {
    font-size: 12px;
    color: var(--art-gray-500);
  }

  .task-card__target {
    display: flex;
    gap: 6px;
    align-items: center;
    min-width: 0;
    font-size: 13px;
    color: var(--art-gray-700);
  }

  .task-card__target-icon {
    flex: none;
    color: var(--art-gray-500);
  }

  .task-card__more {
    font-size: 12px;
    color: var(--art-gray-500);
  }

  .task-card__meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    font-size: 12px;
    color: var(--art-gray-600);
  }

  .task-card__remark {
    overflow: hidden;
    font-size: 12px;
    color: var(--art-gray-500);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .task-card__actions {
    display: flex;
    gap: 4px;
    align-items: center;
    justify-content: flex-end;
    padding-top: 10px;
    border-top: 1px dashed var(--default-border);
  }

  .task-cards__pagination {
    display: flex;
    justify-content: center;
    padding-top: 12px;
  }
</style>
