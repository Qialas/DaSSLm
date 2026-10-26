<!-- 新建/编辑自动部署任务（自动任务页与证书页共用） -->
<template>
  <ElDrawer v-model="visible" :title="`${task?.id ? '编辑' : '新建'}自动部署任务`" :size="drawerSize" destroy-on-close>
    <ElForm ref="formRef" :model="form" :rules="rules" label-position="top">
      <ElFormItem label="任务名称" prop="name">
        <ElInput v-model="form.name" placeholder="默认使用证书域名自动生成" />
      </ElFormItem>
      <ElFormItem v-if="!fixedCertificate" label="SSL证书" prop="certificateId">
        <CertificatePicker
          v-model="form.certificateId"
          :certificates="certificates"
          :ssl-account-map="sslAccountMap"
          @change="onCertificateChange"
        />
      </ElFormItem>
      <ElFormItem label="部署服务" required>
        <DeployTargetsEditor v-model="form.targets" :deploy-accounts="deployAccounts" :default-site-name="currentCertificate?.commonName" />
      </ElFormItem>
      <ElFormItem label="任务状态" prop="status">
        <ElSelect v-model="form.status" class="w-full">
          <ElOption v-for="item in enabledStatusOptions" :key="item.value" :label="item.label" :value="item.value" />
        </ElSelect>
      </ElFormItem>
      <ElFormItem label="任务备注" prop="remark">
        <ElInput v-model="form.remark" type="textarea" :rows="3" />
      </ElFormItem>
    </ElForm>
    <template #footer>
      <ElButton @click="visible = false">取消</ElButton>
      <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
    </template>
  </ElDrawer>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { createResource, updateResource, type Row } from '@/api/dasslm'
  import CertificatePicker from './CertificatePicker.vue'
  import DeployTargetsEditor from './DeployTargetsEditor.vue'
  import { enabledStatusOptions, toMap, useResponsiveSize } from './meta'

  defineOptions({ name: 'DeployTaskDrawer' })

  const props = defineProps<{
    /** 正在编辑的任务，新建时为空 */
    task?: Row | null
    deployAccounts: Row[]
    /** 可选证书（自动任务页使用） */
    certificates?: Row[]
    sslAccountMap?: Record<string, Row>
    /** 固定证书（证书页使用，不显示证书选择） */
    fixedCertificate?: Row | null
  }>()
  const visible = defineModel<boolean>({ default: false })
  const emit = defineEmits<{ (e: 'saved'): void }>()

  const narrowSize = useResponsiveSize(640)
  const wideSize = useResponsiveSize(760)
  const drawerSize = computed(() => (props.fixedCertificate ? narrowSize.value : wideSize.value))
  const formRef = ref<FormInstance>()
  const saving = ref(false)
  const form = reactive<Row>({ name: '', certificateId: undefined, targets: [], status: 'enabled', remark: '' })
  const rules: FormRules = {
    certificateId: [{ required: true, message: '请选择SSL证书', trigger: 'change' }],
    status: [{ required: true, message: '请选择状态', trigger: 'change' }]
  }

  const certificates = computed(() => props.certificates || [])
  const sslAccountMap = computed(() => props.sslAccountMap || {})
  const certificateMap = computed(() => toMap(certificates.value))
  const currentCertificate = computed<Row | undefined>(
    () => props.fixedCertificate || certificateMap.value[form.certificateId] || undefined
  )

  function init() {
    const task = props.task
    Object.assign(form, {
      name: task?.name || '',
      certificateId: props.fixedCertificate?.id ?? task?.certificateId ?? undefined,
      targets: [...(task?.targets || [])],
      status: task?.status || 'enabled',
      remark: task?.remark || ''
    })
    nextTick(() => formRef.value?.clearValidate())
  }

  // 打开前先填充表单，避免闪现上一次的数据
  watch(visible, (open) => {
    if (open) init()
  })

  function onCertificateChange(_id: number, cert: Row) {
    formRef.value?.validateField('certificateId').catch(() => undefined)
    // 只有一个未填写站点的目标时，自动补全为证书主域名
    if (cert.commonName && form.targets.length === 1 && !form.targets[0]?.siteName) {
      form.targets = [{ ...form.targets[0], siteName: cert.commonName }]
    }
  }

  function buildPayload() {
    const cert = currentCertificate.value
    return {
      name: form.name || `${cert?.commonName || 'SSL证书'} 自动部署`,
      certificateId: Number(props.fixedCertificate?.id ?? form.certificateId),
      targets: (form.targets as Row[])
        .filter((item) => Number(item.deployAccountId) > 0)
        .map((item) => ({
          deployAccountId: Number(item.deployAccountId),
          siteName: item.siteName || cert?.commonName || '',
          productType: item.productType || '',
          region: item.region || '',
          resourceId: item.resourceId || '',
          resourceName: item.resourceName || '',
          extraConfig: item.extraConfig || '',
          status: item.status || 'enabled',
          remark: item.remark || ''
        })),
      status: form.status || 'enabled',
      remark: form.remark || ''
    }
  }

  async function submit() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    const payload = buildPayload()
    if (!payload.certificateId) {
      ElMessage.warning('请选择SSL证书')
      return
    }
    if (!payload.targets.length) {
      ElMessage.warning('请至少添加一个部署目标')
      return
    }
    saving.value = true
    try {
      if (props.task?.id) await updateResource('tasks', props.task.id, payload)
      else await createResource('tasks', payload)
      const autoRun = !props.fixedCertificate || (props.fixedCertificate.status === 'issued' && payload.status === 'enabled')
      ElMessage.success(autoRun ? '已保存，系统将自动执行部署' : '已保存部署目标')
      visible.value = false
      emit('saved')
    } finally {
      saving.value = false
    }
  }
</script>
