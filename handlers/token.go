package handlers

import (
	"config-center/models"
	"crypto/rand"
	"encoding/hex"
	"net/http"

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
func generateRandomString(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// GetOrCreateToken 获取或创建 Token
func (h *TokenHandler) GetOrCreateToken(c *gin.Context) {
	serviceID := c.Param("service_id")
	env := c.Param("env")

	// 查找是否已存在
	var token models.ServiceToken
	if err := h.db.Where("service_id = ? AND env = ?", serviceID, env).First(&token).Error; err == nil {
		// 已存在，返回现有 token
		c.JSON(http.StatusOK, gin.H{
			"service_id": serviceID,
			"env":        env,
			"token":      token.Token,
			"created_at": token.CreatedAt,
		})
		return
	}

	// 生成新的 token 和 secret key
	tokenStr := generateRandomString(32)
	secretKey := generateRandomString(32)

	token = models.ServiceToken{
		ServiceID: uint(parseUint(serviceID)),
		Env:       env,
		Token:     tokenStr,
		SecretKey: secretKey,
	}

	if err := h.db.Create(&token).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id": serviceID,
		"env":        env,
		"token":      token.Token,
		"created_at": token.CreatedAt,
	})
}

// GetToken 获取 Token 信息
func (h *TokenHandler) GetToken(c *gin.Context) {
	serviceID := c.Param("service_id")
	env := c.Param("env")

	var token models.ServiceToken
	if err := h.db.Where("service_id = ? AND env = ?", serviceID, env).First(&token).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id": serviceID,
		"env":        env,
		"token":      token.Token,
		"created_at": token.CreatedAt,
	})
}

// RegenerateToken 重新生成 Token
func (h *TokenHandler) RegenerateToken(c *gin.Context) {
	serviceID := c.Param("service_id")
	env := c.Param("env")

	// 生成新的 token 和 secret key
	tokenStr := generateRandomString(32)
	secretKey := generateRandomString(32)

	// 删除旧 token
	h.db.Where("service_id = ? AND env = ?", serviceID, env).Delete(&models.ServiceToken{})

	// 创建新 token
	token := models.ServiceToken{
		ServiceID: uint(parseUint(serviceID)),
		Env:       env,
		Token:     tokenStr,
		SecretKey: secretKey,
	}

	if err := h.db.Create(&token).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id": serviceID,
		"env":        env,
		"token":      token.Token,
		"created_at": token.CreatedAt,
	})
}

// DeleteToken 删除 Token
func (h *TokenHandler) DeleteToken(c *gin.Context) {
	serviceID := c.Param("service_id")
	env := c.Param("env")

	if err := h.db.Where("service_id = ? AND env = ?", serviceID, env).Delete(&models.ServiceToken{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "token deleted"})
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

// parseUint 辅助函数
func parseUint(s string) uint64 {
	var result uint64
	for _, c := range s {
		result = result*10 + uint64(c-'0')
	}
	return result
}
