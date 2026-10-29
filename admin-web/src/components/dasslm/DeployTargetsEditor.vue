<!-- 自动部署任务的部署目标编辑：目标列表 + 添加/编辑目标的二级抽屉 -->
<template>
  <div class="targets-panel">
    <div v-if="targets.length" class="targets-panel__list">
      <div
        v-for="(target, index) in targets"
        :key="`${target.deployAccountId}-${target.productType || ''}-${target.siteName}-${index}`"
        class="target-row"
        :class="{ 'is-disabled': (target.status || 'enabled') !== 'enabled' }"
      >
        <ProviderIcon :provider="brandProvider(accountMap[target.deployAccountId]?.provider)" size="md" boxed />
        <div class="target-row__main">
          <div class="target-row__title">
            <span class="truncate" :title="target.siteName">{{ target.siteName || '-' }}</span>
            <ElTag v-if="targetTypeLabel(target)" size="small" effect="plain" disable-transitions>{{ targetTypeLabel(target) }}</ElTag>
            <ElTag v-if="(target.status || 'enabled') !== 'enabled'" size="small" type="info" disable-transitions>停用</ElTag>
          </div>
          <div class="target-row__sub">
            <span class="truncate">
              {{ accountMap[target.deployAccountId]?.name || `账号#${target.deployAccountId}` }} ·
              {{ deployProviderLabel(accountMap[target.deployAccountId]?.provider) }}
              <template v-if="isTencentTarget(target) && target.resourceId && target.resourceId !== target.siteName">
                · {{ target.resourceId }}
              </template>
              <template v-if="target.remark"> · {{ target.remark }}</template>
            </span>
          </div>
        </div>
        <div class="target-row__actions">
          <ElTooltip content="编辑" placement="top">
            <ElButton text circle size="small" aria-label="编辑部署目标" @click="openPicker(target, index)">
              <ArtSvgIcon icon="ri:pencil-line" />
            </ElButton>
          </ElTooltip>
          <ElPopconfirm title="确认移除这个部署目标？" @confirm="removeTarget(index)">
            <template #reference>
              <ElButton text circle size="small" type="danger" aria-label="删除部署目标">
                <ArtSvgIcon icon="ri:delete-bin-5-line" />
              </ElButton>
            </template>
          </ElPopconfirm>
        </div>
      </div>
    </div>
    <div v-else class="targets-panel__empty">
      <ArtSvgIcon icon="ri:server-line" class="targets-panel__empty-icon" />
      <span>还没有部署目标，添加后证书更新时会自动部署过去</span>
    </div>
    <ElButton class="targets-panel__add" plain @click="openPicker()">
      <ArtSvgIcon icon="ri:add-line" class="mr-1" />添加部署目标
    </ElButton>

    <ElDrawer
      v-model="pickerOpen"
      :title="editingIndex === null ? '添加部署目标' : '编辑部署目标'"
      :size="pickerSize"
      append-to-body
      destroy-on-close
      @closed="resetDraft"
    >
      <div class="targets-picker">
        <div class="targets-picker__label">部署账号</div>
        <DeployAccountPicker v-model="draft.deployAccountId" :accounts="deployAccounts" @change="onAccountChange" />

        <template v-if="isBTPanel">
          <div class="targets-picker__label">部署类型</div>
          <ElRadioGroup v-model="draft.btType" @change="onBTTypeChange">
            <ElRadioButton v-for="item in btDeployTypes" :key="item.value" :value="item.value">{{ item.label }}</ElRadioButton>
          </ElRadioGroup>
        </template>

        <div class="targets-picker__label">
          {{ siteLabel }}
          <ElTooltip :content="siteHelp" placement="top">
            <ArtSvgIcon icon="ri:question-line" class="targets-picker__help" />
          </ElTooltip>
        </div>
        <template v-if="isTencent">
          <div class="targets-picker__sublabel">部署到的腾讯云产品</div>
          <ElSelect v-model="draft.productType" class="w-full">
            <ElOption v-for="item in tencentDeployProducts" :key="item.value" :label="item.label" :value="item.value" />
          </ElSelect>
          <div class="targets-picker__row">
            <ElInput v-model="draft.resourceId" placeholder="资源ID / 域名 / 实例标识" />
            <ElInput v-model="draft.region" placeholder="地域（可选），如 ap-guangzhou" />
          </div>
          <div class="targets-picker__sublabel">站点/服务名称</div>
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
          :no-data-text="siteNoDataText"
          @visible-change="onSiteDropdown"
          @change="normalizeDraftSites"
        >
          <ElOption v-for="site in siteOptions" :key="site.value" :label="site.value" :value="site.value">
            <span class="targets-picker__site">{{ site.value }}</span>
            <span v-if="site.remark" class="targets-picker__site-remark">{{ site.remark }}</span>
          </ElOption>
        </ElSelect>
        <div class="targets-picker__hint">{{ siteHint }}</div>

        <div class="targets-picker__label targets-picker__label--inline">
          <span>启用此目标</span>
          <ElSwitch v-model="draft.status" active-value="enabled" inactive-value="disabled" />
        </div>
        <div class="targets-picker__label">备注</div>
        <ElInput v-model="draft.remark" type="textarea" :autosize="{ minRows: 2, maxRows: 5 }" placeholder="可选" />
      </div>
      <template #footer>
        <ElButton @click="pickerOpen = false">返回</ElButton>
        <ElButton type="primary" @click="saveDraft">{{ editingIndex === null ? '添加' : '保存' }}</ElButton>
      </template>
    </ElDrawer>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage } from 'element-plus'
  import { listDeployAccountSites, type Row } from '@/api/dasslm'
  import DeployAccountPicker from './DeployAccountPicker.vue'
  import ProviderIcon from './ProviderIcon.vue'
  import {
    btDeployTypeNames,
    btDeployTypes,
    deployProviderLabel,
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
  const isBTDocker = computed(() => isBTPanel.value && draft.btType === 'docker')
  const siteLabel = computed(() => {
    if (isBTDocker.value) return 'Docker 网站域名'
    return isBTPanel.value ? '站点名称' : '站点/服务名称'
  })
  const siteHelp = computed(() => {
    if (isTencent.value) return '选择腾讯云产品并填写对应资源ID、域名或实例标识。'
    if (isBTDocker.value) return '宝塔面板没有提供 Docker 网站列表接口，请手动输入 Docker 网站的域名（即面板中显示的网站名），可输入多个。'
    return '宝塔账号可从站点列表选择；其他部署服务可直接输入服务名称或目标标识。'
  })
  const siteHint = computed(() => {
    if (isBTDocker.value) return '宝塔面板不提供 Docker 网站列表，输入域名后按回车，可添加多个。'
    if (isBTPanel.value) return '可从宝塔站点列表中选择，也可直接输入站点名后按回车，可添加多个。'
    return '输入后按回车，可添加多个。'
  })
  const siteNoDataText = computed(() => {
    if (isBTDocker.value) return '请手动输入 Docker 网站域名后回车'
    return isBTPanel.value ? '未找到站点，可直接手动输入' : '可直接手动输入'
  })

  function resetDraft() {
    Object.keys(draft).forEach((key) => delete draft[key])
    Object.assign(draft, { status: 'enabled', siteNames: [], btType: 'site' })
    editingIndex.value = null
    siteOptions.value = []
  }

  function openPicker(target?: Row, index?: number) {
    resetDraft()
    if (target) {
      Object.assign(draft, {
        ...target,
        siteNames: normalizeSiteNames(target.siteName),
        btType: target.productType === 'docker' ? 'docker' : 'site'
      })
      editingIndex.value = index ?? null
    } else {
      Object.assign(draft, {
        deployAccountId: undefined,
        siteNames: props.defaultSiteName ? [props.defaultSiteName] : [],
        btType: 'site',
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

  function onBTTypeChange() {
    siteOptions.value = []
    searchSites('')
  }

  let siteSeq = 0
  async function searchSites(keyword: string) {
    const account = draftAccount.value
    // Docker 网站没有列表接口，只能手动输入
    if (!account || !isBTPanelProvider(account.provider) || isBTDocker.value) {
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
    const productType = tencent ? draft.productType || 'ssl' : isBTDocker.value ? 'docker' : ''
    const nextTargets = siteNames.map((siteName) => ({
      deployAccountId: Number(draft.deployAccountId),
      siteName,
      productType,
      region: tencent ? draft.region || '' : '',
      resourceId: tencent ? draft.resourceId || siteName : '',
      resourceName: tencent ? draft.resourceName || '' : '',
      extraConfig: tencent ? draft.extraConfig || '' : '',
      status: draft.status || 'enabled',
      remark: draft.remark || ''
    }))
    const rows = targets.value.filter((_, index) => index !== editingIndex.value)
    const targetKey = (item: Row) => `${Number(item.deployAccountId)}:${item.productType || ''}:${item.siteName}`
    const existing = new Set(rows.map(targetKey))
    const unique = nextTargets.filter((item) => {
      const key = targetKey(item)
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

  const brandProvider = (provider?: string) => (provider === 'baota' ? 'btpanel' : provider)
  const isTencentTarget = (target: Row) => accountMap.value[target.deployAccountId]?.provider === 'tencentcloud'

  function targetTypeLabel(target: Row) {
    const provider = accountMap.value[target.deployAccountId]?.provider
    if (provider === 'tencentcloud') return tencentDeployProductNames[target.productType || 'ssl'] || target.productType
    if (isBTPanelProvider(provider)) return target.productType === 'docker' ? btDeployTypeNames.docker : btDeployTypeNames.site
    return ''
  }

  function removeTarget(index: number) {
    targets.value = targets.value.filter((_, itemIndex) => itemIndex !== index)
  }
</script>

<style scoped lang="scss">
  .targets-panel {
    display: flex;
    flex-direction: column;
    gap: 10px;
    width: 100%;
  }

  .targets-panel__list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .target-row {
    display: flex;
    gap: 12px;
    align-items: center;
    min-width: 0;
    padding: 10px 12px;
    background: var(--art-gray-100);
    border: 1px solid transparent;
    border-radius: 8px;
    transition: border-color 0.2s;

    &:hover {
      border-color: var(--el-color-primary-light-7);
    }

    &.is-disabled {
      opacity: 0.6;
    }
  }

  .target-row__main {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .target-row__title {
    display: flex;
    gap: 6px;
    align-items: center;
    min-width: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--art-gray-900);
  }

  .target-row__sub {
    display: flex;
    min-width: 0;
    font-size: 12px;
    color: var(--art-gray-500);
  }

  .target-row__actions {
    display: flex;
    flex: none;
    align-items: center;

    .el-button + .el-button {
      margin-left: 2px;
    }
  }

  .targets-panel__empty {
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-items: center;
    padding: 20px 12px;
    font-size: 13px;
    color: var(--art-gray-500);
    text-align: center;
    background: var(--art-gray-100);
    border-radius: 8px;
  }

  .targets-panel__empty-icon {
    font-size: 24px;
    color: var(--art-gray-400);
  }

  .targets-panel__add {
    width: 100%;
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
    margin-top: 6px;
    font-size: 14px;
    font-weight: 600;
    color: var(--art-gray-900);
  }

  .targets-picker__label--inline {
    justify-content: space-between;
  }

  .targets-picker__sublabel {
    font-size: 13px;
    color: var(--art-gray-600);
  }

  .targets-picker__row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;

    @media (max-width: 560px) {
      grid-template-columns: 1fr;
    }
  }

  .targets-picker__hint {
    margin-top: -4px;
    font-size: 12px;
    color: var(--art-gray-500);
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
