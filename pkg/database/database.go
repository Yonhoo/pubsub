// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package database

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config 数据库配置
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	Charset  string
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		Host:     "localhost",
		Port:     3306,
		User:     "root",
		Password: "password",
		DBName:   "pubsub",
		Charset:  "utf8mb4",
	}
}

// NewDatabase 创建数据库连接
func NewDatabase(config *Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		config.DBName,
		config.Charset,
	)

	// 配置详细的日志记录器，记录所有 SQL 语句和错误
	customLogger := logger.New(
		log.New(log.Writer(), "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: false,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: customLogger,
		NowFunc: func() time.Time {
			return time.Now().Local() // 使用本地时区，与数据库时区一致
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// AutoMigrate 自动迁移表结构
func AutoMigrate(db *gorm.DB) error {
	log.Println("📦 [Database] 开始自动迁移...")

	err := db.AutoMigrate(
		&Room{},
		&RoomUser{},
		&ConnectNode{},
	)
	if err != nil {
		return fmt.Errorf("auto migrate failed: %w", err)
	}

	log.Println("✅ [Database] 表结构迁移完成")
	return nil
}

// CreateDatabaseIfNotExists 如果数据库不存在则创建
func CreateDatabaseIfNotExists(config *Config) error {
	// 连接到 MySQL（不指定数据库）
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=%s&parseTime=True&loc=Local",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		config.Charset,
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL: %w", err)
	}

	// 创建数据库
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET %s COLLATE %s_general_ci",
		config.DBName, config.Charset, config.Charset)

	if err := db.Exec(createSQL).Error; err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	log.Printf("✅ [Database] 数据库 '%s' 已就绪\n", config.DBName)
	return nil
}

