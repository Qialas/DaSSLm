package logcleanup

import (
	"time"

	"dasslm/server/internal/model"

	"gorm.io/gorm"
)

const DefaultRetentionDays = 30

type Result struct {
	Cutoff time.Time        `json:"cutoff"`
	Tables map[string]int64 `json:"tables"`
	Total  int64            `json:"total"`
}

type cleanupTarget struct {
	Name  string
	Model any
}

func Run(db *gorm.DB, retentionDays int) (Result, error) {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := Result{Cutoff: cutoff, Tables: map[string]int64{}}
	targets := []cleanupTarget{
		{Name: "dasslm_operation_logs", Model: &model.OperationLog{}},
		{Name: "dasslm_login_logs", Model: &model.LoginLog{}},
		{Name: "dasslm_system_logs", Model: &model.SystemLog{}},
		{Name: "dasslm_certificate_logs", Model: &model.CertificateLog{}},
		{Name: "dasslm_certificate_deploy_logs", Model: &model.CertificateDeployLog{}},
		{Name: "dasslm_domain_account_logs", Model: &model.DomainAccountLog{}},
		{Name: "dasslm_notification_logs", Model: &model.NotificationLog{}},
		{Name: "dasslm_task_logs", Model: &model.TaskLog{}},
	}
	for _, target := range targets {
		tx := db.Where("created_at < ?", cutoff).Delete(target.Model)
		if tx.Error != nil {
			return result, tx.Error
		}
		result.Tables[target.Name] = tx.RowsAffected
		result.Total += tx.RowsAffected
	}
	return result, nil
}
