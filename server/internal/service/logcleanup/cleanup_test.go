package logcleanup

import (
	"testing"
	"time"

	"dasslm/server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunDeletesOnlyExpiredLogs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.OperationLog{},
		&model.LoginLog{},
		&model.SystemLog{},
		&model.CertificateLog{},
		&model.CertificateDeployLog{},
		&model.DomainAccountLog{},
		&model.NotificationLog{},
		&model.TaskLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	oldTime := time.Now().AddDate(0, 0, -10)
	newTime := time.Now().AddDate(0, 0, -1)
	if err := db.Create(&model.OperationLog{BaseModel: model.BaseModel{CreatedAt: oldTime}, Action: "old"}).Error; err != nil {
		t.Fatalf("create old operation log: %v", err)
	}
	if err := db.Create(&model.OperationLog{BaseModel: model.BaseModel{CreatedAt: newTime}, Action: "new"}).Error; err != nil {
		t.Fatalf("create new operation log: %v", err)
	}
	if err := db.Create(&model.TaskLog{BaseModel: model.BaseModel{CreatedAt: oldTime}, TaskName: "old task", TaskType: "test"}).Error; err != nil {
		t.Fatalf("create old task log: %v", err)
	}

	result, err := Run(db, 7)
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected 2 deleted rows, got %d", result.Total)
	}

	var operationCount int64
	if err := db.Model(&model.OperationLog{}).Count(&operationCount).Error; err != nil {
		t.Fatalf("count operation logs: %v", err)
	}
	if operationCount != 1 {
		t.Fatalf("expected 1 operation log after cleanup, got %d", operationCount)
	}
	var taskCount int64
	if err := db.Model(&model.TaskLog{}).Count(&taskCount).Error; err != nil {
		t.Fatalf("count task logs: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected 0 task logs after cleanup, got %d", taskCount)
	}
}
