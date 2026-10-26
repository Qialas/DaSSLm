<!-- 后台用户 -->
<template>
  <div class="art-full-height">
    <ElCard class="art-table-card" shadow="never">
      <ArtTableHeader v-model:columns="columnChecks" :loading="loading" @refresh="refreshData">
        <template #left>
          <ElButton type="primary" v-ripple @click="openForm()">
            <ArtSvgIcon icon="ri:add-line" class="mr-1" />新建
          </ElButton>
        </template>
      </ArtTableHeader>

      <ArtTable
        :loading="loading"
        :data="data"
        :columns="columns"
        :pagination="pagination"
        @pagination:size-change="handleSizeChange"
        @pagination:current-change="handleCurrentChange"
      >
        <template #role="{ row }">
          <ElTag :type="row.role === 'admin' ? 'primary' : 'info'" disable-transitions>{{ roleNames[row.role] || row.role || '-' }}</ElTag>
        </template>
        <template #status="{ row }">
          <ElTag :type="statusTag(row.status).type" disable-transitions>{{ statusTag(row.status).text }}</ElTag>
        </template>
        <template #operation="{ row }">
          <TableAction icon="ri:pencil-line" @click="openForm(row)">编辑</TableAction>
          <ElPopconfirm title="确认删除？" @confirm="remove(row)">
            <template #reference>
              <TableAction icon="ri:delete-bin-5-line" type="danger">删除</TableAction>
            </template>
          </ElPopconfirm>
        </template>
      </ArtTable>
    </ElCard>

    <ElDialog v-model="formOpen" :title="`${current ? '编辑' : '新建'}后台用户`" width="520px" align-center destroy-on-close>
      <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
        <ElFormItem label="用户名" prop="username">
          <ElInput v-model="form.username" autocomplete="off" />
        </ElFormItem>
        <ElFormItem label="密码" prop="password">
          <ElInput
            v-model="form.password"
            type="password"
            show-password
            autocomplete="new-password"
            :placeholder="current ? '留空则不修改密码' : '请输入登录密码'"
          />
        </ElFormItem>
        <ElFormItem label="昵称" prop="nickname">
          <ElInput v-model="form.nickname" />
        </ElFormItem>
        <ElFormItem label="邮箱" prop="email">
          <ElInput v-model="form.email" />
        </ElFormItem>
        <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
          <ElFormItem label="角色" prop="role">
            <ElSelect v-model="form.role">
              <ElOption v-for="(label, value) in roleNames" :key="value" :label="label" :value="value" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="状态" prop="status">
            <ElSelect v-model="form.status">
              <ElOption v-for="item in enabledStatusOptions" :key="item.value" :label="item.label" :value="item.value" />
            </ElSelect>
          </ElFormItem>
        </div>
      </ElForm>
      <template #footer>
        <ElButton @click="formOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
      </template>
    </ElDialog>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { createResource, deleteResource, updateResource, type Row } from '@/api/dasslm'
  import TableAction from '@/components/dasslm/TableAction.vue'
  import { useResourceTable } from '@/components/dasslm/useResourceTable'
  import { enabledStatusOptions, statusTag } from '@/components/dasslm/meta'

  defineOptions({ name: 'DaSSLmUsers' })

  const RESOURCE = 'users'
  const roleNames: Record<string, string> = { admin: '管理员', operator: '操作员' }

  const {
    columns,
    columnChecks,
    data,
    loading,
    pagination,
    handleSizeChange,
    handleCurrentChange,
    refreshData,
    refreshCreate,
    refreshUpdate,
    refreshRemove
  } = useResourceTable(RESOURCE, () => [
    { prop: 'username', label: '用户名', minWidth: 140 },
    { prop: 'nickname', label: '昵称', minWidth: 140 },
    { prop: 'email', label: '邮箱', minWidth: 180 },
    { prop: 'role', label: '角色', width: 110, useSlot: true },
    { prop: 'status', label: '状态', width: 90, useSlot: true },
    { prop: 'operation', label: '操作', width: 160, fixed: 'right', useSlot: true }
  ])

  async function remove(row: Row) {
    await deleteResource(RESOURCE, row.id)
    ElMessage.success('已删除')
    refreshRemove()
  }

  const formOpen = ref(false)
  const saving = ref(false)
  const current = ref<Row | null>(null)
  const formRef = ref<FormInstance>()
  const form = ref<Row>({})
  const rules = computed<FormRules>(() => ({
    username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
    password: current.value ? [] : [{ required: true, message: '请输入密码', trigger: 'blur' }]
  }))

  function openForm(row?: Row) {
    current.value = row || null
    form.value = row ? { ...row, password: '' } : { status: 'enabled', role: 'operator', password: '' }
    formOpen.value = true
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      if (current.value?.id) {
        await updateResource(RESOURCE, current.value.id, { ...current.value, ...form.value })
        refreshUpdate()
      } else {
        await createResource(RESOURCE, form.value)
        refreshCreate()
      }
      ElMessage.success('保存成功')
      formOpen.value = false
    } finally {
      saving.value = false
    }
  }
</script>
