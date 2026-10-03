package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var log *zap.Logger

// Init 初始化全局 zap JSON 日志（stdout 输出，Docker 收割）。
func Init() {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "json"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	l, err := cfg.Build()
	if err != nil {
		panic("init zap logger: " + err.Error())
	}
	log = l
}

// L 返回全局 logger。
func L() *zap.Logger {
	if log == nil {
		Init()
	}
	return log
}

// Sync 刷出日志缓冲。
func Sync() {
	if log != nil {
		_ = log.Sync()
	}
}
