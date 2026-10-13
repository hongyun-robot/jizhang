package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"jizhang/internal/config"
	"jizhang/internal/liability"
)

func main() {
	if _, err := os.Stat(".env"); err == nil {
		_ = godotenv.Load(".env")
	} else {
		_ = godotenv.Load("../.env")
	}
	db, err := gorm.Open(mysql.Open(config.Load().DBDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法连接数据库，请核对本机配置")
		os.Exit(1)
	}
	pool, err := db.DB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法读取数据库连接")
		os.Exit(1)
	}
	defer pool.Close()
	audit, err := liability.AuditRepayments(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(audit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(audit.Funds) > 0 {
		os.Exit(2)
	}
}
