package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"dasslm/server/internal/model"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	ChannelWeComWebhook    = "wecom_webhook"
	ChannelFeishuWebhook   = "feishu_webhook"
	ChannelDingTalkWebhook = "dingtalk_webhook"
	ChannelCustomWebhook   = "custom_webhook"

	EventAdminLoginSuccess = "admin_login_success"
	EventCertificateIssued = "certificate_issued"
	EventDeploySuccess     = "deploy_success"

	rulesSettingKey    = "notification.rules"
	wecomCardActionURL = "https://github.com/Qialas/DnsaSSL_Mgr"
)

type Message struct {
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Event     string            `json:"event"`
	Level     string            `json:"level"`
	Variables map[string]string `json:"variables"`
}

type Config struct {
	WebhookURL   string `json:"webhookUrl"`
	Secret       string `json:"secret"`
	Method       string `json:"method"`
	Headers      string `json:"headers"`
	BodyTemplate string `json:"bodyTemplate"`
}

type Rule struct {
	Event      string    `json:"event"`
	Label      string    `json:"label"`
	Enabled    bool      `json:"enabled"`
	ChannelIDs []uint    `json:"channelIds"`
	Templates  Templates `json:"templates"`
}

type Templates struct {
	WeCom    WeComTemplate    `json:"wecom"`
	Feishu   FeishuTemplate   `json:"feishu"`
	DingTalk DingTalkTemplate `json:"dingtalk"`
	Custom   CustomTemplate   `json:"custom"`
}

type WeComTemplate struct {
	MessageType string `json:"messageType"`
	TextContent string `json:"textContent"`
	CardTitle   string `json:"cardTitle"`
	CardContent string `json:"cardContent"`
	CardMode    string `json:"cardMode"`
	CardJSON    string `json:"cardJson"`
}

type FeishuTemplate struct {
	MessageType string `json:"messageType"`
	TextContent string `json:"textContent"`
	CardTitle   string `json:"cardTitle"`
	CardContent string `json:"cardContent"`
	CardMode    string `json:"cardMode"`
	CardJSON    string `json:"cardJson"`
}

type DingTalkTemplate struct {
	MessageType string `json:"messageType"`
	TextContent string `json:"textContent"`
	CardTitle   string `json:"cardTitle"`
	CardContent string `json:"cardContent"`
	CardMode    string `json:"cardMode"`
	CardJSON    string `json:"cardJson"`
}

type CustomTemplate struct {
	BodyTemplate string `json:"bodyTemplate"`
}

type DeliveryResult struct {
	RequestURL     string
	RequestMethod  string
	RequestHeaders string
	RequestBody    string
	ResponseStatus int
	ResponseBody   string
	DurationMS     int64
}

func DefaultRules() []Rule {
	return []Rule{
		{Event: EventAdminLoginSuccess, Label: "管理员登录成功提醒", Enabled: false, ChannelIDs: []uint{}},
		{Event: EventCertificateIssued, Label: "SSL证书签发成功", Enabled: false, ChannelIDs: []uint{}},
		{Event: EventDeploySuccess, Label: "SSL部署成功", Enabled: false, ChannelIDs: []uint{}},
	}
}

func GetRules(db *gorm.DB) ([]Rule, error) {
	var setting model.SystemSetting
	if err := db.Where("setting_key = ?", rulesSettingKey).First(&setting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DefaultRules(), nil
		}
		return nil, err
	}
	var saved []Rule
	if err := json.Unmarshal([]byte(setting.SettingValue), &saved); err != nil {
		return DefaultRules(), nil
	}
	return mergeRules(saved), nil
}

func SaveRules(db *gorm.DB, rules []Rule) ([]Rule, error) {
	merged := mergeRules(rules)
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	setting := model.SystemSetting{
		SettingKey:   rulesSettingKey,
		SettingValue: string(raw),
		ValueType:    "json",
		Description:  "通知行为提醒规则",
	}
	if err := db.Where("setting_key = ?", rulesSettingKey).Assign(setting).FirstOrCreate(&setting).Error; err != nil {
		return nil, err
	}
	if setting.SettingValue != string(raw) {
		if err := db.Model(&setting).Updates(map[string]any{"setting_value": string(raw), "value_type": "json", "description": "通知行为提醒规则"}).Error; err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func DispatchAsync(db *gorm.DB, event string, message Message) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = Dispatch(ctx, db, event, message)
	}()
}

func Dispatch(ctx context.Context, db *gorm.DB, event string, message Message) error {
	rules, err := GetRules(db)
	if err != nil {
		return err
	}
	var rule *Rule
	for i := range rules {
		if rules[i].Event == event {
			rule = &rules[i]
			break
		}
	}
	if rule == nil || !rule.Enabled || len(rule.ChannelIDs) == 0 {
		return nil
	}
	var channels []model.NotificationSetting
	if err := db.Where("id IN ? AND status = ?", rule.ChannelIDs, "enabled").Find(&channels).Error; err != nil {
		return err
	}
	message.Event = event
	var errs []string
	for _, channel := range channels {
		result, err := SendWithResult(ctx, channel, message, rule.Templates)
		status := "success"
		logMessage := "发送成功"
		if err != nil {
			status = "failed"
			logMessage = err.Error()
			errs = append(errs, fmt.Sprintf("%s: %s", channel.Name, err.Error()))
		}
		recordLog(db, event, rule.Label, channel, message, status, logMessage, result)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func Validate(setting *model.NotificationSetting) error {
	if strings.TrimSpace(setting.Name) == "" {
		return errors.New("请输入通知配置名称")
	}
	if _, ok := channelLabels()[setting.Channel]; !ok {
		return errors.New("请选择有效的通知渠道")
	}
	config, err := ParseConfig(setting.Config)
	if err != nil {
		return err
	}
	if strings.TrimSpace(config.WebhookURL) == "" {
		return errors.New("请输入 Webhook 地址")
	}
	if _, err := url.ParseRequestURI(config.WebhookURL); err != nil {
		return errors.New("Webhook 地址格式不正确")
	}
	if setting.Status == "" {
		setting.Status = "enabled"
	}
	return nil
}

func mergeRules(saved []Rule) []Rule {
	byEvent := map[string]Rule{}
	for _, item := range saved {
		byEvent[item.Event] = item
	}
	rules := DefaultRules()
	for i, item := range rules {
		if savedItem, ok := byEvent[item.Event]; ok {
			rules[i].Enabled = savedItem.Enabled
			rules[i].ChannelIDs = uniqueIDs(savedItem.ChannelIDs)
			rules[i].Templates = normalizeTemplates(savedItem.Templates)
		}
	}
	return rules
}

func normalizeTemplates(templates Templates) Templates {
	templates.WeCom.MessageType = normalizeMessageType(templates.WeCom.MessageType)
	templates.Feishu.MessageType = normalizeMessageType(templates.Feishu.MessageType)
	templates.DingTalk.MessageType = normalizeMessageType(templates.DingTalk.MessageType)
	return templates
}

func normalizeMessageType(messageType string) string {
	messageType = strings.ToLower(strings.TrimSpace(messageType))
	if messageType != "text" && messageType != "card" {
		return "card"
	}
	return messageType
}

func uniqueIDs(ids []uint) []uint {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func ParseConfig(raw string) (Config, error) {
	var config Config
	if strings.TrimSpace(raw) == "" {
		return config, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, errors.New("通知渠道配置不是有效 JSON")
	}
	return config, nil
}

func Send(ctx context.Context, setting model.NotificationSetting, message Message) error {
	_, err := SendWithResult(ctx, setting, message)
	return err
}

func SendWithResult(ctx context.Context, setting model.NotificationSetting, message Message, templates ...Templates) (DeliveryResult, error) {
	if setting.Status == "disabled" {
		return DeliveryResult{}, errors.New("通知渠道已停用")
	}
	config, err := ParseConfig(setting.Config)
	if err != nil {
		return DeliveryResult{}, err
	}
	if err := Validate(&setting); err != nil {
		return DeliveryResult{}, err
	}

	endpoint := config.WebhookURL
	headers := map[string]string{"Content-Type": "application/json"}
	var payload any
	switch setting.Channel {
	case ChannelWeComWebhook:
		payload, err = wecomPayload(message, firstTemplate(templates).WeCom)
		if err != nil {
			return DeliveryResult{}, err
		}
	case ChannelFeishuWebhook:
		body, err := feishuPayload(message, firstTemplate(templates).Feishu)
		if err != nil {
			return DeliveryResult{}, err
		}
		if config.Secret != "" {
			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			body["timestamp"] = timestamp
			body["sign"] = feishuSign(timestamp, config.Secret)
		}
		payload = body
	case ChannelDingTalkWebhook:
		if config.Secret != "" {
			endpoint, err = dingtalkSignedURL(endpoint, config.Secret)
			if err != nil {
				return DeliveryResult{}, err
			}
		}
		payload, err = dingtalkPayload(message, firstTemplate(templates).DingTalk)
		if err != nil {
			return DeliveryResult{}, err
		}
	case ChannelCustomWebhook:
		bodyTemplate := firstTemplate(templates).Custom.BodyTemplate
		if strings.TrimSpace(bodyTemplate) == "" {
			bodyTemplate = config.BodyTemplate
		}
		for key, value := range parseHeaders(config.Headers) {
			headers[key] = value
		}
		method := strings.ToUpper(strings.TrimSpace(config.Method))
		if method == "" {
			method = http.MethodPost
		}
		return sendRaw(ctx, method, endpoint, headers, renderBody(bodyTemplate, message))
	default:
		return DeliveryResult{}, errors.New("暂不支持该通知渠道")
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return DeliveryResult{}, err
	}
	return sendRaw(ctx, http.MethodPost, endpoint, headers, string(raw))
}

func channelLabels() map[string]string {
	return map[string]string{
		ChannelWeComWebhook:    "企业微信",
		ChannelFeishuWebhook:   "飞书",
		ChannelDingTalkWebhook: "钉钉",
		ChannelCustomWebhook:   "自定义 Webhook",
	}
}

func firstTemplate(templates []Templates) Templates {
	if len(templates) == 0 {
		return normalizeTemplates(Templates{})
	}
	return normalizeTemplates(templates[0])
}

func wecomPayload(message Message, template WeComTemplate) (map[string]any, error) {
	if strings.EqualFold(strings.TrimSpace(template.MessageType), "text") {
		return map[string]any{
			"msgtype": "text",
			"text": map[string]string{
				"content": renderTemplateText(template.TextContent, message),
			},
		}, nil
	}
	if payload, ok, err := parseTemplateJSON(template.CardMode, template.CardJSON, message); ok || err != nil {
		if err == nil {
			normalizeWeComTemplateCardPayload(payload)
		}
		return payload, err
	}
	return map[string]any{
		"msgtype":       "template_card",
		"template_card": wecomCard(message, template),
	}, nil
}

func wecomCard(message Message, template WeComTemplate) map[string]any {
	return map[string]any{
		"card_type": "text_notice",
		"source": map[string]any{
			"desc":       "DaSSLm",
			"desc_color": wecomDescColor(message.Level),
		},
		"main_title": map[string]string{
			"title": renderTemplateValue(template.CardTitle, message, notificationText(message.Title, "DaSSLm 通知")),
			"desc":  renderTemplateValue(template.CardContent, message, notificationText(message.Content, "-")),
		},
		"emphasis_content": map[string]string{
			"title": eventLabel(message.Event),
			"desc":  "事件类型",
		},
		"horizontal_content_list": wecomCardContentList(message),
		"card_action": map[string]any{
			"type": 1,
			"url":  wecomCardActionURL,
		},
	}
}

func wecomCardContentList(message Message) []map[string]string {
	item := func(key, value string) map[string]string {
		return map[string]string{"keyname": key, "value": notificationText(value, "-")}
	}
	switch message.Event {
	case EventAdminLoginSuccess:
		return []map[string]string{
			item("账号", message.Variables["username"]),
			item("IP", message.Variables["ip"]),
			item("登录事件", eventLabel(message.Event)),
		}
	case EventCertificateIssued:
		return []map[string]string{
			item("域名", message.Variables["domain"]),
			item("到期时间", message.Variables["expiresAt"]),
			item("证书事件", eventLabel(message.Event)),
		}
	case EventDeploySuccess:
		targetKey := "部署目标"
		targetValue := message.Variables["target"]
		if strings.TrimSpace(targetValue) == "" {
			targetKey = "部署数量"
			targetValue = message.Variables["targetCount"]
		}
		return []map[string]string{
			item("域名", message.Variables["domain"]),
			item(targetKey, targetValue),
			item("部署事件", eventLabel(message.Event)),
		}
	default:
		return []map[string]string{
			item("事件", eventLabel(message.Event)),
			item("等级", levelLabel(message.Level)),
		}
	}
}

func normalizeWeComTemplateCardPayload(payload map[string]any) {
	if payload == nil || payload["msgtype"] != "template_card" {
		return
	}
	card, ok := payload["template_card"].(map[string]any)
	if !ok {
		return
	}
	action, ok := card["card_action"].(map[string]any)
	if !ok || action == nil {
		card["card_action"] = map[string]any{"type": 1, "url": wecomCardActionURL}
		return
	}
	if _, ok := action["type"]; !ok {
		action["type"] = 1
	}
	if fmt.Sprint(action["type"]) == "1" && strings.TrimSpace(fmt.Sprint(action["url"])) == "" {
		action["url"] = wecomCardActionURL
	}
}

func wecomDescColor(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error", "danger", "failed":
		return 2
	case "warning", "warn":
		return 1
	default:
		return 0
	}
}

func feishuPayload(message Message, template FeishuTemplate) (map[string]any, error) {
	if strings.EqualFold(strings.TrimSpace(template.MessageType), "text") {
		return map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": renderTemplateText(template.TextContent, message)},
		}, nil
	}
	if payload, ok, err := parseTemplateJSON(template.CardMode, template.CardJSON, message); ok || err != nil {
		return payload, err
	}
	return map[string]any{
		"msg_type": "interactive",
		"card":     feishuCard(message, template),
	}, nil
}

func feishuCard(message Message, template FeishuTemplate) map[string]any {
	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"template": feishuCardTemplate(message.Level),
			"title": map[string]string{
				"tag":     "plain_text",
				"content": renderTemplateValue(template.CardTitle, message, notificationText(message.Title, "DaSSLm 通知")),
			},
		},
		"elements": []any{
			map[string]any{
				"tag": "div",
				"text": map[string]string{
					"tag":     "lark_md",
					"content": renderTemplateValue(template.CardContent, message, notificationText(message.Content, "-")),
				},
			},
			map[string]any{
				"tag": "hr",
			},
			map[string]any{
				"tag": "note",
				"elements": []any{
					map[string]string{
						"tag":     "plain_text",
						"content": "事件类型：" + eventLabel(message.Event),
					},
					map[string]string{
						"tag":     "plain_text",
						"content": "消息等级：" + levelLabel(message.Level),
					},
				},
			},
		},
	}
}

func dingtalkPayload(message Message, template DingTalkTemplate) (map[string]any, error) {
	if strings.EqualFold(strings.TrimSpace(template.MessageType), "text") {
		return map[string]any{
			"msgtype": "text",
			"text": map[string]string{
				"content": renderTemplateText(template.TextContent, message),
			},
		}, nil
	}
	if payload, ok, err := parseTemplateJSON(template.CardMode, template.CardJSON, message); ok || err != nil {
		return payload, err
	}
	return map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": renderTemplateValue(template.CardTitle, message, notificationText(message.Title, "DaSSLm 通知")),
			"text":  renderTemplateValue(template.CardContent, message, defaultMarkdownContent(message)),
		},
	}, nil
}

func feishuCardTemplate(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error", "danger", "failed":
		return "red"
	case "warning", "warn":
		return "orange"
	case "success":
		return "green"
	default:
		return "blue"
	}
}

func notificationText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func renderTemplateText(template string, message Message) string {
	if strings.TrimSpace(template) == "{{title}}\n{{content}}" {
		template = ""
	}
	return renderTemplateValue(template, message, defaultPlainContent(message))
}

func renderTemplateValue(template string, message Message, fallback string) string {
	if strings.TrimSpace(template) == "" {
		return fallback
	}
	return renderBody(template, message)
}

func parseTemplateJSON(mode, raw string, message Message) (map[string]any, bool, error) {
	if !strings.EqualFold(strings.TrimSpace(mode), "code") || strings.TrimSpace(raw) == "" {
		return nil, false, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(renderBody(raw, message)), &payload); err != nil {
		return nil, true, errors.New("卡片源码不是有效 JSON")
	}
	return payload, true, nil
}

func sendRaw(ctx context.Context, method, endpoint string, headers map[string]string, body string) (DeliveryResult, error) {
	startedAt := time.Now()
	result := DeliveryResult{
		RequestURL:     maskWebhookURL(endpoint),
		RequestMethod:  method,
		RequestHeaders: marshalHeaders(headers),
		RequestBody:    body,
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader([]byte(body)))
	if err != nil {
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result, err
	}
	for key, value := range headers {
		if strings.TrimSpace(key) != "" {
			req.Header.Set(key, value)
		}
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	result.DurationMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		return result, err
	}
	defer res.Body.Close()
	result.ResponseStatus = res.StatusCode
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	result.ResponseBody = strings.TrimSpace(string(raw))
	if res.StatusCode >= http.StatusBadRequest {
		return result, fmt.Errorf("Webhook 返回 HTTP %d：%s", res.StatusCode, result.ResponseBody)
	}
	return result, nil
}

func feishuSign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func dingtalkSignedURL(endpoint, secret string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	query := parsed.Query()
	query.Set("timestamp", timestamp)
	query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func parseHeaders(raw string) map[string]string {
	headers := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			headers[key] = value
		}
	}
	return headers
}

func renderBody(template string, message Message) string {
	if strings.TrimSpace(template) == "" {
		raw, _ := json.Marshal(message)
		return string(raw)
	}
	pairs := []string{
		"{{title}}", message.Title,
		"{{content}}", message.Content,
		"{{event}}", eventLabel(message.Event),
		"{{level}}", levelLabel(message.Level),
		"{{eventCode}}", message.Event,
		"{{levelCode}}", message.Level,
	}
	for key, value := range message.Variables {
		pairs = append(pairs, "{{"+key+"}}", value)
	}
	replacer := strings.NewReplacer(pairs...)
	return replacer.Replace(template)
}

func defaultPlainContent(message Message) string {
	return strings.Join([]string{
		"标题：" + notificationText(message.Title, "DaSSLm 通知"),
		"内容：" + notificationText(message.Content, "-"),
		"事件类型：" + eventLabel(message.Event),
		"消息等级：" + levelLabel(message.Level),
	}, "\n")
}

func defaultMarkdownContent(message Message) string {
	return strings.Join([]string{
		"### " + notificationText(message.Title, "DaSSLm 通知"),
		"",
		"**内容：** " + notificationText(message.Content, "-"),
		"",
		"> 事件类型：" + eventLabel(message.Event),
		"> 消息等级：" + levelLabel(message.Level),
	}, "\n")
}

func eventLabel(event string) string {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case EventAdminLoginSuccess:
		return "管理员登录成功"
	case EventCertificateIssued:
		return "SSL证书签发成功"
	case EventDeploySuccess:
		return "SSL部署成功"
	case "test":
		return "测试"
	default:
		return notificationText(event, "-")
	}
}

func levelLabel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "info":
		return "信息"
	case "success":
		return "成功"
	case "warning", "warn":
		return "警告"
	case "error", "danger":
		return "错误"
	case "failed":
		return "失败"
	default:
		return notificationText(level, "-")
	}
}

func recordLog(db *gorm.DB, event, eventName string, setting model.NotificationSetting, message Message, status, logMessage string, result DeliveryResult) {
	if db == nil {
		return
	}
	_ = db.Create(&model.NotificationLog{
		Event:          event,
		EventName:      eventName,
		ChannelID:      setting.ID,
		Channel:        setting.Channel,
		ChannelName:    setting.Name,
		Title:          message.Title,
		Content:        message.Content,
		Status:         status,
		Message:        truncate(logMessage, 512),
		RequestURL:     result.RequestURL,
		RequestMethod:  result.RequestMethod,
		RequestHeaders: result.RequestHeaders,
		RequestBody:    result.RequestBody,
		ResponseStatus: result.ResponseStatus,
		ResponseBody:   result.ResponseBody,
		DurationMS:     result.DurationMS,
	}).Error
}

func marshalHeaders(headers map[string]string) string {
	safe := map[string]string{}
	for key, value := range headers {
		if isSensitiveHeader(key) {
			safe[key] = "***"
			continue
		}
		safe[key] = value
	}
	raw, _ := json.Marshal(safe)
	return string(raw)
}

func isSensitiveHeader(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return normalized == "authorization" || normalized == "cookie" || normalized == "x-api-key" || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret")
}

func maskWebhookURL(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	parts := strings.Split(parsed.Path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.TrimSpace(parts[i]) != "" {
			parts[i] = "***"
			break
		}
	}
	parsed.Path = strings.Join(parts, "/")
	query := parsed.Query()
	for key := range query {
		query.Set(key, "***")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func truncate(text string, limit int) string {
	if len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}
