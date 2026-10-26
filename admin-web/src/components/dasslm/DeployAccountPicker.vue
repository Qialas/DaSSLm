<!-- 部署账号卡片选择器 -->
<template>
  <div class="deploy-picker">
    <ElInput v-model="keyword" clearable :validate-event="false" placeholder="搜索部署账号、服务类型或地址">
      <template #prefix><ArtSvgIcon icon="ri:search-line" /></template>
    </ElInput>
    <div v-if="filtered.length" class="deploy-picker__grid">
      <button
        v-for="account in filtered"
        :key="account.id"
        type="button"
        class="deploy-picker__card"
        :class="{ 'is-active': Number(modelValue) === Number(account.id) }"
        :aria-pressed="Number(modelValue) === Number(account.id)"
        @click="select(account)"
      >
        <span class="deploy-picker__top">
          <span class="deploy-picker__brand">
            <ProviderIcon :provider="brandProvider(account.provider)" />
            {{ deployProviderLabel(account.provider) }}
          </span>
          <ElTag size="small" :type="account.status === 'enabled' ? 'success' : 'info'" disable-transitions>
            {{ account.status === 'enabled' ? '启用' : '停用' }}
          </ElTag>
        </span>
        <strong class="deploy-picker__name">{{ account.name }}</strong>
        <em v-if="account.endpoint" class="deploy-picker__endpoint">{{ account.endpoint }}</em>
        <span v-if="account.remark" class="deploy-picker__remark">{{ account.remark }}</span>
      </button>
    </div>
    <ElEmpty v-else description="没有可用部署账号" :image-size="64" />
  </div>
</template>

<script setup lang="ts">
  import type { Row } from '@/api/dasslm'
  import ProviderIcon from './ProviderIcon.vue'
  import { deployProviderLabel } from './meta'

  defineOptions({ name: 'DeployAccountPicker' })

  const props = defineProps<{ accounts: Row[] }>()
  const modelValue = defineModel<number>()
  const emit = defineEmits<{ (e: 'change', id: number, account: Row): void }>()
  const keyword = ref('')

  const filtered = computed(() => {
    const query = keyword.value.trim().toLowerCase()
    if (!query) return props.accounts
    return props.accounts.filter((item) =>
      [item.name, deployProviderLabel(item.provider), item.endpoint, item.remark]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
        .includes(query)
    )
  })

  const brandProvider = (provider?: string) => (provider === 'baota' ? 'btpanel' : provider)

  function select(account: Row) {
    modelValue.value = Number(account.id)
    emit('change', Number(account.id), account)
  }
</script>

<style scoped lang="scss">
  .deploy-picker {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .deploy-picker__grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;

    @media (max-width: 520px) {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  .deploy-picker__card {
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

  .deploy-picker__top {
    display: flex;
    gap: 8px;
    align-items: center;
    justify-content: space-between;
  }

  .deploy-picker__brand {
    display: inline-flex;
    gap: 6px;
    align-items: center;
    font-size: 12px;
    color: var(--art-gray-600);
  }

  .deploy-picker__name {
    overflow: hidden;
    font-size: 14px;
    color: var(--art-gray-900);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .deploy-picker__endpoint,
  .deploy-picker__remark {
    overflow: hidden;
    font-size: 12px;
    font-style: normal;
    color: var(--art-gray-600);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
