package config

import (
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

var dbNameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// Getenv อ่านค่า environment variable ถ้าว่างใช้ค่า fallback
func Getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func buildConfig(withDB bool) *mysqldriver.Config {
	cfg := mysqldriver.NewConfig()
	cfg.User = Getenv("DB_USER", "root")
	cfg.Passwd = os.Getenv("DB_PASS")
	cfg.Net = "tcp"
	cfg.Addr = Getenv("DB_HOST", "127.0.0.1") + ":" + Getenv("DB_PORT", "3306")
	if withDB {
		cfg.DBName = Getenv("DB_NAME", "shoestore")
	}
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	if tls := Getenv("DB_TLS", ""); tls != "" {
		cfg.TLSConfig = tls // true | skip-verify | preferred
	}
	return cfg
}

// ensureDatabase พยายามสร้างฐานข้อมูลถ้ายังไม่มี (ถ้าไม่มีสิทธิ์ก็ข้ามไป)
func ensureDatabase() {
	name := Getenv("DB_NAME", "shoestore")
	if !dbNameRe.MatchString(name) {
		return
	}
	db, err := gorm.Open(mysql.Open(buildConfig(false).FormatDSN()), &gorm.Config{})
	if err != nil {
		return
	}
	db.Exec("CREATE DATABASE IF NOT EXISTS `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}

// ConnectDB เชื่อมต่อ MySQL โดยอ่านค่าจาก environment variables
// (DATABASE_DSN หรือ DB_USER / DB_PASS / DB_HOST / DB_PORT / DB_NAME / DB_TLS)
func ConnectDB() {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		ensureDatabase()
		dsn = buildConfig(true).FormatDSN()
	}

	var err error
	for i := 1; i <= 10; i++ {
		DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err == nil {
			break
		}
		log.Printf("เชื่อมต่อฐานข้อมูลไม่สำเร็จ (ครั้งที่ %d/10): %v", i, err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		log.Fatal("Failed to connect to database: ", err)
	}

	if sqlDB, err := DB.DB(); err == nil {
		sqlDB.SetMaxOpenConns(20)
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
	}
	log.Println("Database Connected Successfully!")
}
