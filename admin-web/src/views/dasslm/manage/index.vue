<!-- 管理入口（移动端快捷导航） -->
<template>
  <ElRow :gutter="16">
    <ElCol v-for="item in entries" :key="item.path" :xs="12" :sm="8" :lg="6">
      <RouterLink :to="item.path" class="art-card mb-4 flex-c gap-3 p-4 transition-colors hover:bg-hover-color">
        <span class="size-10 flex-cc rounded-lg bg-theme/10 text-lg text-theme">
          <ArtSvgIcon :icon="item.icon" />
        </span>
        <span class="text-sm font-medium text-g-900">{{ item.title }}</span>
      </RouterLink>
    </ElCol>
  </ElRow>
</template>

<script setup lang="ts">
  defineOptions({ name: 'DaSSLmManage' })

  const entries = [
    { path: '/accounts/domain', title: '域名账号', icon: 'ri:cloud-line' },
    { path: '/accounts/ssl', title: 'SSL 账号', icon: 'ri:file-shield-line' },
    { path: '/accounts/deploy', title: '部署账号', icon: 'ri:server-line' },
    { path: '/logs', title: '日志管理', icon: 'ri:file-list-3-line' },
    { path: '/settings/base', title: '基础设置', icon: 'ri:settings-line' },
    { path: '/settings/profile', title: '后台用户', icon: 'ri:user-settings-line' },
    { path: '/settings/notice', title: '通知渠道', icon: 'ri:notification-3-line' }
  ]
</script>
