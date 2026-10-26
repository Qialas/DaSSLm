package domain

import (
	"testing"

	"dasslm/server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeleteStaleDomainRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.DomainRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewDomainHandler(db)
	rows := []model.DomainRecord{
		{DomainID: 1, ProviderRecordID: "keep", RR: "www", Type: "A", Value: "192.0.2.1"},
		{DomainID: 1, ProviderRecordID: "stale", RR: "old", Type: "A", Value: "192.0.2.2"},
		{DomainID: 2, ProviderRecordID: "other", RR: "api", Type: "A", Value: "192.0.2.3"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed records: %v", err)
	}

	if err := h.deleteStaleDomainRecords(1, []string{"keep"}); err != nil {
		t.Fatalf("delete stale records: %v", err)
	}

	var count int64
	if err := db.Model(&model.DomainRecord{}).Where("domain_id = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("count domain 1: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 record for domain 1, got %d", count)
	}
	var keep model.DomainRecord
	if err := db.Where("domain_id = ? AND provider_record_id = ?", 1, "keep").First(&keep).Error; err != nil {
		t.Fatalf("expected keep record: %v", err)
	}
	if err := db.Model(&model.DomainRecord{}).Where("domain_id = ?", 2).Count(&count).Error; err != nil {
		t.Fatalf("count domain 2: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected unrelated domain record to remain, got %d", count)
	}
}

func TestDeleteStaleDomainRecordsClearsWhenProviderEmpty(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.DomainRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewDomainHandler(db)
	if err := db.Create(&model.DomainRecord{DomainID: 1, ProviderRecordID: "stale", RR: "old", Type: "A", Value: "192.0.2.2"}).Error; err != nil {
		t.Fatalf("seed record: %v", err)
	}

	if err := h.deleteStaleDomainRecords(1, nil); err != nil {
		t.Fatalf("delete stale records: %v", err)
	}
	var count int64
	if err := db.Model(&model.DomainRecord{}).Where("domain_id = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("count domain 1: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty provider list to clear local records, got %d", count)
	}
}
