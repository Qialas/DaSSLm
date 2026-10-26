package database

import (
	"dasslm/server/internal/config"
	"dasslm/server/internal/model"
	"fmt"
	"net/url"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Connect(cfg config.DatabaseConfig) (*gorm.DB, error) {
	loc := url.QueryEscape(cfg.Loc)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%t&loc=%s",
		cfg.Username,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
		cfg.Charset,
		cfg.ParseTime,
		loc,
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	if err := renameLegacyTables(db); err != nil {
		return err
	}
	if db.Migrator().HasIndex(&model.Domain{}, "idx_qdl_domains_name") {
		_ = db.Migrator().DropIndex(&model.Domain{}, "idx_qdl_domains_name")
	}
	if err := renameLegacyIndexes(db); err != nil {
		return err
	}
	if db.Migrator().HasTable(&model.Domain{}) && db.Migrator().HasColumn(&model.Domain{}, "auto_ssl") {
		if err := db.Migrator().DropColumn(&model.Domain{}, "auto_ssl"); err != nil {
			return err
		}
	}
	if db.Migrator().HasTable(&model.DeployAccount{}) && db.Migrator().HasColumn(&model.DeployAccount{}, "site_name") {
		if err := db.Migrator().DropColumn(&model.DeployAccount{}, "site_name"); err != nil {
			return err
		}
	}
	if err := ensureCertificateCompatibilityColumns(db); err != nil {
		return err
	}
	if err := ensureAutoTaskCompatibilityColumns(db); err != nil {
		return err
	}
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DaSSLm系统数据表'").AutoMigrate(
		migrationModels()...,
	); err != nil {
		return err
	}
	return ensureAutoTaskLegacyTargets(db)
}

func migrationModels() []interface{} {
	return []interface{}{
		&model.User{},
		&model.DomainAccount{},
		&model.SSLAccount{},
		&model.DeployAccount{},
		&model.Domain{},
		&model.DomainRecord{},
		&model.Certificate{},
		&model.AutoTask{},
		&model.AutoTaskTarget{},
		&model.TaskLog{},
		&model.OperationLog{},
		&model.LoginLog{},
		&model.SystemLog{},
		&model.DomainAccountLog{},
		&model.CertificateLog{},
		&model.CertificateDeployLog{},
		&model.SystemSetting{},
		&model.ProxySetting{},
		&model.NotificationSetting{},
		&model.NotificationLog{},
	}
}

const (
	legacyTablePrefix = "qdl_"
	tablePrefix       = "dasslm_"
)

// renameLegacyTables 将旧版 qdl_ 前缀的数据表重命名为 dasslm_ 前缀，保留原有数据。
// 仅在旧表存在且新表不存在时执行，重复启动不会产生影响。
func renameLegacyTables(db *gorm.DB) error {
	migrator := db.Migrator()
	for _, value := range migrationModels() {
		table, err := tableName(db, value)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(table, tablePrefix) {
			continue
		}
		legacy := legacyTablePrefix + strings.TrimPrefix(table, tablePrefix)
		if !migrator.HasTable(legacy) || migrator.HasTable(table) {
			continue
		}
		if err := migrator.RenameTable(legacy, table); err != nil {
			return fmt.Errorf("重命名数据表 %s 为 %s 失败：%w", legacy, table, err)
		}
	}
	return nil
}

// renameLegacyIndexes 将随旧表保留下来的自动命名索引（idx_qdl_表名_字段）改为当前命名，
// 避免 AutoMigrate 按新名称重复建索引。显式命名的索引不随表名变化，无需处理。
func renameLegacyIndexes(db *gorm.DB) error {
	migrator := db.Migrator()
	for _, value := range migrationModels() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(value); err != nil {
			return err
		}
		table := stmt.Schema.Table
		if !strings.HasPrefix(table, tablePrefix) || !migrator.HasTable(table) {
			continue
		}
		legacyTable := legacyTablePrefix + strings.TrimPrefix(table, tablePrefix)
		for name, index := range stmt.Schema.ParseIndexes() {
			if len(index.Fields) != 1 || index.Fields[0].Field == nil {
				continue
			}
			column := index.Fields[0].DBName
			if name != db.NamingStrategy.IndexName(table, column) {
				continue
			}
			legacy := db.NamingStrategy.IndexName(legacyTable, column)
			if legacy == name || !migrator.HasIndex(value, legacy) || migrator.HasIndex(value, name) {
				continue
			}
			if err := migrator.RenameIndex(value, legacy, name); err != nil {
				return fmt.Errorf("重命名索引 %s 为 %s 失败：%w", legacy, name, err)
			}
		}
	}
	return nil
}

func tableName(db *gorm.DB, value interface{}) (string, error) {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(value); err != nil {
		return "", err
	}
	return stmt.Schema.Table, nil
}

func ensureCertificateCompatibilityColumns(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.Certificate{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.Certificate{}, "ssl_account_id") {
		if err := db.Exec("ALTER TABLE `dasslm_certificates` ADD `ssl_account_id` bigint unsigned COMMENT '关联SSL账号ID'").Error; err != nil {
			return err
		}
	}
	if db.Migrator().HasColumn(&model.Certificate{}, "s_slaccount_id") {
		if err := db.Exec("UPDATE dasslm_certificates SET ssl_account_id = s_slaccount_id WHERE (ssl_account_id IS NULL OR ssl_account_id = 0) AND s_slaccount_id IS NOT NULL").Error; err != nil {
			return err
		}
		if err := db.Exec("ALTER TABLE `dasslm_certificates` DROP COLUMN `s_slaccount_id`").Error; err != nil {
			return err
		}
	}
	if db.Migrator().HasColumn(&model.Certificate{}, "account_id") {
		if err := db.Exec("UPDATE dasslm_certificates SET ssl_account_id = account_id WHERE (ssl_account_id IS NULL OR ssl_account_id = 0) AND account_id IS NOT NULL").Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(&model.Certificate{}, "auth_cleanup_status") {
		if err := db.Exec("ALTER TABLE `dasslm_certificates` ADD `auth_cleanup_status` varchar(32) COMMENT '验证DNS记录清理状态：pending待清理 running清理中 done已清理 failed清理失败'").Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureAutoTaskCompatibilityColumns(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.AutoTask{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.AutoTask{}, "deploy_account_id") {
		if err := db.Exec("ALTER TABLE `dasslm_auto_tasks` ADD `deploy_account_id` bigint unsigned COMMENT '关联部署账号ID'").Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(&model.AutoTask{}, "certificate_id") {
		if err := db.Exec("ALTER TABLE `dasslm_auto_tasks` ADD `certificate_id` bigint unsigned COMMENT '关联SSL证书ID'").Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(&model.AutoTask{}, "site_name") {
		if err := db.Exec("ALTER TABLE `dasslm_auto_tasks` ADD `site_name` varchar(255) COMMENT '部署目标站点名称'").Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureAutoTaskLegacyTargets(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.AutoTask{}) || !db.Migrator().HasTable(&model.AutoTaskTarget{}) {
		return nil
	}
	return db.Exec(`
		INSERT INTO dasslm_auto_task_targets
			(created_at, updated_at, task_id, deploy_account_id, site_name, last_run_at, last_run_status, status, remark)
		SELECT
			NOW(), NOW(), t.id, t.deploy_account_id, COALESCE(NULLIF(t.site_name, ''), c.common_name, '未知目标'),
			t.last_run_at, t.last_run_status, COALESCE(NULLIF(t.status, ''), 'enabled'), t.remark
		FROM dasslm_auto_tasks t
		LEFT JOIN dasslm_certificates c ON c.id = t.certificate_id
		LEFT JOIN dasslm_auto_task_targets existing ON existing.task_id = t.id
		WHERE existing.id IS NULL
			AND t.deploy_account_id IS NOT NULL
			AND t.deploy_account_id > 0
			AND t.certificate_id IS NOT NULL
			AND t.certificate_id > 0
	`).Error
}
