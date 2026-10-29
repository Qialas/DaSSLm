<!-- SSL 证书卡片选择器（自动部署任务中选择证书） -->
<template>
  <div class="cert-picker">
    <div class="cert-picker__head">
      <ElInput v-model="keyword" clearable :validate-event="false" placeholder="搜索主域名、其他域名或证书品牌" @input="page = 1">
        <template #prefix><ArtSvgIcon icon="ri:search-line" /></template>
      </ElInput>
      <div class="cert-picker__pager">
        <ElButton :disabled="currentPage <= 1" @click="page = currentPage - 1" aria-label="上一页">
          <ArtSvgIcon icon="ri:arrow-left-s-line" />
        </ElButton>
        <span>{{ currentPage }}/{{ totalPages }}</span>
        <ElButton :disabled="currentPage >= totalPages" @click="page = currentPage + 1" aria-label="下一页">
          <ArtSvgIcon icon="ri:arrow-right-s-line" />
        </ElButton>
      </div>
    </div>
    <div v-if="pageItems.length" class="cert-picker__grid">
      <button
        v-for="cert in pageItems"
        :key="cert.id"
        type="button"
        class="cert-picker__card"
        :class="{ 'is-active': Number(modelValue) === Number(cert.id) }"
        :aria-pressed="Number(modelValue) === Number(cert.id)"
        @click="select(cert)"
      >
        <span class="cert-picker__top">
          <span class="cert-picker__brand">
            <ProviderIcon v-bind="brandIcon(cert)" />
            {{ brandName(cert) }}
          </span>
          <ElTag size="small" :type="cert.status === 'issued' ? 'success' : 'warning'" disable-transitions>
            {{ certificateStatusTag(cert.status).text }}
          </ElTag>
        </span>
        <span class="cert-picker__domain">{{ cert.commonName || '-' }}</span>
        <span v-if="otherNames(cert).length" class="cert-picker__sans">{{ otherNames(cert).slice(0, 3).join(' / ') }}</span>
        <span class="cert-picker__foot">
          <span>{{ sslAccountMap[cert.sslAccountId]?.name || '未关联证书账号' }}</span>
          <ElTag size="small" :type="expiryTag(cert).type" disable-transitions>{{ expiryTag(cert).text }}</ElTag>
        </span>
      </button>
    </div>
    <ElEmpty v-else description="没有匹配的SSL证书" :image-size="64" />
  </div>
</template>

<script setup lang="ts">
  import type { Row } from '@/api/dasslm'
  import ProviderIcon from './ProviderIcon.vue'
  import { certificateStatusTag, daysUntil, parseSANs, sslProviderNames, sslProviders, type TagMeta } from './meta'

  defineOptions({ name: 'CertificatePicker' })

  const props = defineProps<{ certificates: Row[]; sslAccountMap: Record<string, Row> }>()
  const modelValue = defineModel<number>()
  const emit = defineEmits<{ (e: 'change', id: number, cert: Row): void }>()

  const PAGE_SIZE = 9
  const keyword = ref('')
  const page = ref(1)

  const brandName = (cert: Row) => {
    const provider = props.sslAccountMap[cert.sslAccountId]?.provider
    return sslProviderNames[provider] || provider || 'SSL'
  }

  const brandIcon = (cert: Row) => {
    const provider = props.sslAccountMap[cert.sslAccountId]?.provider
    const meta = sslProviders.find((item) => item.value === provider)
    return { image: meta?.image, icon: meta?.icon || (meta?.image ? undefined : 'ri:shield-check-line') }
  }

  const otherNames = (cert: Row) => parseSANs(cert.sans).filter((item) => item !== cert.commonName)

  const filtered = computed(() => {
    const query = keyword.value.trim().toLowerCase()
    if (!query) return props.certificates
    return props.certificates.filter((cert) =>
      [cert.commonName, parseSANs(cert.sans).join(' '), brandName(cert), cert.issuer]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
        .includes(query)
    )
  })

  const totalPages = computed(() => Math.max(1, Math.ceil(filtered.value.length / PAGE_SIZE)))
  const currentPage = computed(() => Math.min(page.value, totalPages.value))
  const pageItems = computed(() => filtered.value.slice((currentPage.value - 1) * PAGE_SIZE, currentPage.value * PAGE_SIZE))

  function expiryTag(cert: Row): TagMeta {
    const days = daysUntil(cert.expiresAt)
    if (days === null) return { type: 'info', text: '未知到期' }
    if (days < 0) return { type: 'danger', text: `已过期 ${Math.abs(days)} 天` }
    return { type: days <= 15 ? 'danger' : days <= 30 ? 'warning' : 'success', text: `剩余 ${days} 天` }
  }

  function select(cert: Row) {
    modelValue.value = Number(cert.id)
    emit('change', Number(cert.id), cert)
  }
</script>

<style scoped lang="scss">
  .cert-picker {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: 100%;
  }

  .cert-picker__head {
    display: flex;
    gap: 12px;
    align-items: center;
  }

  .cert-picker__pager {
    display: flex;
    flex: none;
    gap: 6px;
    align-items: center;
    font-size: 13px;
    color: var(--art-gray-600);

    .el-button {
      width: 32px;
      padding: 0;
      margin: 0;
    }
  }

  .cert-picker__grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;

    @media (max-width: 720px) {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    @media (max-width: 480px) {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  .cert-picker__card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    padding: 12px;
    text-align: left;
    cursor: pointer;
    background: var(--default-box-color);
    border: 1px solid var(--default-border);
    border-radius: calc(var(--custom-radius) / 2 + 2px);
    transition:
      border-color 0.15s,
      background-color 0.15s;

    &:hover {
      border-color: var(--el-color-primary-light-5);
    }

    &.is-active {
      background: var(--el-color-primary-light-9);
      border-color: var(--el-color-primary);
      box-shadow: 0 0 0 1px var(--el-color-primary) inset;
    }
  }

  .cert-picker__foot {
    margin-top: auto;
  }

  .cert-picker__top,
  .cert-picker__foot {
    display: flex;
    gap: 6px;
    align-items: center;
    justify-content: space-between;
    min-width: 0;
  }

  .cert-picker__brand {
    display: inline-flex;
    gap: 6px;
    align-items: center;
    min-width: 0;
    overflow: hidden;
    font-size: 12px;
    color: var(--art-gray-600);
    white-space: nowrap;
  }

  .cert-picker__domain {
    overflow: hidden;
    font-size: 14px;
    font-weight: 600;
    color: var(--art-gray-900);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cert-picker__sans {
    overflow: hidden;
    font-size: 12px;
    color: var(--art-gray-600);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cert-picker__foot > span:first-child {
    overflow: hidden;
    font-size: 12px;
    color: var(--art-gray-600);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
