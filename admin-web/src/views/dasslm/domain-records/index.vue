<!-- DNS 解析记录 -->
<template>
  <div class="art-full-height">
    <div class="art-card flex-cb gap-3 px-4 py-3 max-sm:flex-col max-sm:items-start">
      <div class="flex-c min-w-0 gap-3">
        <ElButton @click="router.push('/domains')">
          <ArtSvgIcon icon="ri:arrow-left-line" class="mr-1" />返回
        </ElButton>
        <ProviderIcon :provider="domain?.dnsProvider" size="md" />
        <div class="min-w-0">
          <div class="truncate text-base font-medium text-g-900">{{ domain?.name || 'DNS记录' }}</div>
          <div class="text-xs text-g-500">解析记录</div>
        </div>
      </div>
    </div>

    <ElCard class="art-table-card" shadow="never">
      <ArtTableHeader :loading="loading" layout="refresh,size,fullscreen" @refresh="loadRecords">
        <template #left>
          <ElSpace wrap>
            <ElButton type="primary" v-ripple @click="openRecordForm()">
              <ArtSvgIcon icon="ri:add-line" class="mr-1" />新建记录
            </ElButton>
            <ElDropdown trigger="click" :disabled="!selected.length" @command="handleBatch">
              <ElButton :disabled="!selected.length">
                批量操作{{ selected.length ? `（${selected.length}）` : '' }}
                <ArtSvgIcon icon="ri:arrow-down-s-line" class="ml-1" />
              </ElButton>
              <template #dropdown>
                <ElDropdownMenu>
                  <ElDropdownItem command="enable">批量启用</ElDropdownItem>
                  <ElDropdownItem command="disable">批量暂停</ElDropdownItem>
                  <ElDropdownItem command="edit">批量修改</ElDropdownItem>
                  <ElDropdownItem command="delete" divided class="!text-danger">批量删除</ElDropdownItem>
                </ElDropdownMenu>
              </template>
            </ElDropdown>
          </ElSpace>
        </template>
      </ArtTableHeader>

      <ArtTable
        ref="tableRef"
        row-key="id"
        :loading="loading"
        :data="pageRows"
        :columns="columns"
        :pagination="pagination"
        @selection-change="onSelectionChange"
        @pagination:size-change="onSizeChange"
        @pagination:current-change="onPageChange"
      >
        <template #rr="{ row }">
          <span class="font-medium text-g-900">{{ row.rr || '-' }}</span>
        </template>
        <template #type="{ row }">
          <ElTag type="primary" disable-transitions>{{ row.type || '-' }}</ElTag>
        </template>
        <template #proxied="{ row }">
          <ElTag
            class="cursor-pointer"
            :type="row.proxied ? 'warning' : 'info'"
            disable-transitions
            @click="toggleProxied(row)"
          >
            <ArtSvgIcon icon="ri:cloud-line" class="mr-1" />{{ row.proxied ? '已开启' : '未开启' }}
          </ElTag>
        </template>
        <template #status="{ row }">
          <ElTag
            class="cursor-pointer"
            :type="row.status === 'disabled' ? 'info' : 'success'"
            disable-transitions
            @click="toggleStatus(row)"
          >
            {{ row.status === 'disabled' ? '暂停' : '启用' }}
          </ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction icon="ri:pencil-line" @click="openRecordForm(row)">编辑</TableAction>
          <ElPopconfirm title="确认删除？" @confirm="removeRecord(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ArtTable>
    </ElCard>

    <!-- 新建 / 编辑记录 -->
    <ElDialog v-model="recordOpen" :title="`${currentRecord ? '编辑' : '新建'}DNS记录`" :width="dialogWidth" align-center destroy-on-close>
      <ElForm ref="recordFormRef" :model="recordForm" :rules="recordRules" label-position="top">
        <div class="grid grid-cols-2 gap-x-3 max-sm:grid-cols-1">
          <ElFormItem label="主机记录" prop="rr">
            <ElInput v-model="recordForm.rr" />
          </ElFormItem>
          <ElFormItem label="记录类型" prop="type">
            <ElSelect v-model="recordForm.type">
              <ElOption v-for="type in recordTypes" :key="type" :label="type" :value="type" />
            </ElSelect>
          </ElFormItem>
        </div>
        <ElFormItem label="记录值" prop="value">
          <ElInput v-model="recordForm.value" />
        </ElFormItem>
        <ElFormItem v-if="showLine" label="解析线路" prop="line">
          <ElInput v-model="recordForm.line" />
        </ElFormItem>
        <div class="grid grid-cols-2 gap-x-3 max-sm:grid-cols-1">
          <ElFormItem label="TTL" prop="ttl">
            <ElInputNumber v-model="recordForm.ttl" :min="1" :precision="0" controls-position="right" class="!w-full" />
          </ElFormItem>
          <ElFormItem label="优先级" prop="priority">
            <ElInputNumber v-model="recordForm.priority" :min="0" :precision="0" controls-position="right" class="!w-full" />
          </ElFormItem>
          <ElFormItem v-if="isCloudflare" label="Cloudflare 代理" prop="proxied">
            <ElSelect v-model="recordForm.proxied">
              <ElOption label="开启" :value="true" />
              <ElOption label="关闭" :value="false" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="状态" prop="status">
            <ElSelect v-model="recordForm.status">
              <ElOption label="启用" value="enabled" />
              <ElOption label="暂停" value="disabled" />
            </ElSelect>
          </ElFormItem>
        </div>
        <ElFormItem label="备注" prop="remark">
          <ElInput v-model="recordForm.remark" type="textarea" :rows="3" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="recordOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="submitRecord">保存</ElButton>
      </template>
    </ElDialog>

    <!-- 批量修改 -->
    <ElDialog v-model="batchOpen" :title="`批量修改 ${selected.length} 条解析记录`" :width="dialogWidth" align-center destroy-on-close>
      <ElAlert title="只会修改填写了的字段，留空的字段保持不变" type="info" :closable="false" show-icon class="!mb-4" />
      <ElForm :model="batchForm" label-position="top">
        <div class="grid grid-cols-2 gap-x-3 max-sm:grid-cols-1">
          <ElFormItem label="记录类型">
            <ElSelect v-model="batchForm.type" clearable placeholder="不修改">
              <ElOption v-for="type in recordTypes" :key="type" :label="type" :value="type" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem v-if="showLine" label="解析线路">
            <ElInput v-model="batchForm.line" placeholder="不修改" />
          </ElFormItem>
        </div>
        <ElFormItem label="记录值">
          <ElInput v-model="batchForm.value" placeholder="不修改" />
        </ElFormItem>
        <div class="grid grid-cols-2 gap-x-3 max-sm:grid-cols-1">
          <ElFormItem label="TTL">
            <ElInputNumber v-model="batchForm.ttl" :min="1" :precision="0" controls-position="right" class="!w-full" placeholder="不修改" />
          </ElFormItem>
          <ElFormItem label="优先级">
            <ElInputNumber v-model="batchForm.priority" :min="0" :precision="0" controls-position="right" class="!w-full" placeholder="不修改" />
          </ElFormItem>
          <ElFormItem v-if="isCloudflare" label="Cloudflare 代理">
            <ElSelect v-model="batchForm.proxied" clearable placeholder="不修改">
              <ElOption label="开启" :value="true" />
              <ElOption label="关闭" :value="false" />
            </ElSelect>
          </ElFormItem>
        </div>
        <ElFormItem label="备注">
          <ElInput v-model="batchForm.remark" type="textarea" :rows="3" placeholder="不修改" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="batchOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="submitBatchEdit">保存</ElButton>
      </template>
    </ElDialog>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
  import {
    createDomainRecord,
    deleteDomainRecord,
    getResource,
    listDomainRecords,
    updateDomainRecord,
    type Row
  } from '@/api/dasslm'
  import ProviderIcon from '@/components/dasslm/ProviderIcon.vue'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResponsiveSize } from '@/components/dasslm/meta'
  import type { ColumnOption } from '@/types/component'

  defineOptions({ name: 'DaSSLmDomainRecords' })

  const route = useRoute()
  const router = useRouter()
  const domainId = computed(() => String(route.params.id || ''))

  const domain = ref<Row | null>(null)
  const records = ref<Row[]>([])
  const loading = ref(false)
  const selected = ref<Row[]>([])
  const tableRef = ref<any>()
  const pagination = reactive({ current: 1, size: 10, total: 0 })

  const isCloudflare = computed(() => domain.value?.dnsProvider === 'cloudflare')
  const isTencent = computed(() => ['tencentcloud', 'dnspod', 'tencent_eo'].includes(domain.value?.dnsProvider))
  const showLine = computed(() => !isCloudflare.value && !isTencent.value)
  const recordTypes = computed(() =>
    isTencent.value ? ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'CAA'] : ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'CAA']
  )

  const columns = computed<ColumnOption[]>(() =>
    [
      { type: 'selection', width: 48, reserveSelection: true },
      { prop: 'rr', label: '主机记录', minWidth: 130, useSlot: true },
      { prop: 'type', label: '类型', width: 90, useSlot: true },
      { prop: 'value', label: '记录值', minWidth: 240, showOverflowTooltip: true, formatter: (row: Row) => row.value || '-' },
      showLine.value ? { prop: 'line', label: '线路', width: 120, formatter: (row: Row) => row.line || '-' } : null,
      isCloudflare.value ? { prop: 'proxied', label: '代理', width: 110, useSlot: true } : null,
      { prop: 'ttl', label: 'TTL', width: 90, formatter: (row: Row) => row.ttl || '-' },
      { prop: 'priority', label: '优先级', width: 90, formatter: (row: Row) => row.priority || '-' },
      { prop: 'remark', label: '备注', minWidth: 160, showOverflowTooltip: true, formatter: (row: Row) => row.remark || '-' },
      { prop: 'status', label: '状态', width: 90, useSlot: true },
      { prop: 'operation', label: '操作', width: 160, fixed: 'right', useSlot: true }
    ].filter(Boolean) as ColumnOption[]
  )

  const pageRows = computed(() => {
    const start = (pagination.current - 1) * pagination.size
    return records.value.slice(start, start + pagination.size)
  })

  async function loadDomain() {
    if (!domainId.value) return
    const res = await getResource('domains', domainId.value)
    domain.value = res.data
  }

  async function loadRecords() {
    if (!domainId.value) return
    loading.value = true
    try {
      const res = await listDomainRecords(domainId.value)
      records.value = res.data?.items || []
      pagination.total = records.value.length
      // 已不存在的记录从勾选中移除
      const ids = new Set(records.value.map((row) => row.id))
      selected.value = selected.value.filter((row) => ids.has(row.id))
      const maxPage = Math.max(1, Math.ceil(pagination.total / pagination.size))
      if (pagination.current > maxPage) pagination.current = maxPage
    } finally {
      loading.value = false
    }
  }

  function onPageChange(page: number) {
    pagination.current = page
  }

  function onSizeChange(size: number) {
    pagination.size = size
    pagination.current = 1
  }

  function onSelectionChange(rows: Row[]) {
    selected.value = rows
  }

  function clearSelection() {
    selected.value = []
    tableRef.value?.elTableRef?.clearSelection()
  }

  watch(
    domainId,
    () => {
      domain.value = null
      records.value = []
      pagination.current = 1
      clearSelection()
      loadDomain()
      loadRecords()
    },
    { immediate: true }
  )

  /** 按服务商裁剪字段，去掉只读字段 */
  function buildPayload(row: Row, patch: Row = {}) {
    const payload: Row = { ...row, ...patch }
    if (isCloudflare.value) delete payload.line
    else delete payload.proxied
    if (isTencent.value) delete payload.line
    ;['id', 'domainId', 'providerRecordId', 'createdAt', 'updatedAt'].forEach((key) => delete payload[key])
    return payload
  }

  async function patchRecord(row: Row, patch: Row, successText: string) {
    await updateDomainRecord(domainId.value, row.id, buildPayload(row, patch))
    ElMessage.success(successText)
    loadRecords()
  }

  function toggleStatus(row: Row) {
    const next = row.status === 'disabled' ? 'enabled' : 'disabled'
    patchRecord(row, { status: next }, next === 'disabled' ? '已暂停' : '已启用').catch(() => undefined)
  }

  function toggleProxied(row: Row) {
    const next = !row.proxied
    patchRecord(row, { proxied: next }, next ? 'Cloudflare 代理已开启' : 'Cloudflare 代理已关闭').catch(() => undefined)
  }

  async function removeRecord(row: Row) {
    await deleteDomainRecord(domainId.value, row.id)
    ElMessage.success('已删除')
    loadRecords()
  }

  // ---------------- 批量操作 ----------------
  async function runBatchUpdate(patch: Row, successText: string) {
    // 用最新的记录数据构造请求，避免覆盖勾选后单独做过的修改
    const latest = new Map(records.value.map((row) => [row.id, row]))
    const rows = selected.value.map((row) => latest.get(row.id) || row)
    if (!rows.length) return
    const results = await Promise.allSettled(rows.map((row) => updateDomainRecord(domainId.value, row.id, buildPayload(row, patch))))
    const failed = results.filter((item) => item.status === 'rejected').length
    if (failed) ElMessage.error(`已处理 ${rows.length - failed} 条，${failed} 条失败`)
    else ElMessage.success(successText)
    clearSelection()
    loadRecords()
  }

  async function handleBatch(value: string | number | object) {
    const command = String(value)
    const count = selected.value.length
    if (!count) return
    if (command === 'edit') {
      batchForm.value = {}
      batchOpen.value = true
      return
    }
    if (command === 'delete') {
      const ok = await ElMessageBox.confirm(`确认删除选中的 ${count} 条解析记录？`, '批量删除', {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger'
      }).catch(() => false)
      if (!ok) return
      const ids = selected.value.map((row) => row.id)
      const results = await Promise.allSettled(ids.map((id) => deleteDomainRecord(domainId.value, id)))
      const failed = results.filter((item) => item.status === 'rejected').length
      if (failed) ElMessage.error(`已删除 ${count - failed} 条，${failed} 条删除失败`)
      else ElMessage.success(`已删除 ${count} 条解析记录`)
      clearSelection()
      loadRecords()
      return
    }
    const status = command === 'disable' ? 'disabled' : 'enabled'
    const text = status === 'disabled' ? '暂停' : '启用'
    const ok = await ElMessageBox.confirm(`确认${text}选中的 ${count} 条解析记录？`, `批量${text}`, {
      confirmButtonText: text,
      cancelButtonText: '取消'
    }).catch(() => false)
    if (ok) runBatchUpdate({ status }, `已${text} ${count} 条解析记录`)
  }

  const batchOpen = ref(false)
  const batchForm = ref<Row>({})

  async function submitBatchEdit() {
    const patch = Object.fromEntries(
      Object.entries(batchForm.value).filter(([, value]) => value !== undefined && value !== null && value !== '')
    )
    if (!Object.keys(patch).length) {
      ElMessage.warning('请至少填写一个需要修改的字段')
      return
    }
    saving.value = true
    try {
      await runBatchUpdate(patch, `已修改 ${selected.value.length} 条解析记录`)
      batchOpen.value = false
    } finally {
      saving.value = false
    }
  }

  // ---------------- 新建 / 编辑 ----------------
  const dialogWidth = useResponsiveSize(560)
  const recordOpen = ref(false)
  const saving = ref(false)
  const currentRecord = ref<Row | null>(null)
  const recordFormRef = ref<FormInstance>()
  const recordForm = ref<Row>({})
  const recordRules: FormRules = {
    rr: [{ required: true, message: '请输入主机记录', trigger: 'blur' }],
    type: [{ required: true, message: '请选择记录类型', trigger: 'change' }],
    value: [{ required: true, message: '请输入记录值', trigger: 'blur' }]
  }

  function openRecordForm(row?: Row) {
    currentRecord.value = row || null
    recordForm.value = {
      rr: '@',
      type: 'A',
      line: showLine.value ? 'default' : undefined,
      ttl: 600,
      proxied: false,
      status: 'enabled',
      ...(row || {})
    }
    recordOpen.value = true
  }

  async function submitRecord() {
    if (!(await recordFormRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      const payload = buildPayload(recordForm.value)
      if (currentRecord.value?.id) await updateDomainRecord(domainId.value, currentRecord.value.id, payload)
      else await createDomainRecord(domainId.value, payload)
      ElMessage.success('保存成功')
      recordOpen.value = false
      loadRecords()
    } finally {
      saving.value = false
    }
  }
</script>
