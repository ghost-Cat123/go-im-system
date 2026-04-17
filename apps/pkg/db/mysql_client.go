package db

import (
	"go-im-system/apps/pkg/config"
	"go-im-system/apps/pkg/logger"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"sync"
)

// DB 全局变量私有化
var (
	db *gorm.DB
	// 保证只注册一次
	once    sync.Once
	initErr error
)

func InitMySQL(sqlConfig config.MySQLConfig) error {
	once.Do(func() {
		db, initErr = gorm.Open(mysql.Open(sqlConfig.DSN), &gorm.Config{})
		sqlDB, _ := db.DB()
		sqlDB.SetMaxIdleConns(sqlConfig.MaxIdleConns)
		logger.Log.Info("数据库单例初始化成功！")
	})
	return initErr
}

func GetDB() *gorm.DB {
	if db == nil {
		panic("db not initialized")
	}
	return db
}
