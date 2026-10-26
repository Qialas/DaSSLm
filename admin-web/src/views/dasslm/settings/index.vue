<!-- 基础设置 -->
<template>
  <div class="art-card settings-page">
    <ElTabs v-model="activeTab" :tab-position="isMobile ? 'top' : 'left'" class="settings-page__tabs">
      <ElTabPane v-for="tab in tabs" :key="tab.key" :name="tab.key" lazy>
        <template #label>
          <span class="inline-flex items-center gap-1.5">
            <ArtSvgIcon :icon="tab.icon" />{{ tab.label }}
          </span>
        </template>
        <NoticeRules v-if="tab.key === 'notice'" />
        <ProxyPool v-else-if="tab.key === 'proxy'" />
        <TaskSettings v-else-if="tab.key === 'task'" />
        <ElEmpty v-else description="暂无站点设置项" :image-size="96" />
      </ElTabPane>
    </ElTabs>
  </div>
</template>

<script setup lang="ts">
  import { useWindowSize } from '@vueuse/core'
  import NoticeRules from './modules/notice-rules.vue'
  import ProxyPool from './modules/proxy-pool.vue'
  import TaskSettings from './modules/task-settings.vue'

  defineOptions({ name: 'DaSSLmSettings' })

  const tabs = [
    { key: 'site', label: '站点设置', icon: 'ri:global-line' },
    { key: 'notice', label: '通知设置', icon: 'ri:notification-3-line' },
    { key: 'proxy', label: '代理设置', icon: 'ri:links-line' },
    { key: 'task', label: '计划任务', icon: 'ri:time-line' }
  ]

  const activeTab = ref('site')
  const { width } = useWindowSize()
  const isMobile = computed(() => width.value < 768)
</script>

<style scoped lang="scss">
  .settings-page {
    min-height: var(--art-full-height);
    padding: 16px;
  }

  .settings-page__tabs {
    :deep(.el-tabs__content) {
      padding-left: 20px;
    }

    :deep(.el-tabs__item) {
      justify-content: flex-start;
      min-width: 120px;
    }
  }

  @media (max-width: 767px) {
    .settings-page__tabs :deep(.el-tabs__content) {
      padding-left: 0;
    }
  }
</style>
