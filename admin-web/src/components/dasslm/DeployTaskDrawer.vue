<!-- 新建/编辑自动部署任务（自动任务页与证书页共用） -->
<template>
  <ElDrawer
    v-model="visible"
    :title="`${task?.id ? '编辑' : '新建'}自动部署任务`"
    :size="drawerSize"
    class="task-drawer"
    destroy-on-close
  >
    <ElForm ref="formRef" :model="form" :rules="rules" label-position="top" class="task-form">
      <section class="task-section">
        <div class="task-section__head">
          <span class="task-section__index">1</span>
          <div>
            <div class="task-section__title">基本信息</div>
            <div class="task-section__desc">任务名称留空时按证书域名自动生成</div>
          </div>
        </div>
        <div class="task-basic">
          <ElFormItem label="任务名称" prop="name" class="task-basic__name">
            <ElInput v-model="form.name" :placeholder="defaultName" maxlength="100" />
          </ElFormItem>
          <ElFormItem label="启用任务" prop="status" class="task-basic__status">
            <ElSwitch v-model="form.status" active-value="enabled" inactive-value="disabled" inline-prompt active-text="开" inactive-text="关" />
          </ElFormItem>
        </div>
        <ElFormItem label="备注" prop="remark" class="!mb-0">
          <ElInput v-model="form.remark" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" placeholder="可选，例如用途、负责人" />
        </ElFormItem>
      </section>

      <section class="task-section">
        <div class="task-section__head">
          <span class="task-section__index">2</span>
          <div>
            <div class="task-section__title">SSL 证书</div>
            <div class="task-section__desc">证书签发或续期成功后，会自动部署到下方的部署目标</div>
          </div>
        </div>
        <div v-if="fixedCertificate" class="task-cert-fixed">
          <ArtSvgIcon icon="ri:shield-keyhole-line" class="task-cert-fixed__icon" />
          <span class="truncate">{{ fixedCertificate.commonName }}</span>
        </div>
        <ElFormItem v-else prop="certificateId" class="!mb-0">
          <CertificatePicker
            v-model="form.certificateId"
            :certificates="certificates"
            :ssl-account-map="sslAccountMap"
            @change="onCertificateChange"
          />
        </ElFormItem>
      </section>

      <section class="task-section">
        <div class="task-section__head">
          <span class="task-section__index">3</span>
          <div>
            <div class="task-section__title">部署目标</div>
            <div class="task-section__desc">可添加多个，执行时逐个部署；停用的目标会被跳过</div>
          </div>
        </div>
        <DeployTargetsEditor v-model="form.targets" :deploy-accounts="deployAccounts" :default-site-name="currentCertificate?.commonName" />
      </section>
    </ElForm>
    <template #footer>
      <div class="task-footer">
        <span class="task-footer__hint">
          {{ form.targets.length ? `共 ${form.targets.length} 个部署目标` : '还没有添加部署目标' }}
        </span>
        <div>
          <ElButton @click="visible = false">取消</ElButton>
          <ElButton type="primary" :loading="saving" @click="submit">保存</ElButton>
        </div>
      </div>
    </template>
  </ElDrawer>
</template>

<script setup lang="ts">
  import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
  import { createResource, updateResource, type Row } from '@/api/dasslm'
  import CertificatePicker from './CertificatePicker.vue'
  import DeployTargetsEditor from './DeployTargetsEditor.vue'
  import { toMap, useResponsiveSize } from './meta'

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
    certificateId: [{ required: true, message: '请选择SSL证书', trigger: 'change' }]
  }

  const certificates = computed(() => props.certificates || [])
  const defaultName = computed(() => `${currentCertificate.value?.commonName || 'SSL证书'} 自动部署`)
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
      name: form.name.trim() || defaultName.value,
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

<style scoped lang="scss">
  .task-form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .task-section {
    padding: 16px;
    background: var(--default-box-color);
    border: 1px solid var(--default-border);
    border-radius: calc(var(--custom-radius) / 2 + 4px);
  }

  .task-section__head {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    margin-bottom: 14px;
  }

  .task-section__index {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    margin-top: 1px;
    font-size: 12px;
    font-weight: 600;
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    border-radius: 50%;
  }

  .task-section__title {
    font-size: 15px;
    font-weight: 600;
    color: var(--art-gray-900);
  }

  .task-section__desc {
    margin-top: 2px;
    font-size: 12px;
    color: var(--art-gray-500);
  }

  .task-basic {
    display: flex;
    gap: 16px;
    align-items: flex-start;
  }

  .task-basic__name {
    flex: 1;
    min-width: 0;
  }

  .task-basic__status {
    flex: none;
  }

  .task-cert-fixed {
    display: flex;
    gap: 8px;
    align-items: center;
    min-width: 0;
    padding: 10px 12px;
    font-size: 14px;
    color: var(--art-gray-900);
    background: var(--art-gray-100);
    border-radius: 6px;
  }

  .task-cert-fixed__icon {
    flex: none;
    font-size: 18px;
    color: var(--el-color-primary);
  }

  .task-footer {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
  }

  .task-footer__hint {
    font-size: 13px;
    color: var(--art-gray-500);
  }
</style>
