package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fystack/mpcium/pkg/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db    *gorm.DB
	sqlDB *sql.DB
}

type PostgresConfig struct {
	DSN             string        `json:"dsn"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	MaxOpenConns    int           `json:"max_open_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
}

type Fragment struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Threshold int       `gorm:"column:threshold"`
	Value     []byte    `gorm:"column:value"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Fragment) TableName() string {
	return "fragments"
}

// NewPostgresStore constructs a Postgres-backed Store.
func NewPostgresStore(cfg PostgresConfig) (*PostgresStore, error) {
	if cfg.DSN == "" {
		return nil, errors.New("postgres dsn is required")
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("retrieve sql.DB from gorm: %w", err)
	}

	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	if err := db.AutoMigrate(&Fragment{}); err != nil {
		return nil, fmt.Errorf("auto-migrate kv_entries: %w", err)
	}

	logger.Info("Connected to PostgreSQL successfully!")

	return &PostgresStore{
		db:    db,
		sqlDB: sqlDB,
	}, nil
}

// Put stores or updates a key/value pair.
func (s *PostgresStore) Put(key string, value []byte) error {
	entry := Fragment{
		Key:   key,
		Value: append([]byte(nil), value...),
	}
	return s.db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&entry).Error
}

// Get returns the value stored under key or an error if not found.
func (s *PostgresStore) Get(key string) ([]byte, error) {
	var entry Fragment
	if err := s.db.First(&entry, "key = ?", key).Error; err != nil {
		return nil, err
	}
	return append([]byte(nil), entry.Value...), nil
}

// Delete removes a key/value pair.
func (s *PostgresStore) Delete(key string) error {
	return s.db.Delete(&Fragment{}, "key = ?", key).Error
}

// Backup is a no-op for managed PostgreSQL instances.
func (s *PostgresStore) Backup() error {
	return nil
}

// Close releases the underlying sql.DB.
func (s *PostgresStore) Close() error {
	if s.sqlDB == nil {
		return nil
	}
	return s.sqlDB.Close()
}
