<!-- 自动部署任务的部署目标编辑：目标列表 + 添加/编辑目标的二级抽屉 -->
<template>
  <div class="targets-panel">
    <div class="targets-panel__head">已添加 {{ targets.length }} 个部署服务</div>
    <div v-if="targets.length" class="targets-panel__list">
      <div v-for="(target, index) in targets" :key="`${target.deployAccountId}-${target.siteName}-${index}`" class="targets-panel__item">
        <div class="targets-panel__info">
          <strong>{{ accountMap[target.deployAccountId]?.name || `账号#${target.deployAccountId}` }}</strong>
          <span>{{ deployProviderLabel(accountMap[target.deployAccountId]?.provider) }} / {{ target.siteName || '-' }}</span>
          <span v-if="accountMap[target.deployAccountId]?.provider === 'tencentcloud'">
            {{ tencentDeployProductNames[target.productType || 'ssl'] || target.productType || '腾讯云' }} /
            {{ target.resourceId || target.siteName || '-' }}
          </span>
        </div>
        <div class="targets-panel__actions">
          <ElTag size="small" :type="(target.status || 'enabled') === 'enabled' ? 'success' : 'info'" disable-transitions>
            {{ (target.status || 'enabled') === 'enabled' ? '启用' : '停用' }}
          </ElTag>
          <TableAction icon="ri:pencil-line" @click="openPicker(target, index)">编辑</TableAction>
          <TableAction icon="ri:delete-bin-5-line" type="danger" @click="removeTarget(index)">删除</TableAction>
        </div>
      </div>
    </div>
    <ElEmpty v-else description="还没有添加部署服务" :image-size="56" />
    <ElButton class="targets-panel__add" plain @click="openPicker()">
      <ArtSvgIcon icon="ri:add-line" class="mr-1" />添加部署服务
    </ElButton>

    <ElDrawer
      v-model="pickerOpen"
      :title="editingIndex === null ? '添加部署服务' : '编辑部署服务'"
      :size="pickerSize"
      append-to-body
      destroy-on-close
      @closed="resetDraft"
    >
      <div class="targets-picker">
        <div class="targets-picker__label">选择部署账号</div>
        <DeployAccountPicker v-model="draft.deployAccountId" :accounts="deployAccounts" @change="onAccountChange" />

        <div class="targets-picker__label">
          {{ siteLabel }}
          <ElTooltip :content="siteHelp" placement="top">
            <ArtSvgIcon icon="ri:question-line" class="targets-picker__help" />
          </ElTooltip>
        </div>
        <template v-if="isTencent">
          <ElSelect v-model="draft.productType" class="w-full">
            <ElOption v-for="item in tencentDeployProducts" :key="item.value" :label="item.label" :value="item.value" />
          </ElSelect>
          <ElInput v-model="draft.resourceId" placeholder="资源ID / 域名 / 实例标识" />
          <ElInput v-model="draft.region" placeholder="地域，可选，如 ap-guangzhou" />
        </template>
        <ElSelect
          v-model="draft.siteNames"
          class="w-full"
          multiple
          filterable
          allow-create
          default-first-option
          remote
          :reserve-keyword="false"
          :remote-method="searchSites"
          :loading="siteLoading"
          :placeholder="defaultSiteName || siteLabel"
          :no-data-text="isBTPanel ? '未找到站点，可直接手动输入' : '可直接手动输入'"
          @visible-change="onSiteDropdown"
          @change="normalizeDraftSites"
        >
          <ElOption v-for="site in siteOptions" :key="site.value" :label="site.value" :value="site.value">
            <span class="targets-picker__site">{{ site.value }}</span>
            <span v-if="site.remark" class="targets-picker__site-remark">{{ site.remark }}</span>
          </ElOption>
        </ElSelect>
        <ElSelect v-model="draft.status" class="w-full">
          <ElOption v-for="item in enabledStatusOptions" :key="item.value" :label="item.label" :value="item.value" />
        </ElSelect>
        <ElInput v-model="draft.remark" type="textarea" :autosize="{ minRows: 3, maxRows: 6 }" placeholder="备注" />
      </div>
      <template #footer>
        <ElButton @click="pickerOpen = false">返回</ElButton>
        <ElButton type="primary" @click="saveDraft">添加到任务</ElButton>
      </template>
    </ElDrawer>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage } from 'element-plus'
  import { listDeployAccountSites, type Row } from '@/api/dasslm'
  import DeployAccountPicker from './DeployAccountPicker.vue'
  import TableAction from './TableAction.vue'
  import {
    deployProviderLabel,
    enabledStatusOptions,
    isBTPanelProvider,
    normalizeSiteNames,
    tencentDeployProductNames,
    tencentDeployProducts,
    toMap,
    useResponsiveSize
  } from './meta'

  defineOptions({ name: 'DeployTargetsEditor' })

  const props = defineProps<{
    deployAccounts: Row[]
    /** 默认站点名称，一般是证书主域名 */
    defaultSiteName?: string
  }>()
  const targets = defineModel<Row[]>({ default: () => [] })

  const accountMap = computed(() => toMap(props.deployAccounts))
  const pickerOpen = ref(false)
  const pickerSize = useResponsiveSize(560)
  const editingIndex = ref<number | null>(null)
  const draft = reactive<Row>({ status: 'enabled', siteNames: [] })
  const siteOptions = ref<{ value: string; remark: string }[]>([])
  const siteLoading = ref(false)

  const draftAccount = computed(() => (draft.deployAccountId ? accountMap.value[draft.deployAccountId] : undefined))
  const isBTPanel = computed(() => isBTPanelProvider(draftAccount.value?.provider))
  const isTencent = computed(() => draftAccount.value?.provider === 'tencentcloud')
  const siteLabel = computed(() => (isBTPanel.value ? '站点名称' : '站点/服务名称'))
  const siteHelp = computed(() =>
    isTencent.value
      ? '选择腾讯云产品并填写对应资源ID、域名或实例标识。'
      : '宝塔账号可从站点列表选择；其他部署服务可直接输入服务名称或目标标识。'
  )

  function resetDraft() {
    Object.keys(draft).forEach((key) => delete draft[key])
    Object.assign(draft, { status: 'enabled', siteNames: [] })
    editingIndex.value = null
    siteOptions.value = []
  }

  function openPicker(target?: Row, index?: number) {
    resetDraft()
    if (target) {
      Object.assign(draft, { ...target, siteNames: normalizeSiteNames(target.siteName) })
      editingIndex.value = index ?? null
    } else {
      Object.assign(draft, {
        deployAccountId: undefined,
        siteNames: props.defaultSiteName ? [props.defaultSiteName] : [],
        status: 'enabled',
        remark: ''
      })
    }
    pickerOpen.value = true
    if (draft.deployAccountId) searchSites('')
  }

  function onAccountChange(_id: number, account: Row) {
    draft.productType = account.provider === 'tencentcloud' ? draft.productType || 'ssl' : ''
    siteOptions.value = []
    searchSites('')
  }

  let siteSeq = 0
  async function searchSites(keyword: string) {
    const account = draftAccount.value
    if (!account || !isBTPanelProvider(account.provider)) {
      siteOptions.value = []
      return
    }
    const current = ++siteSeq
    siteLoading.value = true
    try {
      const res = await listDeployAccountSites(account.id, { page: 1, pageSize: 100, type: 'all', keyword })
      if (current !== siteSeq) return
      siteOptions.value = (res.data.items || [])
        .filter((item) => item.name)
        .map((item) => ({ value: String(item.name), remark: item.ps || item.remark || '' }))
    } catch {
      if (current === siteSeq) siteOptions.value = []
    } finally {
      if (current === siteSeq) siteLoading.value = false
    }
  }

  function onSiteDropdown(open: boolean) {
    if (open) searchSites('')
  }

  /** 支持一次粘贴多个站点（逗号或换行分隔） */
  function normalizeDraftSites(values: string[]) {
    draft.siteNames = normalizeSiteNames(values.flatMap((item) => String(item).split(/[\n,]/)))
  }

  function saveDraft() {
    if (!draft.deployAccountId) {
      ElMessage.warning('请选择一个部署账号')
      return
    }
    const siteNames = normalizeSiteNames(draft.siteNames?.length ? draft.siteNames : draft.siteName || props.defaultSiteName)
    if (!siteNames.length) {
      ElMessage.warning(`请输入${siteLabel.value}`)
      return
    }
    const tencent = isTencent.value
    const nextTargets = siteNames.map((siteName) => ({
      deployAccountId: Number(draft.deployAccountId),
      siteName,
      productType: tencent ? draft.productType || 'ssl' : '',
      region: tencent ? draft.region || '' : '',
      resourceId: tencent ? draft.resourceId || siteName : '',
      resourceName: tencent ? draft.resourceName || '' : '',
      extraConfig: tencent ? draft.extraConfig || '' : '',
      status: draft.status || 'enabled',
      remark: draft.remark || ''
    }))
    const rows = targets.value.filter((_, index) => index !== editingIndex.value)
    const existing = new Set(rows.map((item) => `${Number(item.deployAccountId)}:${item.siteName}`))
    const unique = nextTargets.filter((item) => {
      const key = `${item.deployAccountId}:${item.siteName}`
      if (existing.has(key)) return false
      existing.add(key)
      return true
    })
    if (!unique.length) {
      ElMessage.warning('部署目标已存在')
      return
    }
    if (editingIndex.value === null) rows.push(...unique)
    else rows.splice(editingIndex.value, 0, ...unique)
    targets.value = rows
    pickerOpen.value = false
  }

  function removeTarget(index: number) {
    targets.value = targets.value.filter((_, itemIndex) => itemIndex !== index)
  }
</script>

<style scoped lang="scss">
  .targets-panel {
    width: 100%;
    padding: 12px;
    background: var(--art-gray-100);
    border: 1px dashed var(--default-border-dashed);
    border-radius: calc(var(--custom-radius) / 2 + 2px);
  }

  .targets-panel__head {
    margin-bottom: 10px;
    font-size: 13px;
    color: var(--art-gray-600);
  }

  .targets-panel__list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .targets-panel__item {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px;
    background: var(--default-box-color);
    border: 1px solid var(--default-border);
    border-radius: 8px;

    @media (max-width: 560px) {
      flex-direction: column;
      align-items: flex-start;
    }
  }

  .targets-panel__info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    font-size: 12px;
    color: var(--art-gray-600);

    strong {
      font-size: 14px;
      color: var(--art-gray-900);
    }

    span {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  .targets-panel__actions {
    display: flex;
    flex: none;
    gap: 6px;
    align-items: center;
  }

  .targets-panel__add {
    width: 100%;
    margin-top: 10px;
    border-style: dashed;
  }

  .targets-picker {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .targets-picker__label {
    display: flex;
    gap: 4px;
    align-items: center;
    margin-top: 4px;
    font-size: 14px;
    font-weight: 600;
    color: var(--art-gray-900);
  }

  .targets-picker__help {
    color: var(--art-gray-500);
    cursor: help;
  }

  .targets-picker__site {
    margin-right: 8px;
  }

  .targets-picker__site-remark {
    font-size: 12px;
    color: var(--art-gray-500);
  }
</style>
