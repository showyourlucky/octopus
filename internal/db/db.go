package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db/migrate"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func InitDB(dbType, dsn string, debug bool) error {
	var err error
	gormConfig := gorm.Config{Logger: logger.Discard}
	if debug {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	}

	switch dbType {
	case "sqlite":
		db, err = initSQLite(dsn, &gormConfig)
	case "mysql":
		db, err = initMySQL(dsn, &gormConfig)
	case "postgres", "postgresql":
		db, err = initPostgres(dsn, &gormConfig)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	if dbType == "sqlite" {
		// SQLite 同时只能有一个写者；连接过多只会放大 database is locked。
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	} else {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	if err := migrate.BeforeAutoMigrate(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.Channel{},
		&model.ChannelKey{},
		&model.Group{},
		&model.GroupItem{},
		&model.LLMInfo{},
		&model.APIKey{},
		&model.Setting{},
		&model.StatsTotal{},
		&model.StatsDaily{},
		&model.StatsHourly{},
		&model.StatsModel{},
		&model.StatsChannel{},
		&model.StatsAPIKey{},
		&model.RelayLog{},
		&migrate.MigrationRecord{},
	); err != nil {
		return err
	}
	if err := migrate.AfterAutoMigrate(db); err != nil {
		return err
	}
	// Postgres: schema changes during migrations can invalidate cached prepared plans
	// (e.g. "cached plan must not change result type"). Clear them.
	if db.Dialector != nil && db.Dialector.Name() == "postgres" {
		db.Exec("DEALLOCATE ALL")
		db.Exec("DISCARD ALL")
	}
	return nil
}

func initSQLite(path string, config *gorm.Config) (*gorm.DB, error) {
	// glebarez/modernc 只认 _pragma=name(value)，mattn 的 _journal_mode=WAL 会被忽略。
	params := []string{
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=cache_size(10000)",
		"_pragma=foreign_keys(1)",
		"_pragma=mmap_size(268435456)",
	}
	// :memory: 不能切 WAL；auto_vacuum 必须在建表前设置，仅对新库生效。
	if !sqliteMemoryPath(path) {
		params = append([]string{
			"_pragma=auto_vacuum(INCREMENTAL)",
			"_pragma=journal_mode(WAL)",
		}, params...)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return gorm.Open(sqlite.Open(path+sep+strings.Join(params, "&")), config)
}

func sqliteMemoryPath(path string) bool {
	return path == ":memory:" || strings.Contains(path, "mode=memory")
}

func initMySQL(dsn string, config *gorm.Config) (*gorm.DB, error) {
	// DSN 格式: user:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local
	if !strings.Contains(dsn, "?") {
		dsn += "?charset=utf8mb4&parseTime=True&loc=Local"
	}
	return gorm.Open(mysql.Open(dsn), config)
}

func initPostgres(dsn string, config *gorm.Config) (*gorm.DB, error) {
	// DSN 格式: host=localhost user=postgres password=xxx dbname=octopus port=5432 sslmode=disable
	return gorm.Open(postgres.Open(dsn), config)
}

func Close() error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func GetDB() *gorm.DB {
	return db
}

// Vacuum rebuilds the SQLite file to reclaim space after large deletes.
// Non-SQLite dialects are a no-op. VACUUM is not bound to the caller context
// so an HTTP disconnect cannot abort a long rebuild.
//
// WAL 模式下 VACUUM 只会把逻辑 page_count 压下去，主文件要等
// wal_checkpoint(TRUNCATE) 才会 ftruncate；长驻连接不做截断检查点时
// data.db 会一直停在删除前的体积。
func Vacuum() error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "sqlite" {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}
