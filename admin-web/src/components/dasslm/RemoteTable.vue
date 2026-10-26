<!-- 抽屉/弹窗内使用的分页表格：自行管理分页与加载状态，列通过默认插槽传入 ElTableColumn -->
<template>
  <div class="remote-table">
    <ElTable v-loading="loading" :data="rows" :row-key="rowKey" :max-height="maxHeight" border>
      <slot />
      <template #empty>
        <ElEmpty v-if="!loading" :description="emptyText" :image-size="80" />
      </template>
    </ElTable>
    <div v-if="total > 0" class="remote-table__pagination">
      <ElPagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, prev, pager, next, sizes"
        background
        size="small"
        @current-change="load"
        @size-change="reload"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
  import type { ListResult, Row } from '@/api/dasslm'

  defineOptions({ name: 'RemoteTable' })

  const props = withDefaults(
    defineProps<{
      /** 请求函数，返回 { items, total } */
      fetcher: (page: number, pageSize: number) => Promise<ListResult | undefined>
      rowKey?: string | ((row: Row) => string)
      maxHeight?: string | number
      emptyText?: string
      immediate?: boolean
    }>(),
    { rowKey: 'id', maxHeight: '62vh', emptyText: '暂无数据', immediate: true }
  )

  const rows = ref<Row[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(10)
  const loading = ref(false)
  let seq = 0

  async function load() {
    const current = ++seq
    loading.value = true
    try {
      const result = await props.fetcher(page.value, pageSize.value)
      if (current !== seq) return
      rows.value = result?.items || []
      total.value = result?.total || 0
    } catch {
      if (current === seq) {
        rows.value = []
        total.value = 0
      }
    } finally {
      if (current === seq) loading.value = false
    }
  }

  /** 回到第一页重新加载 */
  function reload() {
    page.value = 1
    return load()
  }

  onMounted(() => {
    if (props.immediate) load()
  })

  defineExpose({ load, reload, rows })
</script>

<style scoped lang="scss">
  .remote-table__pagination {
    display: flex;
    justify-content: flex-end;
    margin-top: 12px;
  }
</style>
