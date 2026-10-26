package systemtask

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dasslm/server/internal/model"

	"gorm.io/gorm"
)

const (
	CertificateApplyQueueTaskKey = "certificate_apply_queue"
	CertificateExpiryMonitorKey  = "certificate_expiry_monitor"
	LogCleanupTaskKey            = "log_cleanup"
)

type TaskSetting struct {
	Key                    string `json:"key"`
	Name                   string `json:"name"`
	Description            string `json:"description"`
	Module                 string `json:"module"`
	Status                 string `json:"status"`
	ScheduleType           string `json:"scheduleType"`
	IntervalValue          int    `json:"intervalValue"`
	IntervalUnit           string `json:"intervalUnit"`
	IntervalSeconds        int    `json:"intervalSeconds"`
	DefaultIntervalSeconds int    `json:"defaultIntervalSeconds"`
	MinIntervalSeconds     int    `json:"minIntervalSeconds"`
	MaxIntervalSeconds     int    `json:"maxIntervalSeconds"`
	DailyHour              int    `json:"dailyHour"`
	DailyMinute            int    `json:"dailyMinute"`
	RetentionDays          int    `json:"retentionDays"`
	DefaultRetentionDays   int    `json:"defaultRetentionDays"`
	MinRetentionDays       int    `json:"minRetentionDays"`
	MaxRetentionDays       int    `json:"maxRetentionDays"`
	ScheduleText           string `json:"scheduleText"`
}

type UpdateRequest struct {
	ScheduleType    string `json:"scheduleType"`
	IntervalValue   int    `json:"intervalValue"`
	IntervalUnit    string `json:"intervalUnit"`
	IntervalSeconds int    `json:"intervalSeconds"`
	DailyHour       int    `json:"dailyHour"`
	DailyMinute     int    `json:"dailyMinute"`
	RetentionDays   int    `json:"retentionDays"`
}

type storedSchedule struct {
	ScheduleType    string `json:"scheduleType"`
	IntervalValue   int    `json:"intervalValue"`
	IntervalUnit    string `json:"intervalUnit"`
	IntervalSeconds int    `json:"intervalSeconds"`
	DailyHour       int    `json:"dailyHour"`
	DailyMinute     int    `json:"dailyMinute"`
	RetentionDays   int    `json:"retentionDays"`
}

func defaultTasks() []TaskSetting {
	return []TaskSetting{
		{
			Key:                    CertificateApplyQueueTaskKey,
			Name:                   "证书申请队列监控",
			Description:            "扫描待申请证书任务，自动创建证书申请单并进入验证签发流程",
			Module:                 "certificate",
			Status:                 "enabled",
			ScheduleType:           "interval",
			IntervalValue:          30,
			IntervalUnit:           "second",
			IntervalSeconds:        30,
			DefaultIntervalSeconds: 30,
			MinIntervalSeconds:     10,
			MaxIntervalSeconds:     30 * 24 * 3600,
		},
		{
			Key:                    CertificateExpiryMonitorKey,
			Name:                   "证书到期监控",
			Description:            "扫描即将到期的已签发证书，自动加入续期申请队列",
			Module:                 "certificate",
			Status:                 "enabled",
			ScheduleType:           "daily",
			DailyHour:              7,
			DailyMinute:            0,
			DefaultIntervalSeconds: 24 * 3600,
			MinIntervalSeconds:     10,
			MaxIntervalSeconds:     30 * 24 * 3600,
		},
		{
			Key:                    LogCleanupTaskKey,
			Name:                   "日志清理",
			Description:            "按保留天数清理系统运行、操作、登录、证书、部署、通知和任务日志",
			Module:                 "system",
			Status:                 "enabled",
			ScheduleType:           "daily",
			DailyHour:              3,
			DailyMinute:            0,
			DefaultIntervalSeconds: 24 * 3600,
			MinIntervalSeconds:     10,
			MaxIntervalSeconds:     30 * 24 * 3600,
			RetentionDays:          30,
			DefaultRetentionDays:   30,
			MinRetentionDays:       1,
			MaxRetentionDays:       3650,
		},
	}
}

func List(db *gorm.DB) ([]TaskSetting, error) {
	tasks := defaultTasks()
	for i := range tasks {
		schedule, err := readSchedule(db, tasks[i])
		if err != nil {
			return nil, err
		}
		applySchedule(&tasks[i], schedule)
	}
	return tasks, nil
}

func Save(db *gorm.DB, key string, req UpdateRequest) (TaskSetting, error) {
	task, ok := findDefaultTask(key)
	if !ok {
		return TaskSetting{}, fmt.Errorf("系统任务不存在")
	}
	schedule, err := normalizeRequest(req, task)
	if err != nil {
		return TaskSetting{}, err
	}
	raw, err := json.Marshal(schedule)
	if err != nil {
		return TaskSetting{}, err
	}
	setting := model.SystemSetting{
		SettingKey:   intervalSettingKey(task.Key),
		SettingValue: string(raw),
		ValueType:    "json",
		Description:  task.Name + "调度设置",
	}
	if err := db.Where("setting_key = ?", setting.SettingKey).Assign(setting).FirstOrCreate(&setting).Error; err != nil {
		return TaskSetting{}, err
	}
	applySchedule(&task, schedule)
	return task, nil
}

func Interval(db *gorm.DB, key string) time.Duration {
	return NextDelay(db, key, time.Now())
}

func RetentionDays(db *gorm.DB, key string) int {
	task, ok := findDefaultTask(key)
	if !ok {
		return 30
	}
	schedule, err := readSchedule(db, task)
	if err != nil {
		return defaultRetentionDays(task)
	}
	return normalizeRetentionDays(schedule.RetentionDays, task)
}

func NextDelay(db *gorm.DB, key string, now time.Time) time.Duration {
	task, ok := findDefaultTask(key)
	if !ok {
		return 30 * time.Second
	}
	schedule, err := readSchedule(db, task)
	if err != nil {
		return time.Duration(task.DefaultIntervalSeconds) * time.Second
	}
	if schedule.ScheduleType == "daily" {
		next := time.Date(now.Year(), now.Month(), now.Day(), schedule.DailyHour, schedule.DailyMinute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		return next.Sub(now)
	}
	return time.Duration(schedule.IntervalSeconds) * time.Second
}

func findDefaultTask(key string) (TaskSetting, bool) {
	for _, task := range defaultTasks() {
		if task.Key == key {
			return task, true
		}
	}
	return TaskSetting{}, false
}

func readSchedule(db *gorm.DB, task TaskSetting) (storedSchedule, error) {
	var setting model.SystemSetting
	err := db.Where("setting_key = ?", intervalSettingKey(task.Key)).First(&setting).Error
	if err == gorm.ErrRecordNotFound {
		return defaultSchedule(task), nil
	}
	if err != nil {
		return defaultSchedule(task), err
	}
	value := strings.TrimSpace(setting.SettingValue)
	var schedule storedSchedule
	if strings.HasPrefix(value, "{") {
		if err := json.Unmarshal([]byte(value), &schedule); err != nil {
			return defaultSchedule(task), nil
		}
		return normalizeStoredSchedule(schedule, task), nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return defaultSchedule(task), nil
	}
	return normalizeStoredSchedule(storedSchedule{ScheduleType: "interval", IntervalSeconds: seconds}, task), nil
}

func normalizeRequest(req UpdateRequest, task TaskSetting) (storedSchedule, error) {
	retentionDays := normalizeRetentionDays(req.RetentionDays, task)
	scheduleType := strings.TrimSpace(req.ScheduleType)
	if scheduleType == "" {
		scheduleType = "interval"
	}
	if scheduleType == "daily" {
		if req.DailyHour < 0 || req.DailyHour > 23 || req.DailyMinute < 0 || req.DailyMinute > 59 {
			return storedSchedule{}, fmt.Errorf("每天执行时间不正确")
		}
		return storedSchedule{
			ScheduleType:  "daily",
			DailyHour:     req.DailyHour,
			DailyMinute:   req.DailyMinute,
			RetentionDays: retentionDays,
		}, nil
	}
	unit := strings.TrimSpace(req.IntervalUnit)
	if unit == "" {
		unit = "second"
	}
	value := req.IntervalValue
	if value <= 0 && req.IntervalSeconds > 0 {
		value, unit = secondsToValueUnit(req.IntervalSeconds)
	}
	if value <= 0 {
		value = task.IntervalValue
	}
	seconds := intervalToSeconds(value, unit)
	seconds = clampInterval(seconds, task)
	value, unit = secondsToValueUnit(seconds)
	return storedSchedule{
		ScheduleType:    "interval",
		IntervalValue:   value,
		IntervalUnit:    unit,
		IntervalSeconds: seconds,
		RetentionDays:   retentionDays,
	}, nil
}

func normalizeStoredSchedule(schedule storedSchedule, task TaskSetting) storedSchedule {
	retentionDays := normalizeRetentionDays(schedule.RetentionDays, task)
	if schedule.ScheduleType == "daily" {
		if schedule.DailyHour < 0 || schedule.DailyHour > 23 {
			schedule.DailyHour = 0
		}
		if schedule.DailyMinute < 0 || schedule.DailyMinute > 59 {
			schedule.DailyMinute = 0
		}
		schedule.IntervalSeconds = 0
		schedule.RetentionDays = retentionDays
		return schedule
	}
	seconds := schedule.IntervalSeconds
	if seconds <= 0 {
		seconds = intervalToSeconds(schedule.IntervalValue, schedule.IntervalUnit)
	}
	seconds = clampInterval(seconds, task)
	value, unit := secondsToValueUnit(seconds)
	return storedSchedule{
		ScheduleType:    "interval",
		IntervalValue:   value,
		IntervalUnit:    unit,
		IntervalSeconds: seconds,
		RetentionDays:   retentionDays,
	}
}

func defaultSchedule(task TaskSetting) storedSchedule {
	return storedSchedule{
		ScheduleType:    task.ScheduleType,
		IntervalValue:   task.IntervalValue,
		IntervalUnit:    task.IntervalUnit,
		IntervalSeconds: task.DefaultIntervalSeconds,
		DailyHour:       task.DailyHour,
		DailyMinute:     task.DailyMinute,
		RetentionDays:   defaultRetentionDays(task),
	}
}

func applySchedule(task *TaskSetting, schedule storedSchedule) {
	task.ScheduleType = schedule.ScheduleType
	task.IntervalValue = schedule.IntervalValue
	task.IntervalUnit = schedule.IntervalUnit
	task.IntervalSeconds = schedule.IntervalSeconds
	task.DailyHour = schedule.DailyHour
	task.DailyMinute = schedule.DailyMinute
	task.RetentionDays = normalizeRetentionDays(schedule.RetentionDays, *task)
	task.ScheduleText = scheduleText(schedule)
}

func defaultRetentionDays(task TaskSetting) int {
	if task.DefaultRetentionDays > 0 {
		return task.DefaultRetentionDays
	}
	return task.RetentionDays
}

func normalizeRetentionDays(value int, task TaskSetting) int {
	defaultValue := defaultRetentionDays(task)
	if defaultValue <= 0 {
		return 0
	}
	if value <= 0 {
		value = defaultValue
	}
	if task.MinRetentionDays > 0 && value < task.MinRetentionDays {
		return task.MinRetentionDays
	}
	if task.MaxRetentionDays > 0 && value > task.MaxRetentionDays {
		return task.MaxRetentionDays
	}
	return value
}

func clampInterval(value int, task TaskSetting) int {
	if value < task.MinIntervalSeconds {
		return task.MinIntervalSeconds
	}
	if value > task.MaxIntervalSeconds {
		return task.MaxIntervalSeconds
	}
	return value
}

func intervalToSeconds(value int, unit string) int {
	switch unit {
	case "day":
		return value * 24 * 3600
	case "hour":
		return value * 3600
	case "minute":
		return value * 60
	default:
		return value
	}
}

func secondsToValueUnit(seconds int) (int, string) {
	if seconds%(24*3600) == 0 {
		return seconds / (24 * 3600), "day"
	}
	if seconds%3600 == 0 {
		return seconds / 3600, "hour"
	}
	if seconds%60 == 0 {
		return seconds / 60, "minute"
	}
	return seconds, "second"
}

func scheduleText(schedule storedSchedule) string {
	if schedule.ScheduleType == "daily" {
		return fmt.Sprintf("每天 %02d:%02d", schedule.DailyHour, schedule.DailyMinute)
	}
	labels := map[string]string{"second": "秒", "minute": "分", "hour": "小时", "day": "天"}
	label := labels[schedule.IntervalUnit]
	if label == "" {
		label = "秒"
	}
	return fmt.Sprintf("每 %d %s", schedule.IntervalValue, label)
}

func intervalSettingKey(taskKey string) string {
	return "system_task." + taskKey + ".interval_seconds"
}
