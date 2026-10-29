<!-- 修改当前登录用户的密码 -->
<template>
  <ElDialog v-model="open" title="修改密码" width="420px" align-center destroy-on-close append-to-body>
    <ElForm ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
      <ElFormItem label="原密码" prop="oldPassword">
        <ElInput v-model="form.oldPassword" type="password" show-password autocomplete="current-password" />
      </ElFormItem>
      <ElFormItem label="新密码" prop="newPassword">
        <ElInput
          v-model="form.newPassword"
          type="password"
          show-password
          autocomplete="new-password"
          placeholder="至少 8 位"
        />
      </ElFormItem>
      <ElFormItem label="确认新密码" prop="confirmPassword">
        <ElInput v-model="form.confirmPassword" type="password" show-password autocomplete="new-password" />
      </ElFormItem>
    </ElForm>
    <template #footer>
      <ElButton @click="open = false">取消</ElButton>
      <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { changePassword } from '@/api/dasslm'
  import { useUserStore } from '@/store/modules/user'

  defineOptions({ name: 'ChangePasswordDialog' })

  const open = defineModel<boolean>({ default: false })
  const userStore = useUserStore()
  const formRef = ref<FormInstance>()
  const saving = ref(false)
  const form = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })

  const rules: FormRules = {
    oldPassword: [{ required: true, message: '请输入原密码', trigger: 'blur' }],
    newPassword: [
      { required: true, message: '请输入新密码', trigger: 'blur' },
      { min: 8, message: '新密码至少需要 8 位', trigger: 'blur' }
    ],
    confirmPassword: [
      { required: true, message: '请再次输入新密码', trigger: 'blur' },
      {
        validator: (_rule, value, callback) =>
          value === form.newPassword ? callback() : callback(new Error('两次输入的新密码不一致')),
        trigger: 'blur'
      }
    ]
  }

  watch(open, (value) => {
    if (value) Object.assign(form, { oldPassword: '', newPassword: '', confirmPassword: '' })
  })

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      const res = await changePassword({ oldPassword: form.oldPassword, newPassword: form.newPassword })
      // 修改密码后旧凭证全部失效，换用服务端返回的新凭证保持当前登录
      userStore.setToken(res.data.token)
      ElMessage.success('密码已修改，其他设备上的登录已失效')
      open.value = false
    } catch {
      // 错误提示已由 api 统一弹出
    } finally {
      saving.value = false
    }
  }
</script>
