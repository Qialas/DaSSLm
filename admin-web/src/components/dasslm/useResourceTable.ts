import { useTable } from '@/hooks/core/useTable'
import type { ColumnOption } from '@/types/component'
import { PAGE_KEYS, pagedFetcher, type Row } from '@/api/dasslm'

interface ResourceTableOptions {
  /** 额外的查询参数（每次请求时读取） */
  params?: () => Record<string, unknown>
  /** 数据加载成功回调 */
  onLoaded?: (rows: Row[]) => void
  pageSize?: number
}

/**
 * 基于 Art Design Pro useTable 的资源列表：对接 Go 服务端的 page / pageSize 分页接口
 */
export function useResourceTable(
  resource: string,
  columnsFactory: () => ColumnOption<Row>[],
  options: ResourceTableOptions = {}
) {
  return useTable({
    core: {
      apiFn: pagedFetcher<Row>(resource, options.params),
      apiParams: { page: 1, pageSize: options.pageSize || 10 },
      paginationKey: PAGE_KEYS,
      columnsFactory
    },
    hooks: {
      onSuccess: (rows) => options.onLoaded?.(rows as Row[])
    }
  })
}
