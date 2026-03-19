package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"os"
	"path/filepath"
)

var Logger *zap.Logger

// Config日志配置
type Config struct {
	Level      string //debug info warn
	Format     string //日志输出格式 json text
	OutputPath string //日志为文件输出路径
	MaxSize    int    //单个日志文件最大大小MB
	MaxBackups int    //保留旧文件最大数量
	MaxAge     int    //保留旧文件最大天数
}

// 初始化日志
func Init(cfg Config) error {
	//1.设置日志级别
	level := getLogLevel(cfg.Level)
	//2.设置编码器
	encoder := getEncoder(cfg.Format)
	//3.设置输出位置
	syncer, err := getWriteSyncer(cfg)
	if err != nil {
		return err
	}
	//4.创建core
	core := zapcore.NewCore(encoder, syncer, level)
	//5.创建logger
	Logger = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	return nil
}

// 获取日志级别
func getLogLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// 设置编码器  让日志按zap的格式化输出
func getEncoder(format string) zapcore.Encoder {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	if format == "json" {
		return zapcore.NewJSONEncoder(encoderConfig)
	}
	return zapcore.NewConsoleEncoder(encoderConfig)
}

// 获取日志的输出位置 ,并配置日志轮转
func getWriteSyncer(cfg Config) (zapcore.WriteSyncer, error) {
	// 如果没有配置文件路径，只输出到控制台
	if cfg.OutputPath == "" {
		return zapcore.AddSync(os.Stdout), nil
	}

	// 确保日志目录存在
	dir := filepath.Dir(cfg.OutputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// 配置日志轮转
	lumberJackLogger := &lumberjack.Logger{
		Filename:   cfg.OutputPath,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   true, //配置压缩
	}

	// 同时输出到控制台和文件
	return zapcore.NewMultiWriteSyncer(
		zapcore.AddSync(os.Stdout),
		zapcore.AddSync(lumberJackLogger),
	), nil
}

// 全局日志函数 更加简洁
func Info(msg string, fields ...zap.Field) {
	Logger.Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	Logger.Warn(msg, fields...)
}
func Error(msg string, fields ...zap.Field) {
	Logger.Error(msg, fields...)
}
func Debug(msg string, fields ...zap.Field) {
	Logger.Debug(msg, fields...)
}

// Sync刷新缓冲区 确保日志写到控制台和日志文件
func Sync() error {
	return Logger.Sync()
}
