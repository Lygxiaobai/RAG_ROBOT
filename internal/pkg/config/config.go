package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server         ServerConfig         `yaml:"server"`
	Database       DatabaseConfig       `yaml:"database"`
	Redis          RedisConfig          `yaml:"redis"`
	OpenAI         OpenAIConfig         `yaml:"openai"` // 现在是千问
	Qdrant         QdrantConfig         `yaml:"qdrant"`
	Log            LogConfig            `yaml:"log"`
	Metrics        MetricsConfig        `yaml:"metrics"`
	Tracing        TracingConfig        `yaml:"tracing"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
}

type ServerConfig struct {
	Port string `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Host                   string `yaml:"host"`
	Port                   int    `yaml:"port"`
	Username               string `yaml:"username"`
	Password               string `yaml:"password"`
	DBName                 string `yaml:"dbname"`
	Charset                string `yaml:"charset"`
	MaxOpenConns           int    `yaml:"maxOpenConns"`
	MaxIdleConns           int    `yaml:"maxIdleConns"`
	ConnMaxLifetimeMinutes int    `yaml:"connMaxLifetimeMinutes"`
	ConnMaxIdleTimeMinutes int    `yaml:"connMaxIdleTimeMinutes"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	Enabled  bool   `yaml:"enabled"`
}

type OpenAIConfig struct {
	APIKey         string `yaml:"api_key"`
	BaseURL        string `yaml:"base_url"`
	Model          string `yaml:"model"`
	EmbeddingModel string `yaml:"embedding_model"`
}

type QdrantConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type LogConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	OutputPath string `yaml:"outputPath"`
	MaxSize    int    `yaml:"maxSize"`
	MaxBackups int    `yaml:"maxBackups"`
	MaxAge     int    `yaml:"maxAge"`
}

// MetricsConfig Prometheus 监控配置
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"` // Prometheus 抓取路径，默认 /metrics
}

// TracingConfig OpenTelemetry 链路追踪配置
type TracingConfig struct {
	Enabled     bool   `yaml:"enabled"`
	ServiceName string `yaml:"service_name"`
	// Exporter 支持 stdout（开发调试）和 jaeger（生产上报）
	Exporter string `yaml:"exporter"`
}

// CircuitBreakerConfig 各依赖服务的熔断器参数配置
type CircuitBreakerConfig struct {
	OpenAI CBItemConfig `yaml:"openai"`
	Qdrant CBItemConfig `yaml:"qdrant"`
}

// CBItemConfig 单个熔断器参数
type CBItemConfig struct {
	// WindowSize 滑动窗口时长，例如 "60s"
	WindowSize string `yaml:"window_size"`
	// MinRequests 触发熔断判断的最小请求数
	MinRequests uint32 `yaml:"min_requests"`
	// FailureRate 失败率阈值（0~1），超过则打开熔断器
	FailureRate float64 `yaml:"failure_rate"`
	// ResetTimeout 熔断器 Open→HalfOpen 的等待时长，例如 "30s"
	ResetTimeout string `yaml:"reset_timeout"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file failed: %w", err)
	}

	var config Config
	if err = yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config file failed: %w", err)
	}

	return &config, nil
}

// 获取配置文件路径
func GetConfigPath() string {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "dev"
	}

	candidates := []string{
		fmt.Sprintf("configs/config.%s.local.yaml", env),
		"configs/config.local.yaml",
		fmt.Sprintf("configs/config.%s.example.yaml", env),
		"configs/config.example.yaml",
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return candidates[0]
}
