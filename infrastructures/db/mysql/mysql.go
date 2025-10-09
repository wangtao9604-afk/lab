package mysql

import (
	"time"

	"qywx/infrastructures/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Open() (*gorm.DB, error) {
	cfg := config.GetInstance()

	db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN), &gorm.Config{
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.MySQL.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MySQL.MaxIdleConns)
	sqlDB.SetConnMaxIdleTime(time.Duration(cfg.MySQL.ConnMaxIdleTime) * time.Second)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.MySQL.ConnMaxLifetime) * time.Second)

	return db, nil
}
