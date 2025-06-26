package database

import (
	"NetherLink-server/config"
	"gorm.io/gorm"
	"sync"
)

var (
	db     *gorm.DB
	dbOnce sync.Once
)

// GetDB 获取数据库连接（单例模式）
func GetDB() (*gorm.DB, error) {
	var err error
	dbOnce.Do(func() {
		cfg := config.GlobalConfig.Database
		db, err = NewDB(&Config{
			Driver:       cfg.Driver,
			Host:        cfg.Host,
			Port:        cfg.Port,
			Username:    cfg.Username,
			Password:    cfg.Password,
			DBName:      cfg.DBName,
			Charset:     cfg.Charset,
			ParseTime:   cfg.ParseTime,
			Loc:         cfg.Loc,
			MaxIdleConns: cfg.MaxIdleConns,
			MaxOpenConns: cfg.MaxOpenConns,
		})
	})
	return db, err
} 