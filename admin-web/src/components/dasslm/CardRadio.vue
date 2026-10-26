<!-- 卡片式单选：用于选择 DNS 服务商、证书服务、部署服务、通知渠道 -->
<template>
  <div class="card-radio" role="radiogroup" :aria-label="ariaLabel">
    <button
      v-for="option in options"
      :key="option.value"
      type="button"
      role="radio"
      class="card-radio__item"
      :class="{ 'is-active': modelValue === option.value }"
      :aria-checked="modelValue === option.value"
      :disabled="disabled"
      @click="select(option.value)"
    >
      <ProviderIcon :image="option.image" :icon="option.icon" :fallback="option.letter" size="md" boxed />
      <span class="card-radio__text">
        <span class="card-radio__label">{{ option.label }}</span>
        <span v-if="option.desc" class="card-radio__desc">{{ option.desc }}</span>
      </span>
      <ArtSvgIcon v-if="modelValue === option.value" icon="ri:checkbox-circle-fill" class="card-radio__check" />
    </button>
  </div>
</template>

<script setup lang="ts">
  import ProviderIcon from './ProviderIcon.vue'
  import type { CardOption } from './meta'

  defineOptions({ name: 'CardRadio' })

  defineProps<{
    options: CardOption[]
    disabled?: boolean
    ariaLabel?: string
  }>()

  const modelValue = defineModel<string>()
  const emit = defineEmits<{ (e: 'change', value: string): void }>()

  function select(value: string) {
    modelValue.value = value
    emit('change', value)
  }
</script>

<style scoped lang="scss">
  .card-radio {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
    width: 100%;

    @media (max-width: 520px) {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  .card-radio__item {
    position: relative;
    display: flex;
    gap: 12px;
    align-items: center;
    min-width: 0;
    padding: 12px 14px;
    text-align: left;
    cursor: pointer;
    background: var(--default-box-color);
    border: 1px solid var(--default-border);
    border-radius: calc(var(--custom-radius) / 2 + 2px);
    transition:
      border-color 0.15s,
      background-color 0.15s,
      box-shadow 0.15s;

    &:hover {
      border-color: var(--el-color-primary-light-5);
    }

    &.is-active {
      background: var(--el-color-primary-light-9);
      border-color: var(--el-color-primary);
      box-shadow: 0 0 0 1px var(--el-color-primary) inset;
    }

    &:focus-visible {
      outline: 2px solid var(--el-color-primary);
      outline-offset: 2px;
    }

    &:disabled {
      cursor: not-allowed;
      opacity: 0.6;
    }
  }

  .card-radio__text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    line-height: 1.35;
  }

  .card-radio__label {
    overflow: hidden;
    font-size: 14px;
    font-weight: 600;
    color: var(--art-gray-900);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-radio__desc {
    overflow: hidden;
    font-size: 12px;
    color: var(--art-gray-600);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-radio__check {
    position: absolute;
    top: 8px;
    right: 8px;
    font-size: 16px;
    color: var(--el-color-primary);
  }
</style>
