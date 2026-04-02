package handlers

import (
	"config-center/models"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TokenHandler Token 管理处理器
type TokenHandler struct {
	db *gorm.DB
}

// NewTokenHandler 创建 Token 处理器
func NewTokenHandler(db *gorm.DB) *TokenHandler {
	return &TokenHandler{db: db}
}

// generateRandomString 生成随机字符串
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GetOrCreateToken 获取或创建 Token
func (h *TokenHandler) GetOrCreateToken(c *gin.Context) {
	serviceIDParam := c.Param("service_id")
	env := c.Param("env")
	serviceID, err := parseServiceID(serviceIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := h.loadService(serviceID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 查找是否已存在
	var token models.ServiceToken
	if err := h.db.Where("service_id = ? AND env = ?", serviceID, env).First(&token).Error; err == nil {
		// 已存在，返回现有 token
		c.JSON(http.StatusOK, gin.H{
			"service_id": serviceIDParam,
			"env":        env,
			"token":      token.Token,
			"created_at": token.CreatedAt,
		})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query token"})
		return
	}

	// 生成新的 token 和 secret key
	tokenStr, err := generateRandomString(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}
	secretKey, err := generateRandomString(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token secret"})
		return
	}

	token = models.ServiceToken{
		ServiceID: serviceID,
		Env:       env,
		Token:     tokenStr,
		SecretKey: secretKey,
	}

	if err := h.db.Create(&token).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id": serviceIDParam,
		"env":        env,
		"token":      token.Token,
		"created_at": token.CreatedAt,
	})
}

// GetToken 获取 Token 信息
func (h *TokenHandler) GetToken(c *gin.Context) {
	serviceIDParam := c.Param("service_id")
	env := c.Param("env")
	serviceID, err := parseServiceID(serviceIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := h.loadService(serviceID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var token models.ServiceToken
	if err := h.db.Where("service_id = ? AND env = ?", serviceID, env).First(&token).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id": serviceIDParam,
		"env":        env,
		"token":      token.Token,
		"created_at": token.CreatedAt,
	})
}

// RegenerateToken 重新生成 Token
func (h *TokenHandler) RegenerateToken(c *gin.Context) {
	serviceIDParam := c.Param("service_id")
	env := c.Param("env")
	serviceID, err := parseServiceID(serviceIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := h.loadService(serviceID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 生成新的 token 和 secret key
	tokenStr, err := generateRandomString(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}
	secretKey, err := generateRandomString(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token secret"})
		return
	}

	token := models.ServiceToken{
		ServiceID: serviceID,
		Env:       env,
		Token:     tokenStr,
		SecretKey: secretKey,
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("service_id = ? AND env = ?", serviceID, env).Delete(&models.ServiceToken{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&token).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id": serviceIDParam,
		"env":        env,
		"token":      token.Token,
		"created_at": token.CreatedAt,
	})
}

// DeleteToken 删除 Token
func (h *TokenHandler) DeleteToken(c *gin.Context) {
	serviceIDParam := c.Param("service_id")
	env := c.Param("env")
	serviceID, err := parseServiceID(serviceIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := h.loadService(serviceID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.db.Where("service_id = ? AND env = ?", serviceID, env).Delete(&models.ServiceToken{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "token deleted"})
}

// ListConnections 获取当前在线连接
func (h *TokenHandler) ListConnections(c *gin.Context) {
	serviceIDParam := c.Param("service_id")
	env := c.Param("env")
	serviceID, err := parseServiceID(serviceIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	service, err := h.loadService(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sseManager := GetGlobalSSEManager()
	if sseManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sse manager not initialized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id":  serviceIDParam,
		"service":     service.Name,
		"env":         env,
		"connections": sseManager.ListConnections(service.Name, env),
	})
}

// VerifyToken 验证 Token
func (h *TokenHandler) VerifyToken(tokenStr string) (*models.ServiceToken, error) {
	var token models.ServiceToken
	if err := h.db.Where("token = ?", tokenStr).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

// GetSecretKeyByToken 通过 Token 获取 Secret Key
func (h *TokenHandler) GetSecretKeyByToken(tokenStr string) (string, error) {
	var token models.ServiceToken
	if err := h.db.Where("token = ?", tokenStr).First(&token).Error; err != nil {
		return "", err
	}
	return token.SecretKey, nil
}

func parseServiceID(raw string) (uint, error) {
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid service id")
	}
	return uint(parsed), nil
}

func (h *TokenHandler) loadService(serviceID uint) (*models.Service, error) {
	var service models.Service
	if err := h.db.First(&service, serviceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("service not found")
		}
		return nil, fmt.Errorf("failed to query service")
	}
	return &service, nil
}
