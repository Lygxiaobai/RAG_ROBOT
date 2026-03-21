package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	OpenAI   OpenAIConfig   `yaml:"openai"`
	Qdrant   QdrantConfig   `yaml:"qdrant"`
	Log      LogConfig      `yaml:"log"`
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
