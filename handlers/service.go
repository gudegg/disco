package handlers

import (
	"config-center/models"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateServiceRequest 创建服务请求
type CreateServiceRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=50"`
	Description string `json:"description" binding:"max=255"`
}

// ServiceHandler 服务处理器
type ServiceHandler struct {
	db *gorm.DB
}

// NewServiceHandler 创建服务处理器
func NewServiceHandler(db *gorm.DB) *ServiceHandler {
	return &ServiceHandler{db: db}
}

// List 获取服务列表
func (h *ServiceHandler) List(c *gin.Context) {
	var services []models.Service
	if err := h.db.Order("id DESC").Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch services"})
		return
	}

	c.JSON(http.StatusOK, services)
}

// Create 创建服务
func (h *ServiceHandler) Create(c *gin.Context) {
	var req CreateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	service := models.Service{
		Name:        req.Name,
		Description: req.Description,
	}

	if err := h.db.Create(&service).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service name already exists"})
		return
	}

	c.JSON(http.StatusOK, service)
}

// Delete 删除服务
func (h *ServiceHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.db.Delete(&models.Service{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete service"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "service deleted"})
}
