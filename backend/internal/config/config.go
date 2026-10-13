package config

import (
	"os"
	"strconv"
)

// Config 从环境变量加载全部运行配置（12-factor，.env 不进 git）。
type Config struct {
	Port            string
	DBHost          string
	DBPort          string
	DBUser          string
	DBPassword      string
	DBName          string
	RedisAddr       string
	RedisPassword   string
	JWTSecret       string
	AdminUsername   string
	AdminPassword   string
	CORSOrigins     []string
	AccessTTLMin    int
	RefreshTTLHours int
	CookieSecure    bool
	// CookiePath refresh cookie 的 Path。站点挂子路径（如 /jizhang）时必须同步前缀，
	// 否则浏览器在 /jizhang/api/v1/auth/refresh 上不会携带 Path=/api/v1/auth 的 cookie。
	CookiePath string
	// BackupStatusFile 备份状态文件路径（docker/backup.sh 写入，health 暴露最近备份时间）。
	BackupStatusFile string
	FuyaoAPIKey      string `json:"-"`
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// Load 读取环境变量，缺失项使用开发默认值。
func Load() *Config {
	return &Config{
		Port:            getEnv("SERVER_PORT", "8080"),
		DBHost:          getEnv("DB_HOST", "127.0.0.1"),
		DBPort:          getEnv("DB_PORT", "3306"),
		DBUser:          getEnv("DB_USER", "root"),
		DBPassword:      getEnv("DB_PASSWORD", "devpass"),
		DBName:          getEnv("DB_NAME", "jizhang"),
		RedisAddr:       getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:   getEnv("REDIS_PASSWORD", ""),
		JWTSecret:       getEnv("JWT_SECRET", "dev-secret-change-me"),
		AdminUsername:   getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:   getEnv("ADMIN_PASSWORD", "admin123"),
		CORSOrigins:     []string{getEnv("CORS_ORIGINS", "http://localhost:5600")},
		AccessTTLMin:    getEnvInt("ACCESS_TTL_MIN", 30),
		RefreshTTLHours: getEnvInt("REFRESH_TTL_HOURS", 24*30),
		CookieSecure:    getEnvBool("COOKIE_SECURE", false),
		CookiePath:      getEnv("COOKIE_PATH", "/api/v1/auth"),

		BackupStatusFile: getEnv("BACKUP_STATUS_FILE", ""),
		FuyaoAPIKey:      getEnv("FUYAO_API_KEY", ""),
	}
}

// DBDSN 返回 MySQL DSN。
func (c *Config) DBDSN() string {
	return c.DBUser + ":" + c.DBPassword + "@tcp(" + c.DBHost + ":" + c.DBPort + ")/" +
		c.DBName + "?charset=utf8mb4&parseTime=True&loc=Local"
}
