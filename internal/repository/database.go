package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"privacy-relay/internal/config"
	"privacy-relay/internal/model"
	appErr "privacy-relay/pkg/errors"
)

type Database struct {
	db *gorm.DB
}

func NewDatabase(cfg *config.MySQLConfig) (*Database, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time {
			return time.Now().Local()
		},
	})
	if err != nil {
		return nil, appErr.DatabaseError("failed to connect mysql", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, appErr.DatabaseError("failed to get sql.DB", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, appErr.DatabaseError("failed to ping mysql", err)
	}

	database := &Database{db: db}
	if err := database.autoMigrate(); err != nil {
		return nil, err
	}

	return database, nil
}

func (d *Database) autoMigrate() error {
	if err := d.db.AutoMigrate(
		&model.RelayRecord{},
		&model.StateTransition{},
		&model.ReplayRecord{},
	); err != nil {
		return appErr.DatabaseError("failed to auto migrate tables", err)
	}
	return nil
}

func (d *Database) GetDB() *gorm.DB {
	return d.db
}

func (d *Database) WithTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}

func (d *Database) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("get db instance failed: %w", err)
	}
	return sqlDB.Close()
}
