package handlers

import (
	"config-center/models"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SSEManager 接口定义
type SSEManager interface {
	BroadcastConfigChange(service, env string, version int)
}

// CreateConfigRequest 创建配置请求
type CreateConfigRequest struct {
	ServiceID uint   `json:"service_id" binding:"required"`
	Env       string `json:"env" binding:"required,min=1,max=20"`
	Key       string `json:"key" binding:"required,min=1,max=100"`
	Value     string `json:"value" binding:"required"`
	Type      string `json:"type" binding:"required,oneof=string json"`
}

// UpdateConfigRequest 更新配置请求
type UpdateConfigRequest struct {
	Value string `json:"value" binding:"required"`
	Type  string `json:"type" binding:"omitempty,oneof=string json"`
}

// validateJSON 验证 JSON 格式
func validateJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

// Encryption 加密工具
type Encryption struct {
	secretKey string
}

// NewEncryption 创建加密工具
func NewEncryption(secretKey string) *Encryption {
	return &Encryption{secretKey: secretKey}
}

// Encrypt 加密数据
func (e *Encryption) Encrypt(plaintext string) (string, error) {
	key := sha256.Sum256([]byte(e.secretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密数据
func (e *Encryption) Decrypt(ciphertext string) (string, error) {
	key := sha256.Sum256([]byte(e.secretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// ConfigHandler 配置处理器
type ConfigHandler struct {
	db         *gorm.DB
	sseManager SSEManager
}

// NewConfigHandler 创建配置处理器
func NewConfigHandler(db *gorm.DB, sse SSEManager) *ConfigHandler {
	return &ConfigHandler{
		db:         db,
		sseManager: sse,
	}
}

// List 获取配置列表
func (h *ConfigHandler) List(c *gin.Context) {
	serviceName := c.Param("service")
	env := c.Param("env")

	// 查找服务
	var service models.Service
	if err := h.db.Where("name = ?", serviceName).First(&service).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	// 获取该服务该环境下的所有配置
	var configs []models.Config
	if err := h.db.Where("service_id = ? AND env = ?", service.ID, env).Order("id DESC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch configs"})
		return
	}

	c.JSON(http.StatusOK, configs)
}

// Create 创建配置
func (h *ConfigHandler) Create(c *gin.Context) {
	var req CreateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 如果是 JSON 类型，验证 JSON 格式
	if req.Type == models.ConfigTypeJSON && !validateJSON(req.Value) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON format"})
		return
	}

	// 检查服务是否存在
	var service models.Service
	if err := h.db.First(&service, req.ServiceID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service not found"})
		return
	}

	config := models.Config{
		ServiceID: req.ServiceID,
		Env:       req.Env,
		Key:       req.Key,
		Value:     req.Value,
		Type:      req.Type,
		Version:   1,
	}

	if err := h.db.Create(&config).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config key already exists in this service and env"})
		return
	}

	// 广播配置变更
	h.sseManager.BroadcastConfigChange(service.Name, req.Env, config.Version)

	c.JSON(http.StatusOK, config)
}

// Update 更新配置
func (h *ConfigHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 查找配置
	var config models.Config
	if err := h.db.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "config not found"})
		return
	}

	// 如果是 JSON 类型或要改为 JSON 类型，验证 JSON 格式
	configType := config.Type
	if req.Type != "" {
		configType = req.Type
	}
	if configType == models.ConfigTypeJSON && !validateJSON(req.Value) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON format"})
		return
	}

	// 查找服务
	var service models.Service
	if err := h.db.First(&service, config.ServiceID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "service not found"})
		return
	}

	// 更新配置
	config.Value = req.Value
	if req.Type != "" {
		config.Type = req.Type
	}
	config.Version++

	if err := h.db.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update config"})
		return
	}

	// 广播配置变更
	h.sseManager.BroadcastConfigChange(service.Name, config.Env, config.Version)

	c.JSON(http.StatusOK, config)
}

// Delete 删除配置
func (h *ConfigHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	// 查找配置
	var config models.Config
	if err := h.db.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "config not found"})
		return
	}

	// 查找服务
	var service models.Service
	if err := h.db.First(&service, config.ServiceID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "service not found"})
		return
	}

	// 删除配置
	if err := h.db.Delete(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete config"})
		return
	}

	// 广播配置变更
	h.sseManager.BroadcastConfigChange(service.Name, config.Env, config.Version+1)

	c.JSON(http.StatusOK, gin.H{"message": "config deleted"})
}

// GetEnvs 获取环境列表
func (h *ConfigHandler) GetEnvs(c *gin.Context) {
	serviceName := c.Param("service")

	// 查找服务
	var service models.Service
	if err := h.db.Where("name = ?", serviceName).First(&service).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	// 获取该服务的所有环境
	var envs []string
	if err := h.db.Model(&models.Config{}).
		Where("service_id = ?", service.ID).
		Distinct("env").
		Pluck("env", &envs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch envs"})
		return
	}

	c.JSON(http.StatusOK, envs)
}

// GetServiceConfig 获取服务配置（供客户端使用）
func (h *ConfigHandler) GetServiceConfig(c *gin.Context) {
	serviceName := c.Param("service")
	env := c.Param("env")

	// 查找服务
	var service models.Service
	if err := h.db.Where("name = ?", serviceName).First(&service).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	// 获取该服务该环境下的所有配置
	var configs []models.Config
	if err := h.db.Where("service_id = ? AND env = ?", service.ID, env).Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch configs"})
		return
	}

	// 转换为 map
	configMap := make(map[string]string)
	maxVersion := 0
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
		if cfg.Version > maxVersion {
			maxVersion = cfg.Version
		}
	}

	response := models.ConfigResponse{
		Service: service.Name,
		Env:     env,
		Version: maxVersion,
		Configs: configMap,
	}

	c.JSON(http.StatusOK, response)
}

// SSEHandler SSE 处理器
type SSEHandler struct {
	sseManager SSEManager
}

// NewSSEHandler 创建 SSE 处理器
func NewSSEHandler(sse SSEManager) *SSEHandler {
	return &SSEHandler{sseManager: sse}
}

// HandleSSE 处理 SSE 连接（需要 Token 鉴权）
func (h *SSEHandler) HandleSSE(c *gin.Context) {
	service := c.Query("service")
	env := c.Query("env")
	token := c.Query("token")

	if service == "" || env == "" || token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service, env and token are required"})
		return
	}

	// 验证 Token
	tokenHandler := GetGlobalTokenHandler()
	serviceToken, err := tokenHandler.VerifyToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	// 验证服务名和环境是否匹配
	var svc models.Service
	if err := tokenHandler.db.Where("id = ? AND name = ?", serviceToken.ServiceID, service).First(&svc).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "token does not match service"})
		return
	}

	if serviceToken.Env != env {
		c.JSON(http.StatusForbidden, gin.H{"error": "token does not match env"})
		return
	}

	// 获取全局 SSE 管理器
	sseMgr := GetGlobalSSEManager()

	// 订阅配置变更
	ch := sseMgr.Subscribe(service, env)
	defer sseMgr.Unsubscribe(service, env, ch)

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 创建加密工具
	encryption := NewEncryption(serviceToken.Token)

	// 发送初始连接成功消息（加密）
	connectedMsg := `{"type":"connected","service":"` + service + `","env":"` + env + `"}`
	encryptedMsg, _ := encryption.Encrypt(connectedMsg)
	c.SSEvent("message", encryptedMsg)
	c.Writer.Flush()

	// 监听配置变更
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// 加密消息
			encryptedMsg, err := encryption.Encrypt(msg)
			if err != nil {
				continue
			}
			c.SSEvent("message", encryptedMsg)
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}

// 全局 SSE 管理器（需要在 main.go 中初始化）
var globalSSEManager *SSEManagerImpl

// SetGlobalSSEManager 设置全局 SSE 管理器
func SetGlobalSSEManager(mgr *SSEManagerImpl) {
	globalSSEManager = mgr
}

// GetGlobalSSEManager 获取全局 SSE 管理器
func GetGlobalSSEManager() *SSEManagerImpl {
	return globalSSEManager
}

// 全局 TokenHandler
var globalTokenHandler *TokenHandler

// SetGlobalTokenHandler 设置全局 TokenHandler
func SetGlobalTokenHandler(handler *TokenHandler) {
	globalTokenHandler = handler
}

// GetGlobalTokenHandler 获取全局 TokenHandler
func GetGlobalTokenHandler() *TokenHandler {
	return globalTokenHandler
}

// SSEManagerImpl SSE 管理器实现
type SSEManagerImpl struct {
	clients map[string]map[chan string]bool
}

// NewSSEManagerImpl 创建 SSE 管理器
func NewSSEManagerImpl() *SSEManagerImpl {
	return &SSEManagerImpl{
		clients: make(map[string]map[chan string]bool),
	}
}

// Subscribe 订阅
func (m *SSEManagerImpl) Subscribe(service, env string) chan string {
	key := fmt.Sprintf("%s:%s", service, env)
	ch := make(chan string, 10)

	if m.clients[key] == nil {
		m.clients[key] = make(map[chan string]bool)
	}
	m.clients[key][ch] = true

	return ch
}

// Unsubscribe 取消订阅
func (m *SSEManagerImpl) Unsubscribe(service, env string, ch chan string) {
	key := fmt.Sprintf("%s:%s", service, env)

	if clients, ok := m.clients[key]; ok {
		delete(clients, ch)
		if len(clients) == 0 {
			delete(m.clients, key)
		}
	}

	close(ch)
}

// BroadcastConfigChange 广播配置变更
func (m *SSEManagerImpl) BroadcastConfigChange(service, env string, version int) {
	key := fmt.Sprintf("%s:%s", service, env)

	clients, ok := m.clients[key]
	if !ok {
		return
	}

	msg := fmt.Sprintf(`{"type":"config_changed","service":"%s","env":"%s","version":%d}`, service, env, version)

	for ch := range clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// BroadcastHeartbeat 广播心跳
func (m *SSEManagerImpl) BroadcastHeartbeat() {
	msg := `{"type":"heartbeat"}`

	for _, clients := range m.clients {
		for ch := range clients {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}
