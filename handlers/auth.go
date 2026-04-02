package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gudegg/disco/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// LoginRequest 登录请求
type LoginRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// AuthHandler 认证处理器
type AuthHandler struct {
	db                   *gorm.DB
	jwtSecret            string
	jwtExpire            int
	captchas             *captchaStore
	captchaLimiter       *requestLimiter
	loginIPLimiter       *requestLimiter
	loginCredentialLimit *requestLimiter
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(db *gorm.DB, jwtSecret string, jwtExpire int) *AuthHandler {
	return &AuthHandler{
		db:                   db,
		jwtSecret:            jwtSecret,
		jwtExpire:            jwtExpire,
		captchas:             NewCaptchaStore(1 * time.Minute),
		captchaLimiter:       NewRequestLimiter(30, 1*time.Minute),
		loginIPLimiter:       NewRequestLimiter(20, 10*time.Minute),
		loginCredentialLimit: NewRequestLimiter(5, 10*time.Minute),
	}
}

// GetCaptcha 获取登录验证码
func (h *AuthHandler) GetCaptcha(c *gin.Context) {
	if !h.captchaLimiter.Allow("captcha:" + c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁，请稍后再试"})
		return
	}

	captcha, err := h.captchas.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate captcha"})
		return
	}

	c.JSON(http.StatusOK, captcha)
}

// generateToken 生成 JWT Token
func (h *AuthHandler) generateToken(userID uint, username string) (string, error) {
	claims := models.JWTClaims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(h.jwtExpire) * time.Second)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.jwtSecret))
}

// Login 登录
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !h.allowLoginAttempt(c.ClientIP(), req.Username) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "登录尝试过于频繁，请稍后再试"})
		return
	}

	if !h.captchas.Verify(req.CaptchaID, req.CaptchaCode) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "验证码错误或已过期"})
		return
	}

	var user models.User
	if err := h.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	token, err := h.generateToken(user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":    token,
		"expires":  h.jwtExpire,
		"username": user.Username,
	})
}

// ChangePassword 修改密码
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, _ := c.Get("userID")

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 查找用户
	var user models.User
	if err := h.db.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "old password is incorrect"})
		return
	}

	// 生成新密码哈希
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	// 更新密码
	user.Password = string(hashedPassword)
	if err := h.db.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password changed successfully"})
}

func (h *AuthHandler) allowLoginAttempt(clientIP, username string) bool {
	normalizedIP := strings.TrimSpace(clientIP)
	if normalizedIP == "" {
		normalizedIP = "unknown"
	}
	normalizedUser := strings.ToLower(strings.TrimSpace(username))
	if normalizedUser == "" {
		normalizedUser = "unknown"
	}

	if !h.loginIPLimiter.Allow("ip:" + normalizedIP) {
		return false
	}

	return h.loginCredentialLimit.Allow("ip-user:" + normalizedIP + "|" + normalizedUser)
}
