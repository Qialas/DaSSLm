<!-- 通知模板编辑：单个渠道（企业微信 / 飞书 / 钉钉 / 自定义） -->
<template>
  <div class="template-panel">
    <div class="template-panel__head">
      <strong>{{ channel.label }}</strong>
      <div class="template-panel__vars">
        <span v-for="item in variables" :key="item.value">
          <b>{{ item.label }}</b><code>{{ item.value }}</code>
        </span>
      </div>
      <ElRadioGroup v-if="!channel.custom" v-model="model.messageType" size="small">
        <ElRadioButton value="card">卡片</ElRadioButton>
        <ElRadioButton value="text">文本</ElRadioButton>
      </ElRadioGroup>
    </div>

    <ElInput
      v-if="channel.custom"
      v-model="model.bodyTemplate"
      type="textarea"
      :autosize="{ minRows: 12, maxRows: 18 }"
      class="template-panel__code"
      :placeholder="customPlaceholder"
    />
    <ElInput
      v-else-if="model.messageType === 'text'"
      v-model="model.textContent"
      type="textarea"
      :autosize="{ minRows: 10, maxRows: 16 }"
      class="template-panel__code"
      :placeholder="defaultTextTemplate"
    />
    <template v-else>
      <ElSegmented v-model="model.cardMode" :options="modeOptions" class="mb-3" />
      <div v-if="model.cardMode === 'visual'" class="template-panel__visual">
        <div class="flex flex-col gap-3">
          <ElInput v-model="model.cardTitle" placeholder="{{title}}">
            <template #prepend>标题</template>
          </ElInput>
          <ElInput v-model="model.cardContent" type="textarea" :autosize="{ minRows: 8, maxRows: 14 }" placeholder="{{content}}" />
        </div>
        <div class="template-panel__preview" :class="`is-${channel.key}`">
          <span>{{ channel.label }}</span>
          <strong>{{ preview(model.cardTitle, sample.title) }}</strong>
          <p>{{ preview(model.cardContent, sample.content) }}</p>
          <em>事件：{{ sample.event }}</em>
        </div>
      </div>
      <ElInput
        v-else
        v-model="model.cardJson"
        type="textarea"
        :autosize="{ minRows: 14, maxRows: 22 }"
        class="template-panel__code"
        :placeholder="defaultCardJson[channel.key]"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
  import type { Row } from '@/api/dasslm'

  defineOptions({ name: 'SettingsTemplatePanel' })

  const props = defineProps<{
    channel: { key: string; label: string; custom?: boolean }
    variables: { label: string; value: string }[]
    sample: Row
  }>()

  /** 当前渠道的模板对象，直接修改其字段 */
  const model = defineModel<Row>({ required: true })

  const modeOptions = [
    { label: '可视化编辑', value: 'visual' },
    { label: '代码编辑', value: 'code' }
  ]

  const customPlaceholder = '{"title":"{{title}}","content":"{{content}}","event":"{{event}}","level":"{{level}}"}'
  const defaultTextTemplate = '标题：{{title}}\n内容：{{content}}\n事件类型：{{event}}\n消息等级：{{level}}'

  const defaultCardJson: Record<string, string> = {
    wecom: `{
  "msgtype": "template_card",
  "template_card": {
    "card_type": "text_notice",
    "source": { "desc": "DaSSLm", "desc_color": 0 },
    "main_title": { "title": "{{title}}", "desc": "{{content}}" },
    "horizontal_content_list": [
      { "keyname": "事件", "value": "{{event}}" }
    ],
    "card_action": {
      "type": 1,
      "url": "https://github.com/Qialas/DnsaSSL_Mgr"
    }
  }
}`,
    feishu: `{
  "msg_type": "interactive",
  "card": {
    "config": { "wide_screen_mode": true },
    "header": {
      "template": "green",
      "title": { "tag": "plain_text", "content": "{{title}}" }
    },
    "elements": [
      { "tag": "div", "text": { "tag": "lark_md", "content": "{{content}}" } },
      { "tag": "hr" },
      { "tag": "note", "elements": [{ "tag": "plain_text", "content": "事件：{{event}}" }] }
    ]
  }
}`,
    dingtalk: `{
  "msgtype": "markdown",
  "markdown": {
    "title": "{{title}}",
    "text": "### {{title}}\\n\\n{{content}}\\n\\n> 事件：{{event}}"
  }
}`
  }

  const eventLabels: Record<string, string> = {
    admin_login_success: '管理员登录成功',
    certificate_issued: 'SSL证书签发成功',
    deploy_success: 'SSL部署成功',
    test: '测试'
  }

  const levelLabels: Record<string, string> = {
    info: '信息',
    success: '成功',
    warning: '警告',
    warn: '警告',
    error: '错误',
    danger: '错误',
    failed: '失败'
  }

  /** 用示例消息替换模板变量，生成预览文本 */
  function preview(template: string, fallback: string) {
    const message = props.sample
    let text = String(template || fallback || '')
      .replaceAll('{{title}}', message.title)
      .replaceAll('{{content}}', message.content)
      .replaceAll('{{event}}', eventLabels[message.event] || message.event || '-')
      .replaceAll('{{level}}', levelLabels[message.level] || message.level || '-')
      .replaceAll('{{eventCode}}', message.event)
      .replaceAll('{{levelCode}}', message.level)
    Object.entries((message.variables || {}) as Record<string, string>).forEach(([key, value]) => {
      text = text.replaceAll(`{{${key}}}`, value)
    })
    return text
  }
</script>

<style scoped lang="scss">
  .template-panel__head {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;

    strong {
      font-size: 14px;
      color: var(--art-gray-900);
    }
  }

  .template-panel__vars {
    display: flex;
    flex: 1;
    flex-wrap: wrap;
    gap: 6px;

    span {
      display: inline-flex;
      gap: 4px;
      align-items: center;
      padding: 2px 8px;
      font-size: 12px;
      background: var(--art-gray-200);
      border-radius: 4px;
    }

    b {
      font-weight: 500;
      color: var(--art-gray-700);
    }

    code {
      color: var(--el-color-primary);
    }
  }

  .template-panel__code :deep(textarea) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
  }

  .template-panel__visual {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 14px;

    @media (max-width: 640px) {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  .template-panel__preview {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px;
    background: var(--art-gray-100);
    border: 1px solid var(--default-border);
    border-top: 3px solid var(--el-color-primary);
    border-radius: 8px;

    span {
      font-size: 12px;
      color: var(--art-gray-500);
    }

    strong {
      font-size: 15px;
      color: var(--art-gray-900);
    }

    p {
      margin: 0;
      font-size: 13px;
      line-height: 1.6;
      color: var(--art-gray-700);
      white-space: pre-wrap;
    }

    em {
      font-size: 12px;
      font-style: normal;
      color: var(--art-gray-500);
    }

    &.is-wecom {
      border-top-color: #07c160;
    }

    &.is-feishu {
      border-top-color: #3370ff;
    }

    &.is-dingtalk {
      border-top-color: #ff8a00;
    }
  }
</style>
