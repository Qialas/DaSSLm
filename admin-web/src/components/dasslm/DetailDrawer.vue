<!-- 通用详情抽屉：按字段列表渲染描述表，长文本使用可复制代码块 -->
<template>
  <ElDrawer v-model="visible" :title="title" :size="drawerSize" destroy-on-close>
    <ElDescriptions :column="1" border class="detail-drawer__desc">
      <ElDescriptionsItem v-for="item in visibleItems" :key="item.label" :label="item.label" label-class-name="detail-drawer__label">
        <ElTag v-if="item.tag" :type="item.tag.type" disable-transitions>{{ item.tag.text }}</ElTag>
        <CopyBlock v-else-if="item.block" :value="item.value" />
        <span v-else class="detail-drawer__text">{{ display(item.value) }}</span>
      </ElDescriptionsItem>
    </ElDescriptions>
    <slot />
  </ElDrawer>
</template>

<script setup lang="ts">
  import CopyBlock from './CopyBlock.vue'
  import { useResponsiveSize, type DetailItem } from './meta'

  defineOptions({ name: 'DetailDrawer' })

  const props = withDefaults(defineProps<{ title: string; items: DetailItem[]; width?: number }>(), { width: 760 })
  const visible = defineModel<boolean>({ default: false })
  const drawerSize = useResponsiveSize(props.width)
  const visibleItems = computed(() => props.items.filter((item) => !item.hidden))

  function display(value: unknown) {
    if (value === undefined || value === null || value === '') return '-'
    return String(value)
  }
</script>

<style scoped lang="scss">
  .detail-drawer__text {
    word-break: break-all;
    white-space: pre-wrap;
  }

  :deep(.detail-drawer__label) {
    width: 120px;
    white-space: nowrap;
  }
</style>
