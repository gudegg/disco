package models

import (
	"time"
)

// User 用户模型
type User struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Username  string    `json:"username" gorm:"uniqueIndex;size:50;not null"`
	Password  string    `json:"-" gorm:"size:255;not null"`
	CreatedAt time.Time `json:"created_at"`
}

// Service 服务模型
type Service struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"uniqueIndex;size:50;not null"`
	Description string    `json:"description" gorm:"size:255"`
	CreatedAt   time.Time `json:"created_at"`
}

// ConfigType 配置类型
const (
	ConfigTypeString = "string"
	ConfigTypeJSON   = "json"
)

// Config 配置模型
type Config struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ServiceID uint      `json:"service_id" gorm:"not null;index"`
	Env       string    `json:"env" gorm:"size:20;not null;index"`
	Key       string    `json:"key" gorm:"column:config_key;size:100;not null"`
	Value     string    `json:"value" gorm:"column:config_value;type:text;not null"`
	Type      string    `json:"type" gorm:"size:20;default:'string'"` // string 或 json
	Version   int       `json:"version" gorm:"default:1"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (Config) TableName() string {
	return "configs"
}

// ConfigResponse 配置响应结构
type ConfigResponse struct {
	Service string            `json:"service"`
	Env     string            `json:"env"`
	Version int               `json:"version"`
	Configs map[string]string `json:"configs"`
}

// ConfigChangeMessage 配置变更消息
type ConfigChangeMessage struct {
	Type    string `json:"type"`
	Service string `json:"service"`
	Env     string `json:"env"`
	Version int    `json:"version"`
}

// ServiceToken 服务订阅 Token
type ServiceToken struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ServiceID uint      `json:"service_id" gorm:"not null;index"`
	Env       string    `json:"env" gorm:"size:20;not null"`
	Token     string    `json:"token" gorm:"uniqueIndex;size:64;not null"`
	SecretKey string    `json:"secret_key" gorm:"size:64;not null"` // 用于加密数据
	CreatedAt time.Time `json:"created_at"`
}
