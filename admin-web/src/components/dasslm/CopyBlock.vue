<!-- 可复制的长文本块：日志请求/响应、PEM 证书、WHOIS 原文等 -->
<template>
  <div class="copy-block" :class="{ 'is-empty': !text }">
    <div v-if="title || text" class="copy-block__head">
      <span class="copy-block__title">{{ title }}</span>
      <ElButton v-if="text" size="small" text @click="copyText(text, title || '内容')">
        <ArtSvgIcon icon="ri:file-copy-line" class="mr-1" />复制
      </ElButton>
    </div>
    <pre class="copy-block__text" :style="{ maxHeight }">{{ text || '-' }}</pre>
  </div>
</template>

<script setup lang="ts">
  import { copyText, formatLogText } from './meta'

  defineOptions({ name: 'CopyBlock' })

  const props = withDefaults(
    defineProps<{
      value?: unknown
      title?: string
      /** 是否尝试把 JSON 格式化输出 */
      pretty?: boolean
      maxHeight?: string
    }>(),
    { pretty: true, maxHeight: '320px' }
  )

  const text = computed(() => {
    if (props.value === undefined || props.value === null) return ''
    return props.pretty ? formatLogText(props.value) : String(props.value)
  })
</script>

<style scoped lang="scss">
  .copy-block {
    width: 100%;
    min-width: 0;
  }

  .copy-block__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 24px;
    margin-bottom: 4px;
  }

  .copy-block__title {
    font-size: 13px;
    font-weight: 600;
    color: var(--art-gray-800);
  }

  .copy-block__text {
    max-width: 100%;
    padding: 10px 12px;
    margin: 0;
    overflow: auto;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    line-height: 1.6;
    color: var(--art-gray-800);
    word-break: break-all;
    white-space: pre-wrap;
    background: var(--art-gray-100);
    border: 1px solid var(--default-border);
    border-radius: 6px;
  }

  .copy-block.is-empty .copy-block__text {
    padding: 0;
    background: transparent;
    border: 0;
  }
</style>
