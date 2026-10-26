<!-- 表格行内操作按钮：沿用 ArtButtonTable 的浅色底样式，并带文字说明 -->
<template>
  <button
    type="button"
    class="table-action"
    :class="[`table-action--${type}`, { 'is-disabled': disabled || loading }]"
    :disabled="disabled || loading"
    @click="emit('click', $event)"
  >
    <ArtSvgIcon v-if="loading" icon="ri:loader-4-line" class="animate-spin" />
    <ArtSvgIcon v-else-if="icon" :icon="icon" />
    <span><slot /></span>
  </button>
</template>

<script setup lang="ts">
  defineOptions({ name: 'TableAction' })

  withDefaults(
    defineProps<{
      icon?: string
      type?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
      disabled?: boolean
      loading?: boolean
    }>(),
    { type: 'primary', disabled: false, loading: false }
  )

  const emit = defineEmits<{ (e: 'click', event: MouseEvent): void }>()
</script>

<style scoped lang="scss">
  .table-action {
    display: inline-flex;
    gap: 4px;
    align-items: center;
    height: 26px;
    padding: 0 9px;
    margin: 2px 6px 2px 0;
    font-size: 12px;
    line-height: 1;
    white-space: nowrap;
    vertical-align: middle;
    cursor: pointer;
    border: 0;
    border-radius: 6px;
    transition:
      opacity 0.15s,
      filter 0.15s;

    &:hover:not(.is-disabled) {
      filter: brightness(0.94);
    }

    &:focus-visible {
      outline: 2px solid var(--el-color-primary);
      outline-offset: 1px;
    }

    &.is-disabled {
      cursor: not-allowed;
      opacity: 0.45;
    }
  }

  .table-action--primary {
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }

  .table-action--success {
    color: var(--el-color-success);
    background: var(--el-color-success-light-9);
  }

  .table-action--warning {
    color: var(--el-color-warning);
    background: var(--el-color-warning-light-9);
  }

  .table-action--danger {
    color: var(--el-color-danger);
    background: var(--el-color-danger-light-9);
  }

  .table-action--info {
    color: var(--art-gray-700);
    background: var(--art-gray-200);
  }
</style>
