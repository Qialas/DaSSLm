<!-- 仪表盘：资产统计、服务器状态与项目地址 -->
<template>
  <div class="dasslm-dashboard">
    <div class="art-card mb-5 flex-cb gap-4 px-5 py-4 max-sm:mb-4 max-sm:flex-col max-sm:items-start">
      <div class="flex-c gap-3">
        <div class="size-11 flex-cc rounded-xl bg-theme/10">
          <ArtSvgIcon icon="ri:dashboard-line" class="text-xl text-theme" />
        </div>
        <div>
          <h3 class="text-lg font-medium text-g-900">运行工作台</h3>
          <p class="text-sm text-g-600">DNS、SSL 与自动化任务一屏掌握</p>
        </div>
      </div>
      <div class="flex-c gap-3">
        <span v-if="updatedAt" class="text-sm text-g-500">更新于 {{ formatDateTime(updatedAt) }}</span>
        <ElButton type="primary" :loading="loading" @click="load">
          <ArtSvgIcon v-if="!loading" icon="ri:refresh-line" class="mr-1" />刷新
        </ElButton>
      </div>
    </div>

    <ElAlert v-if="error" :title="error" :type="data ? 'warning' : 'error'" show-icon class="!mb-5" @close="error = ''" />

    <div v-loading="loading && !data">
      <ElRow :gutter="20">
        <ElCol v-for="item in assets" :key="item.title" :xs="24" :sm="12" :lg="6">
          <div class="art-card relative mb-5 flex h-32 flex-col justify-center px-5 max-sm:mb-4">
            <span class="text-sm text-g-700">{{ item.title }}</span>
            <ArtCountTo class="mt-2 text-[26px] font-medium" :target="item.value" :duration="1000" />
            <span class="mt-1 text-xs text-g-500">{{ item.detail }}</span>
            <div class="absolute top-0 bottom-0 right-5 m-auto size-12.5 flex-cc rounded-xl bg-theme/10">
              <ArtSvgIcon :icon="item.icon" class="text-xl text-theme" />
            </div>
          </div>
        </ElCol>
      </ElRow>

      <div class="art-card mb-5 p-5 max-sm:mb-4">
        <div class="mb-4 flex-cb">
          <div>
            <h4 class="text-base font-medium text-g-900">服务器状态</h4>
            <p class="text-xs text-g-500">负载、计算与存储占用</p>
          </div>
          <ElTag type="success" effect="light" round disable-transitions>实时采样</ElTag>
        </div>
        <ElRow :gutter="16">
          <ElCol v-for="meter in meters" :key="meter.label" :xs="24" :sm="12" :lg="6">
            <div class="dasslm-meter">
              <div class="flex-cb">
                <div>
                  <div class="text-sm text-g-600">{{ meter.label }}</div>
                  <div class="mt-1 truncate text-sm font-medium text-g-900">{{ meter.primary }}</div>
                </div>
                <div class="text-2xl font-semibold" :style="{ color: tone(meter.percent).color }">{{ meter.percent }}%</div>
              </div>
              <ElProgress :percentage="meter.percent" :show-text="false" :stroke-width="8" :color="tone(meter.percent).color" class="my-3" />
              <div class="flex-cb text-xs">
                <span class="text-g-500">{{ meter.secondary }}</span>
                <span :style="{ color: tone(meter.percent).color }">{{ tone(meter.percent).text }}</span>
              </div>
            </div>
          </ElCol>
        </ElRow>
      </div>
    </div>

    <div class="art-card p-5">
      <h4 class="text-base font-medium text-g-900">项目地址</h4>
      <p class="mb-4 text-xs text-g-500">开源仓库、问题反馈与版本发布</p>
      <ElRow :gutter="16">
        <ElCol v-for="link in projectLinks" :key="link.title" :xs="24" :md="8">
          <a class="dasslm-link" :href="link.url" target="_blank" rel="noopener noreferrer">
            <span class="size-10 flex-cc rounded-lg bg-g-200 text-lg text-g-700">
              <ArtSvgIcon :icon="link.icon" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="block font-medium text-g-900">{{ link.title }}</span>
              <span class="block text-xs text-g-500">{{ link.detail }}</span>
              <span class="block truncate text-xs text-g-500">{{ link.url }}</span>
            </span>
            <ArtSvgIcon icon="ri:external-link-line" class="text-g-500" />
          </a>
        </ElCol>
      </ElRow>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { api, type Row } from '@/api/dasslm'
  import { formatDateTime } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmDashboard' })

  const projectLinks = [
    { title: '开源地址', detail: 'GitHub Repository', url: 'https://github.com/Qialas/DnsaSSL_Mgr', icon: 'ri:github-line' },
    { title: 'Issues 提交', detail: 'Bug / Feature Request', url: 'https://github.com/Qialas/DnsaSSL_Mgr/issues', icon: 'ri:bug-line' },
    { title: 'Release 地址', detail: '版本发布与下载', url: 'https://github.com/Qialas/DnsaSSL_Mgr/releases', icon: 'ri:price-tag-3-line' }
  ]

  const data = ref<Row | null>(null)
  const loading = ref(false)
  const error = ref('')
  const updatedAt = computed(() => data.value?.serverUpdatedAt)

  const assets = computed(() => [
    { title: '域名数量', value: Number(data.value?.domains || 0), detail: '已接入管理的域名', icon: 'ri:global-line' },
    { title: '证书数量', value: Number(data.value?.certificates || 0), detail: '证书资产总量', icon: 'ri:shield-keyhole-line' },
    { title: '自动任务数量', value: Number(data.value?.tasks || 0), detail: '自动化执行配置', icon: 'ri:time-line' },
    { title: '7天内到期证书', value: Number(data.value?.expiringCerts7d || 0), detail: '建议优先检查续签', icon: 'ri:alarm-warning-line' }
  ])

  const clamp = (value: unknown) => Math.round(Math.min(Math.max(Number(value || 0), 0), 100))

  const meters = computed(() => {
    const server = data.value?.server || {}
    const cpu = server.cpu || {}
    const memory = server.memory || {}
    const disk = server.disk || {}
    const load = Number(server.loadAverage || 0)
    const loadPercent = load ? Math.min((load / Math.max(cpu.total || 1, 1)) * 100, 100) : 0
    return [
      { label: '负载', percent: clamp(loadPercent), primary: '运行平稳', secondary: `1分钟 ${load.toFixed(2)}` },
      { label: 'CPU', percent: clamp(cpu.percent), primary: `${cpu.total || 0} 核心`, secondary: '实时采样' },
      { label: '内存', percent: clamp(memory.percent), primary: memory.detailText || '暂无数据', secondary: '系统内存' },
      { label: '储存', percent: clamp(disk.percent), primary: disk.detailText || '暂无数据', secondary: '根分区' }
    ]
  })

  function tone(percent: number) {
    if (percent >= 85) return { color: 'var(--el-color-danger)', text: '压力较高' }
    if (percent >= 65) return { color: 'var(--el-color-warning)', text: '需要关注' }
    return { color: 'var(--el-color-success)', text: '运行平稳' }
  }

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const res = await api<Row>('/dashboard/overview', { silentError: true })
      data.value = res.data
    } catch (cause: any) {
      error.value = cause?.message || '仪表盘数据加载失败'
    } finally {
      loading.value = false
    }
  }

  onMounted(load)
</script>

<style scoped lang="scss">
  .dasslm-meter {
    padding: 14px 16px;
    margin-bottom: 12px;
    background: var(--art-gray-100);
    border: 1px solid var(--default-border);
    border-radius: calc(var(--custom-radius) / 2 + 2px);
  }

  .dasslm-link {
    display: flex;
    gap: 12px;
    align-items: center;
    padding: 14px;
    margin-bottom: 12px;
    border: 1px solid var(--default-border);
    border-radius: calc(var(--custom-radius) / 2 + 2px);
    transition:
      border-color 0.15s,
      background-color 0.15s;

    &:hover,
    &:focus-visible {
      background: var(--art-hover-color);
      border-color: var(--el-color-primary-light-5);
    }
  }
</style>
