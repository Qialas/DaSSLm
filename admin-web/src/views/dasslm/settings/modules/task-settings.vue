<!-- 计划任务：系统级后台任务的执行频率 -->
<template>
  <section>
    <div class="mb-4 flex-cb gap-3 max-sm:flex-col max-sm:items-start">
      <div>
        <h3 class="text-base font-medium text-g-900">计划任务</h3>
        <p class="text-sm text-g-500">维护系统级后台任务的执行频率，保存后将在下一轮调度时生效</p>
      </div>
      <ElButton :loading="loading" @click="load">刷新</ElButton>
    </div>

    <ElTable v-loading="loading" :data="tasks" row-key="key" border>
      <ElTableColumn label="计划任务" min-width="200">
        <template #default="{ row }">
          <div class="leading-5">
            <div class="font-medium text-g-900">{{ row.name }}</div>
            <div class="text-xs text-g-500">{{ row.description }}</div>
          </div>
        </template>
      </ElTableColumn>
      <ElTableColumn label="模块" width="110">
        <template #default="{ row }">
          <ElTag type="primary" disable-transitions>{{ row.module || '-' }}</ElTag>
        </template>
      </ElTableColumn>
      <ElTableColumn label="调度方式" min-width="330">
        <template #default="{ row }">
          <div v-if="drafts[row.key]" class="flex flex-wrap items-center gap-2">
            <ElSelect v-model="drafts[row.key].scheduleType" class="!w-28">
              <ElOption label="按间隔" value="interval" />
              <ElOption label="每天定时" value="daily" />
            </ElSelect>
            <template v-if="drafts[row.key].scheduleType === 'daily'">
              <ElInputNumber v-model="drafts[row.key].dailyHour" :min="0" :max="23" :precision="0" controls-position="right" class="!w-24" />
              <span class="text-g-600">点</span>
              <ElInputNumber v-model="drafts[row.key].dailyMinute" :min="0" :max="59" :precision="0" controls-position="right" class="!w-24" />
              <span class="text-g-600">分</span>
            </template>
            <template v-else>
              <ElInputNumber v-model="drafts[row.key].intervalValue" :min="1" :precision="0" controls-position="right" class="!w-24" />
              <ElSelect v-model="drafts[row.key].intervalUnit" class="!w-24">
                <ElOption v-for="unit in intervalUnits" :key="unit.value" :label="unit.label" :value="unit.value" />
              </ElSelect>
            </template>
          </div>
        </template>
      </ElTableColumn>
      <ElTableColumn label="当前规则" width="150">
        <template #default="{ row }">{{ row.scheduleText || `${row.intervalSeconds} 秒` }}</template>
      </ElTableColumn>
      <ElTableColumn label="任务参数" width="190">
        <template #default="{ row }">
          <div v-if="(row.defaultRetentionDays || row.retentionDays) && drafts[row.key]" class="flex items-center gap-1.5">
            <span class="text-g-600">清理</span>
            <ElInputNumber
              v-model="drafts[row.key].retentionDays"
              :min="row.minRetentionDays || 1"
              :max="row.maxRetentionDays || 3650"
              :precision="0"
              controls-position="right"
              class="!w-24"
            />
            <span class="text-g-600">天前</span>
          </div>
          <span v-else>-</span>
        </template>
      </ElTableColumn>
      <ElTableColumn label="状态" width="90">
        <template #default="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
      </ElTableColumn>
      <ElTableColumn label="操作" width="100" fixed="right">
        <template #default="{ row }">
          <TableAction icon="ri:check-line" :loading="!!savingKeys[row.key]" @click="saveTask(row)">保存</TableAction>
        </template>
      </ElTableColumn>
    </ElTable>
  </section>
</template>

<script setup lang="ts">
  import { ElMessage } from 'element-plus'
  import { getSystemTasks, updateSystemTask, type Row } from '@/api/dasslm'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { statusTag } from '@/components/dasslm/meta'

  defineOptions({ name: 'SettingsTaskSettings' })

  const intervalUnits = [
    { label: '秒', value: 'second' },
    { label: '分', value: 'minute' },
    { label: '小时', value: 'hour' },
    { label: '天', value: 'day' }
  ]

  const tasks = ref<Row[]>([])
  const drafts = reactive<Record<string, Row>>({})
  const loading = ref(false)
  const savingKeys = reactive<Record<string, boolean>>({})

  function toDraft(item: Row = {}): Row {
    return {
      scheduleType: item.scheduleType || 'interval',
      intervalValue: Number(item.intervalValue || item.intervalSeconds || item.defaultIntervalSeconds || 30),
      intervalUnit: item.intervalUnit || 'second',
      dailyHour: Number(item.dailyHour || 0),
      dailyMinute: Number(item.dailyMinute || 0),
      retentionDays: Number(item.retentionDays || item.defaultRetentionDays || 30)
    }
  }

  async function load() {
    loading.value = true
    try {
      const res = await getSystemTasks()
      tasks.value = res.data || []
      Object.keys(drafts).forEach((key) => delete drafts[key])
      tasks.value.forEach((item) => {
        drafts[item.key] = toDraft(item)
      })
    } finally {
      loading.value = false
    }
  }

  onMounted(load)

  async function saveTask(row: Row) {
    savingKeys[row.key] = true
    try {
      const draft = drafts[row.key] || toDraft(row)
      const res = await updateSystemTask(row.key, {
        ...draft,
        intervalValue: Number(draft.intervalValue || 1),
        dailyHour: Number(draft.dailyHour || 0),
        dailyMinute: Number(draft.dailyMinute || 0),
        retentionDays: Number(draft.retentionDays || row.defaultRetentionDays || 30)
      })
      tasks.value = tasks.value.map((item) => (item.key === row.key ? res.data : item))
      drafts[row.key] = toDraft(res.data)
      ElMessage.success('保存成功')
    } finally {
      savingKeys[row.key] = false
    }
  }
</script>
