package systemtask

import (
	"testing"
	"time"

	"dasslm/server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestListUsesDefaultInterval(t *testing.T) {
	tasks, err := List(testDB(t))
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Key != CertificateApplyQueueTaskKey {
		t.Fatalf("unexpected task key: %s", tasks[0].Key)
	}
	if tasks[0].IntervalSeconds != 30 {
		t.Fatalf("expected default interval 30, got %d", tasks[0].IntervalSeconds)
	}
	if tasks[1].Key != CertificateExpiryMonitorKey {
		t.Fatalf("unexpected expiry monitor key: %s", tasks[1].Key)
	}
	if tasks[1].ScheduleType != "daily" || tasks[1].DailyHour != 7 || tasks[1].DailyMinute != 0 {
		t.Fatalf("unexpected expiry monitor schedule: %#v", tasks[1])
	}
	if tasks[1].ScheduleText != "每天 07:00" {
		t.Fatalf("unexpected expiry monitor schedule text: %s", tasks[1].ScheduleText)
	}
	if tasks[2].Key != LogCleanupTaskKey {
		t.Fatalf("unexpected log cleanup key: %s", tasks[2].Key)
	}
	if tasks[2].ScheduleType != "daily" || tasks[2].DailyHour != 3 || tasks[2].DailyMinute != 0 {
		t.Fatalf("unexpected log cleanup schedule: %#v", tasks[2])
	}
	if tasks[2].RetentionDays != 30 {
		t.Fatalf("expected default retention days 30, got %d", tasks[2].RetentionDays)
	}
}

func TestSaveClampsInterval(t *testing.T) {
	db := testDB(t)
	task, err := Save(db, CertificateApplyQueueTaskKey, UpdateRequest{IntervalSeconds: 1})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	if task.IntervalSeconds != task.MinIntervalSeconds {
		t.Fatalf("expected min interval %d, got %d", task.MinIntervalSeconds, task.IntervalSeconds)
	}
	task, err = Save(db, CertificateApplyQueueTaskKey, UpdateRequest{IntervalSeconds: 99 * 24 * 3600})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	if task.IntervalSeconds != task.MaxIntervalSeconds {
		t.Fatalf("expected max interval %d, got %d", task.MaxIntervalSeconds, task.IntervalSeconds)
	}
}

func TestSaveIntervalUnit(t *testing.T) {
	db := testDB(t)
	task, err := Save(db, CertificateApplyQueueTaskKey, UpdateRequest{
		ScheduleType:  "interval",
		IntervalValue: 5,
		IntervalUnit:  "minute",
	})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	if task.IntervalSeconds != 300 {
		t.Fatalf("expected 300 seconds, got %d", task.IntervalSeconds)
	}
	if task.ScheduleText != "每 5 分" {
		t.Fatalf("unexpected schedule text: %s", task.ScheduleText)
	}
}

func TestSaveDailySchedule(t *testing.T) {
	db := testDB(t)
	task, err := Save(db, CertificateApplyQueueTaskKey, UpdateRequest{
		ScheduleType: "daily",
		DailyHour:    2,
		DailyMinute:  30,
	})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	if task.ScheduleType != "daily" || task.DailyHour != 2 || task.DailyMinute != 30 {
		t.Fatalf("unexpected daily schedule: %#v", task)
	}
	if task.ScheduleText != "每天 02:30" {
		t.Fatalf("unexpected schedule text: %s", task.ScheduleText)
	}
}

func TestNextDelayDailySchedule(t *testing.T) {
	db := testDB(t)
	_, err := Save(db, CertificateApplyQueueTaskKey, UpdateRequest{
		ScheduleType: "daily",
		DailyHour:    2,
		DailyMinute:  30,
	})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	now := time.Date(2026, 6, 13, 1, 0, 0, 0, time.Local)
	if delay := NextDelay(db, CertificateApplyQueueTaskKey, now); delay != 90*time.Minute {
		t.Fatalf("expected 90 minutes, got %v", delay)
	}
	now = time.Date(2026, 6, 13, 3, 0, 0, 0, time.Local)
	if delay := NextDelay(db, CertificateApplyQueueTaskKey, now); delay != 23*time.Hour+30*time.Minute {
		t.Fatalf("expected next day delay, got %v", delay)
	}
}

func TestExpiryMonitorDefaultDelay(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 6, 13, 6, 30, 0, 0, time.Local)
	if delay := NextDelay(db, CertificateExpiryMonitorKey, now); delay != 30*time.Minute {
		t.Fatalf("expected 30 minutes, got %v", delay)
	}
	now = time.Date(2026, 6, 13, 8, 0, 0, 0, time.Local)
	if delay := NextDelay(db, CertificateExpiryMonitorKey, now); delay != 23*time.Hour {
		t.Fatalf("expected 23 hours, got %v", delay)
	}
}

func TestSaveRetentionDays(t *testing.T) {
	db := testDB(t)
	task, err := Save(db, LogCleanupTaskKey, UpdateRequest{
		ScheduleType:  "daily",
		DailyHour:     4,
		DailyMinute:   15,
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	if task.RetentionDays != 7 {
		t.Fatalf("expected retention days 7, got %d", task.RetentionDays)
	}
	if got := RetentionDays(db, LogCleanupTaskKey); got != 7 {
		t.Fatalf("expected persisted retention days 7, got %d", got)
	}
	task, err = Save(db, LogCleanupTaskKey, UpdateRequest{
		ScheduleType:  "daily",
		DailyHour:     4,
		DailyMinute:   15,
		RetentionDays: 99999,
	})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	if task.RetentionDays != task.MaxRetentionDays {
		t.Fatalf("expected max retention days %d, got %d", task.MaxRetentionDays, task.RetentionDays)
	}
}
