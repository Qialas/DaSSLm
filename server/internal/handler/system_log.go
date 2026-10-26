package handler

import (
	"encoding/json"

	"dasslm/server/internal/model"

	"gorm.io/gorm"
)

func logSystem(db *gorm.DB, source, module, level, event, status, message string, detail any) {
	LogSystem(db, source, module, level, event, status, message, detail)
}

func LogSystem(db *gorm.DB, source, module, level, event, status, message string, detail any) {
	rawDetail := ""
	if detail != nil {
		if raw, err := json.Marshal(detail); err == nil {
			rawDetail = string(raw)
		}
	}
	_ = db.Create(&model.SystemLog{
		Source:  source,
		Module:  module,
		Level:   level,
		Event:   event,
		Status:  status,
		Message: message,
		Detail:  rawDetail,
	}).Error
}
