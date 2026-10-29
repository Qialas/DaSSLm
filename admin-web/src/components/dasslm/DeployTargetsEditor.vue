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
              <template v-if="isTencentTarget(target) && tencentTargetDetail(target)"> · {{ tencentTargetDetail(target) }}</template>
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

        <template v-if="isTencent">
          <div class="targets-picker__label">部署到</div>
          <ElSelect v-model="draft.productType" class="w-full" @change="onTencentProductChange">
            <ElOption v-for="item in tencentDeployProducts" :key="item.value" :label="item.label" :value="item.value" />
          </ElSelect>
          <div class="targets-picker__hint">{{ tencentProductDesc }}</div>

          <template v-if="tencentNeedsRegion">
            <div class="targets-picker__label">地域</div>
            <ElSelect v-if="draft.productType === 'waf'" v-model="draft.region" class="w-full" placeholder="选择 WAF 所在地域">
              <ElOption v-for="item in tencentWafRegions" :key="item.value" :label="item.label" :value="item.value" />
            </ElSelect>
            <ElSelect
              v-else
              v-model="draft.region"
              class="w-full"
              filterable
              allow-create
              default-first-option
              placeholder="选择或输入地域，如 ap-guangzhou"
            >
              <ElOption v-for="item in tencentRegions" :key="item.value" :label="`${item.label}（${item.value}）`" :value="item.value" />
            </ElSelect>
          </template>

          <template v-if="draft.productType === 'clb'">
            <div class="targets-picker__label">负载均衡</div>
            <div class="targets-picker__row">
              <ElInput v-model="draft.tcLoadBalancerId" placeholder="负载均衡ID，如 lb-xxxxxxxx" />
              <ElInput v-model="draft.tcListenerId" placeholder="监听器ID，如 lbl-xxxxxxxx" />
            </div>
            <div class="targets-picker__label">域名（可选）</div>
            <ElInput v-model="draft.tcDomain" placeholder="监听器开启 SNI 时填写，未开启留空" />
          </template>
          <template v-else-if="draft.productType === 'apigateway'">
            <div class="targets-picker__label">API 网关服务</div>
            <ElInput v-model="draft.tcServiceId" placeholder="服务ID，如 service-xxxxxxxx" />
            <div class="targets-picker__label">自定义域名</div>
            <ElInput v-model="draft.tcDomain" placeholder="服务绑定的自定义域名" />
          </template>
          <template v-else-if="draft.productType === 'cos'">
            <div class="targets-picker__label">存储桶</div>
            <ElInput v-model="draft.tcBucket" placeholder="存储桶名称，如 examplebucket-1250000000" />
            <div class="targets-picker__label">自定义域名</div>
            <ElInput v-model="draft.tcDomain" placeholder="存储桶绑定的自定义域名" />
          </template>
        </template>

        <template v-if="showSiteSelect">
        <div class="targets-picker__label">
          {{ siteLabel }}
          <ElTooltip v-if="!isTencent" :content="siteHelp" placement="top">
            <ArtSvgIcon icon="ri:question-line" class="targets-picker__help" />
          </ElTooltip>
        </div>
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
        </template>

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
    normalizeTencentProduct,
    tencentDeployProductNames,
    tencentDeployProducts,
    tencentMultiDomainProducts,
    tencentRegionProducts,
    tencentRegions,
    tencentWafRegions,
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
  const tencentProductDesc = computed(
    () => tencentDeployProducts.find((item) => item.value === draft.productType)?.desc || ''
  )
  const tencentNeedsRegion = computed(() => tencentRegionProducts.has(draft.productType))
  /** 腾讯云只有 CDN、EdgeOne、WAF 使用多域名输入，其他产品用各自的字段 */
  const showSiteSelect = computed(() => !isTencent.value || tencentMultiDomainProducts.has(draft.productType))
  const siteLabel = computed(() => {
    if (isTencent.value) return draft.productType === 'cdn' ? '加速域名' : '域名'
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
    if (isTencent.value) return '输入域名后按回车，可添加多个，每个域名会作为一个部署目标。'
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
      if (accountMap.value[target.deployAccountId]?.provider === 'tencentcloud') fillTencentDraft(target)
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
    if (account.provider === 'tencentcloud') {
      draft.productType = draft.productType || 'ssl'
      onTencentProductChange()
    } else {
      draft.productType = ''
    }
    siteOptions.value = []
    searchSites('')
  }

  /** 把已保存的腾讯云实例标识（官方 InstanceIdList 格式）拆回各个输入框 */
  function fillTencentDraft(target: Row) {
    const product = normalizeTencentProduct(target.productType)
    const parts = String(target.resourceId || target.siteName || '').split('|')
    draft.productType = product
    draft.region = target.region || ''
    draft.siteNames = tencentMultiDomainProducts.has(product) ? normalizeSiteNames(parts[0]) : []
    if (product === 'clb') {
      Object.assign(draft, { tcLoadBalancerId: parts[0] || '', tcListenerId: parts[1] || '', tcDomain: parts[2] || '' })
    } else if (product === 'apigateway') {
      Object.assign(draft, { tcServiceId: parts[0] || '', tcDomain: parts[1] || '' })
    } else if (product === 'cos') {
      Object.assign(draft, { tcBucket: parts[1] || '', tcDomain: parts[2] || '' })
      draft.region = draft.region || parts[0] || ''
    }
  }

  function onTencentProductChange() {
    // 切换产品时清空上一个产品的字段，避免把负载均衡ID等带成域名；WAF 地域默认中国大陆
    draft.region = draft.productType === 'waf' ? 'ap-guangzhou' : ''
    Object.assign(draft, { tcLoadBalancerId: '', tcListenerId: '', tcServiceId: '', tcBucket: '', tcDomain: '' })
    draft.siteNames = props.defaultSiteName ? [props.defaultSiteName] : []
  }

  /** 按产品组装官方 InstanceIdList 格式的部署目标；缺少必填项时返回提示文字 */
  function buildTencentTargets(): Row[] | string {
    const product = draft.productType || 'ssl'
    const region = tencentRegionProducts.has(product) ? String(draft.region || '').trim() : ''
    if (tencentRegionProducts.has(product) && !region) return '请选择地域'
    const domain = String(draft.tcDomain || '').trim()
    const base = { productType: product, region, resourceName: '', extraConfig: '' }
    if (product === 'ssl') {
      return [{ ...base, siteName: props.defaultSiteName || '证书管理', resourceId: '' }]
    }
    if (tencentMultiDomainProducts.has(product)) {
      const domains = normalizeSiteNames(draft.siteNames)
      if (!domains.length) return `请输入${siteLabel.value}`
      return domains.map((name) => ({ ...base, siteName: name, resourceId: name }))
    }
    if (product === 'clb') {
      const lb = String(draft.tcLoadBalancerId || '').trim()
      const listener = String(draft.tcListenerId || '').trim()
      if (!lb || !listener) return '请填写负载均衡ID和监听器ID'
      return [{ ...base, siteName: domain || lb, resourceId: [lb, listener, domain].filter(Boolean).join('|') }]
    }
    if (product === 'apigateway') {
      const service = String(draft.tcServiceId || '').trim()
      if (!service || !domain) return '请填写服务ID和自定义域名'
      return [{ ...base, siteName: domain, resourceId: `${service}|${domain}` }]
    }
    if (product === 'cos') {
      const bucket = String(draft.tcBucket || '').trim()
      if (!bucket || !domain) return '请填写存储桶和自定义域名'
      return [{ ...base, siteName: domain, resourceId: `${region}|${bucket}|${domain}` }]
    }
    return '不支持的腾讯云产品'
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
    const common = {
      deployAccountId: Number(draft.deployAccountId),
      status: draft.status || 'enabled',
      remark: draft.remark || ''
    }
    let nextTargets: Row[]
    if (isTencent.value) {
      const built = buildTencentTargets()
      if (typeof built === 'string') {
        ElMessage.warning(built)
        return
      }
      nextTargets = built.map((item) => ({ ...common, ...item }))
    } else {
      const siteNames = normalizeSiteNames(draft.siteNames?.length ? draft.siteNames : draft.siteName || props.defaultSiteName)
      if (!siteNames.length) {
        ElMessage.warning(`请输入${siteLabel.value}`)
        return
      }
      const productType = isBTDocker.value ? 'docker' : ''
      nextTargets = siteNames.map((siteName) => ({
        ...common,
        siteName,
        productType,
        region: '',
        resourceId: '',
        resourceName: '',
        extraConfig: ''
      }))
    }
    const rows = targets.value.filter((_, index) => index !== editingIndex.value)
    const targetKey = (item: Row) =>
      `${Number(item.deployAccountId)}:${item.productType || ''}:${item.resourceId || item.siteName}`
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

  /** 腾讯云目标的资源说明，把官方实例标识拆成可读文字 */
  function tencentTargetDetail(target: Row) {
    const product = normalizeTencentProduct(target.productType)
    const parts = String(target.resourceId || '').split('|')
    const regionName = (value?: string) =>
      [...tencentRegions, ...tencentWafRegions].find((item) => item.value === value)?.label || value || ''
    const details: string[] = []
    if (product === 'clb') details.push(`负载均衡 ${parts[0] || '-'} / 监听器 ${parts[1] || '-'}`)
    if (product === 'apigateway') details.push(`服务 ${parts[0] || '-'}`)
    if (product === 'cos') details.push(`存储桶 ${parts[1] || '-'}`)
    if (product === 'waf') details.push(regionName(target.region))
    else if (target.region) details.push(regionName(target.region))
    return details.filter(Boolean).join(' · ')
  }

  function targetTypeLabel(target: Row) {
    const provider = accountMap.value[target.deployAccountId]?.provider
    if (provider === 'tencentcloud') {
      return tencentDeployProductNames[normalizeTencentProduct(target.productType)] || target.productType
    }
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
