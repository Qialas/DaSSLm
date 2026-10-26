<!-- 云服务商 / 证书服务 / 通知渠道图标 -->
<template>
  <span class="provider-icon" :class="[`provider-icon--${size}`, { 'is-boxed': boxed }]">
    <img v-if="src" :src="src" :alt="alt" />
    <ArtSvgIcon v-else-if="icon" :icon="icon" />
    <span v-else class="provider-icon__letter">{{ letter }}</span>
  </span>
</template>

<script setup lang="ts">
  import { providerImages, providerNames } from './meta'

  defineOptions({ name: 'ProviderIcon' })

  interface Props {
    /** 服务商标识，用于查找内置图标 */
    provider?: string
    /** 直接指定图片 */
    image?: string
    /** 直接指定 Remix 图标 */
    icon?: string
    /** 无图标时显示的缩写 */
    fallback?: string
    size?: 'sm' | 'md' | 'lg'
    /** 是否显示带底色的方块 */
    boxed?: boolean
  }

  const props = withDefaults(defineProps<Props>(), { size: 'sm', boxed: false })

  const src = computed(() => props.image || (props.provider ? providerImages[props.provider] : ''))
  const alt = computed(() => (props.provider && providerNames[props.provider]) || props.provider || '')
  const letter = computed(() => props.fallback || props.provider?.slice(0, 2).toUpperCase() || '')
</script>

<style scoped lang="scss">
  .provider-icon {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    color: var(--theme-color);
    vertical-align: middle;

    img {
      width: 100%;
      height: 100%;
      object-fit: contain;
    }

    &--md {
      width: 28px;
      height: 28px;
      font-size: 18px;
    }

    &--lg {
      width: 36px;
      height: 36px;
      font-size: 22px;
    }

    &.is-boxed {
      padding: 6px;
      background: var(--art-gray-200);
      border-radius: 8px;
    }
  }

  .provider-icon--md.is-boxed {
    width: 38px;
    height: 38px;
  }

  .provider-icon__letter {
    font-size: 11px;
    font-weight: 600;
    color: var(--art-gray-700);
  }
</style>
