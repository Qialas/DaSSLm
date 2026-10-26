package model

import "time"

type AutoTask struct {
	BaseModel
	Name            string           `gorm:"size:128;not null;comment:自动任务名称" json:"name"`
	TaskType        string           `gorm:"size:64;not null;comment:任务类型：ssl_deploy证书部署" json:"taskType"`
	CronExpr        string           `gorm:"size:64;comment:兼容旧版本Cron表达式，当前自动执行任务不使用" json:"cronExpr"`
	TargetID        uint             `gorm:"comment:任务目标资源ID" json:"targetId"`
	TargetType      string           `gorm:"size:64;comment:任务目标类型：certificate" json:"targetType"`
	DeployAccountID uint             `gorm:"index;comment:关联部署账号ID" json:"deployAccountId"`
	CertificateID   uint             `gorm:"index;comment:关联SSL证书ID" json:"certificateId"`
	SiteName        string           `gorm:"size:255;comment:部署目标站点名称" json:"siteName"`
	LastRunAt       *time.Time       `gorm:"comment:最近执行时间" json:"lastRunAt"`
	NextRunAt       *time.Time       `gorm:"comment:下次执行时间，自动执行任务为空" json:"nextRunAt"`
	LastRunStatus   string           `gorm:"size:32;comment:最近执行状态：success成功 failed失败 running运行中" json:"lastRunStatus"`
	Status          string           `gorm:"size:32;not null;default:enabled;comment:任务状态：enabled启用 disabled停用" json:"status"`
	Remark          string           `gorm:"size:255;comment:备注信息" json:"remark"`
	Targets         []AutoTaskTarget `gorm:"foreignKey:TaskID" json:"targets,omitempty"`
}

func (AutoTask) TableName() string {
	return "dasslm_auto_tasks"
}

type AutoTaskTarget struct {
	BaseModel
	TaskID          uint       `gorm:"index;not null;comment:关联自动任务ID" json:"taskId"`
	DeployAccountID uint       `gorm:"index;not null;comment:关联部署账号ID" json:"deployAccountId"`
	SiteName        string     `gorm:"size:255;not null;comment:部署目标站点或服务名称" json:"siteName"`
	ProductType     string     `gorm:"size:64;comment:腾讯云产品类型，如ssl、cdn、eo、clb、waf、apigateway、cos" json:"productType"`
	Region          string     `gorm:"size:64;comment:腾讯云地域，部分产品需要" json:"region"`
	ResourceID      string     `gorm:"size:255;comment:腾讯云资源ID或域名" json:"resourceId"`
	ResourceName    string     `gorm:"size:255;comment:腾讯云资源名称" json:"resourceName"`
	ExtraConfig     string     `gorm:"type:text;comment:部署目标扩展配置JSON" json:"extraConfig"`
	LastRunAt       *time.Time `gorm:"comment:最近执行时间" json:"lastRunAt"`
	LastRunStatus   string     `gorm:"size:32;comment:最近执行状态：success成功 failed失败 running运行中" json:"lastRunStatus"`
	Status          string     `gorm:"size:32;not null;default:enabled;comment:目标状态：enabled启用 disabled停用" json:"status"`
	Remark          string     `gorm:"size:255;comment:备注信息" json:"remark"`
}

func (AutoTaskTarget) TableName() string {
	return "dasslm_auto_task_targets"
}

type TaskLog struct {
	BaseModel
	TaskName   string     `gorm:"size:128;not null;comment:任务名称" json:"taskName"`
	TaskType   string     `gorm:"size:64;not null;index;comment:任务类型：certificate_apply证书申请 certificate_issue证书验证签发" json:"taskType"`
	TargetID   uint       `gorm:"index;comment:任务目标资源ID" json:"targetId"`
	TargetType string     `gorm:"size:64;index;comment:任务目标类型：certificate、domain、account" json:"targetType"`
	TargetName string     `gorm:"size:255;comment:任务目标名称" json:"targetName"`
	Status     string     `gorm:"size:32;index;comment:执行状态：running执行中 success成功 failed失败" json:"status"`
	Message    string     `gorm:"size:512;comment:执行结果说明" json:"message"`
	StartedAt  *time.Time `gorm:"comment:开始时间" json:"startedAt"`
	FinishedAt *time.Time `gorm:"comment:结束时间" json:"finishedAt"`
	DurationMs int64      `gorm:"comment:执行耗时毫秒" json:"durationMs"`
	Detail     string     `gorm:"type:longtext;comment:任务详情JSON" json:"detail"`
}

func (TaskLog) TableName() string {
	return "dasslm_task_logs"
}
