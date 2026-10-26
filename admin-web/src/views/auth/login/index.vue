<template>
  <div class="dasslm-login">
    <LoginLeftView />
    <div class="dasslm-login-panel">
      <AuthTopBar />
      <div class="dasslm-login-form">
        <h1>欢迎回来</h1>
        <p>登录 DaSSLm 管理后台</p>
        <ElAlert v-if="error" :title="error" type="error" :closable="false" show-icon />
        <ElForm ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
          <ElFormItem label="用户名" prop="username">
            <ElInput v-model="form.username" aria-label="用户名" placeholder="请输入用户名" size="large" autocomplete="username" />
          </ElFormItem>
          <ElFormItem label="密码" prop="password">
            <ElInput v-model="form.password" type="password" show-password aria-label="密码" placeholder="请输入密码" size="large" autocomplete="current-password" />
          </ElFormItem>
          <ElButton native-type="submit" type="primary" size="large" :loading="loading" class="dasslm-login-submit">登录控制台</ElButton>
        </ElForm>
        <span class="dasslm-login-foot">域名解析 · SSL 证书 · 自动化部署</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { reactive, ref } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import type { FormInstance, FormRules } from 'element-plus'
  import LoginLeftView from '@/components/core/views/login/LoginLeftView.vue'
  import AuthTopBar from '@/components/core/views/login/AuthTopBar.vue'
  import { fetchLogin } from '@/api/auth'
  import { useUserStore } from '@/store/modules/user'

  const form = reactive({ username: '', password: '' })
  const rules: FormRules = {
    username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
    password: [{ required: true, message: '请输入密码', trigger: 'blur' }]
  }
  const formRef = ref<FormInstance>()
  const loading = ref(false)
  const error = ref('')
  const route = useRoute()
  const router = useRouter()
  const user = useUserStore()

  async function submit() {
    if (loading.value || !(await formRef.value?.validate().catch(() => false))) return
    error.value = ''
    loading.value = true
    try {
      const result = await fetchLogin({ userName: form.username.trim(), password: form.password })
      if (!result.token) throw new Error('登录失败：未收到登录凭证')
      user.setToken(result.token)
      user.setUserInfo(result.user)
      user.setLoginStatus(true)
      const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : ''
      await router.replace(redirect.startsWith('/') && !redirect.startsWith('//') && !redirect.startsWith('/auth/login') && redirect !== '/login' ? redirect : '/dashboard')
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '登录失败，请稍后重试'
    } finally {
      loading.value = false
    }
  }
</script>

<style scoped>
  .dasslm-login { display: flex; min-height: 100dvh; background: var(--default-box-color); }
  :deep(.login-left-view) { width: 55%; height: 100dvh; }
  .dasslm-login-panel { position: relative; display: grid; flex: 1; min-width: 0; place-items: center; padding: 80px 32px; }
  .dasslm-login-form { width: 100%; max-width: 380px; }
  h1 { font-size: 32px; font-weight: 650; color: var(--art-gray-900); }
  p { margin: 12px 0 32px; color: var(--art-gray-600); }
  .el-alert { margin-bottom: 20px; }
  .dasslm-login-submit { width: 100%; margin-top: 8px; }
  .dasslm-login-foot { display: block; margin-top: 28px; text-align: center; font-size: 12px; color: var(--art-gray-500); }
  @media (max-width: 900px) { :deep(.login-left-view) { display: none; } }
  @media (max-width: 480px) { .dasslm-login-panel { padding: 90px 24px 48px; } }
</style>
