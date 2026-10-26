package database

import (
	"sort"
	"testing"

	"dasslm/server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 旧版 qdl_ 前缀的表和自动命名索引在启动时迁移为 dasslm_ 前缀，数据保持不变
func TestRenameLegacyTablesAndIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	value := &model.DomainRecord{}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(value); err != nil {
		t.Fatal(err)
	}
	var columns []string
	for name, index := range stmt.Schema.ParseIndexes() {
		if len(index.Fields) == 1 && name == db.NamingStrategy.IndexName(stmt.Schema.Table, index.Fields[0].DBName) {
			columns = append(columns, index.Fields[0].DBName)
		}
	}
	if len(columns) == 0 {
		t.Fatal("expected DomainRecord to have an auto-named index")
	}
	sort.Strings(columns)
	column := columns[0]

	for _, sql := range []string{
		"CREATE TABLE qdl_domain_records (id integer primary key, " + column + " text)",
		"CREATE INDEX idx_qdl_domain_records_" + column + " ON qdl_domain_records(" + column + ")",
		"INSERT INTO qdl_domain_records (id, " + column + ") VALUES (7, 'x')",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	for i := 0; i < 2; i++ { // 重复执行应保持幂等
		if err := renameLegacyTables(db); err != nil {
			t.Fatal(err)
		}
		if err := renameLegacyIndexes(db); err != nil {
			t.Fatal(err)
		}
	}

	migrator := db.Migrator()
	if migrator.HasTable("qdl_domain_records") || !migrator.HasTable("dasslm_domain_records") {
		t.Fatal("expected qdl_domain_records to be renamed to dasslm_domain_records")
	}
	if !migrator.HasIndex(value, "idx_dasslm_domain_records_"+column) || migrator.HasIndex(value, "idx_qdl_domain_records_"+column) {
		t.Fatal("expected legacy index to be renamed")
	}
	var count int64
	if err := db.Table("dasslm_domain_records").Where("id = ?", 7).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected legacy row to be kept, count=%d err=%v", count, err)
	}
}
