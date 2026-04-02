package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port int `yaml:"port"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
}

// JWTConfig JWT配置
type JWTConfig struct {
	Secret  string `yaml:"secret"`
	Expires int    `yaml:"expires"`
}

// DSN 返回数据库连接字符串
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Name)
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     3306,
			User:     "root",
			Password: "change_me",
			Name:     "config_center",
		},
		JWT: JWTConfig{
			Secret:  "change-me-in-production",
			Expires: 86400,
		},
	}
}

func ResolveConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("CONFIG_FILE")); path != "" {
		return path
	}

	candidates := []string{"config.local.yaml", "config.yaml"}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// LoadConfig 加载配置文件
func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			return nil, err
		}

		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}

	applyEnvOverrides(&cfg)

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	cfg.Server.Port = getEnvInt("CONFIG_CENTER_SERVER_PORT", cfg.Server.Port)

	cfg.Database.Host = getEnvString("CONFIG_CENTER_DB_HOST", cfg.Database.Host)
	cfg.Database.Port = getEnvInt("CONFIG_CENTER_DB_PORT", cfg.Database.Port)
	cfg.Database.User = getEnvString("CONFIG_CENTER_DB_USER", cfg.Database.User)
	cfg.Database.Password = getEnvString("CONFIG_CENTER_DB_PASSWORD", cfg.Database.Password)
	cfg.Database.Name = getEnvString("CONFIG_CENTER_DB_NAME", cfg.Database.Name)

	cfg.JWT.Secret = getEnvString("CONFIG_CENTER_JWT_SECRET", cfg.JWT.Secret)
	cfg.JWT.Expires = getEnvInt("CONFIG_CENTER_JWT_EXPIRES", cfg.JWT.Expires)
}

func getEnvString(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			return fallback
		}
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
