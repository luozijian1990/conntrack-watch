package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Log *zap.Logger

// Config 日志配置
type Config struct {
	Path       string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

// Init 初始化日志系统
func Init(cfg Config) {
	// 确保日志目录存在
	logDir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// 如果无法创建目录，使用当前目录
		cfg.Path = "./nat.log"
	}

	// 日志切割配置
	lumberJackLogger := &lumberjack.Logger{
		Filename:   cfg.Path,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
	}

	// 编码器配置
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		MessageKey:     "msg",
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
	}

	// 文件输出 (JSON 格式，便于 Filebeat 采集)
	fileCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(lumberJackLogger),
		zapcore.InfoLevel,
	)

	// 控制台输出 (可读格式)
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zapcore.InfoLevel,
	)

	// 合并输出
	core := zapcore.NewTee(fileCore, consoleCore)
	Log = zap.New(core)
}

// LogConnection 记录连接信息
func LogConnection(dstPort uint16, srcIP string, srcPort uint16, dstIP string, snatIP string, snatPort uint16) {
	Log.Info("new_connection",
		zap.String("type", "conntrack"),
		zap.Uint16("dst_port", dstPort),
		zap.String("src_ip", srcIP),
		zap.Uint16("src_port", srcPort),
		zap.String("dst_ip", dstIP),
		zap.String("snat_ip", snatIP),
		zap.Uint16("snat_port", snatPort),
	)
	// 确保日志及时写入文件
	Log.Sync()
}

// Info 记录普通日志信息（带 type=log 字段）
func Info(msg string, fields ...zap.Field) {
	allFields := append([]zap.Field{zap.String("type", "log")}, fields...)
	Log.Info(msg, allFields...)
}

// Error 记录错误日志信息（带 type=log 字段）
func Error(msg string, fields ...zap.Field) {
	allFields := append([]zap.Field{zap.String("type", "log")}, fields...)
	Log.Error(msg, allFields...)
}
