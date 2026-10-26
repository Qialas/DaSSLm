<!-- 账号表单中的代理配置：直连 / 指定代理节点 / 代理池自动轮询 -->
<template>
  <ElFormItem label="使用代理" prop="useProxy">
    <ElSwitch v-model="model.useProxy" />
  </ElFormItem>
  <template v-if="model.useProxy">
    <ElFormItem label="代理方式" prop="proxyMode" :rules="[{ required: true, message: '请选择代理方式', trigger: 'change' }]">
      <ElRadioGroup v-model="model.proxyMode">
        <ElRadioButton value="manual">指定代理</ElRadioButton>
        <ElRadioButton value="auto">自动轮询</ElRadioButton>
      </ElRadioGroup>
    </ElFormItem>
    <ElFormItem
      v-if="model.proxyMode !== 'auto'"
      label="代理节点"
      prop="proxyId"
      :rules="[{ required: true, message: '请选择代理节点', trigger: 'change' }]"
    >
      <ElSelect v-model="model.proxyId" placeholder="从代理池选择" :loading="loading" filterable class="w-full">
        <ElOption v-for="item in proxies" :key="item.id" :label="proxyLabel(item)" :value="item.id" />
      </ElSelect>
    </ElFormItem>
  </template>
</template>

<script setup lang="ts">
  import { listAll, type Row } from '@/api/dasslm'

  defineOptions({ name: 'ProxyFields' })

  /** 表单数据对象，直接读写其中的 useProxy / proxyMode / proxyId */
  const model = defineModel<Row>({ required: true })

  const proxies = ref<Row[]>([])
  const loading = ref(false)
  let loaded = false

  function proxyLabel(row: Row) {
    return `${row.name} · ${String(row.protocol || '').toUpperCase()} · ${row.host}:${row.port}`
  }

  async function loadProxies() {
    if (loaded || loading.value) return
    loading.value = true
    try {
      proxies.value = (await listAll('proxy-settings')).filter((item) => item.status === 'enabled')
      loaded = true
    } finally {
      loading.value = false
    }
  }

  watch(
    () => model.value.useProxy && model.value.proxyMode !== 'auto',
    (needList) => {
      if (needList) loadProxies().catch(() => undefined)
    },
    { immediate: true }
  )

  watch(
    () => model.value.proxyId,
    (value) => {
      // 后端用 0 表示未选择
      if (value === 0) model.value.proxyId = undefined
    },
    { immediate: true }
  )
</script>
