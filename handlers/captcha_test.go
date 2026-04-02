package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestCaptchaStoreVerifySingleUse(t *testing.T) {
	store := NewCaptchaStore(time.Minute)

	payload, err := store.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	entry, ok := store.entries[payload.ID]
	if !ok {
		t.Fatalf("captcha entry not found")
	}

	if !store.Verify(payload.ID, entry.code) {
		t.Fatalf("Verify() = false, want true")
	}

	if store.Verify(payload.ID, entry.code) {
		t.Fatalf("Verify() reused captcha, want false")
	}
}

func TestCaptchaStoreRejectExpiredCode(t *testing.T) {
	store := NewCaptchaStore(10 * time.Millisecond)

	payload, err := store.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	entry := store.entries[payload.ID]
	time.Sleep(20 * time.Millisecond)

	if store.Verify(payload.ID, entry.code) {
		t.Fatalf("Verify() accepted expired captcha")
	}
}

func TestGetCaptcha(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(nil, "secret", 3600)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/auth/captcha", nil)

	handler.GetCaptcha(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GetCaptcha() status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload CaptchaPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.ID == "" || payload.Image == "" {
		t.Fatalf("GetCaptcha() returned empty payload: %+v", payload)
	}

	if !strings.HasPrefix(payload.Image, "data:image/png;base64,") {
		t.Fatalf("GetCaptcha() image prefix = %q", payload.Image)
	}
}

func TestCaptchaImageIsPNG(t *testing.T) {
	store := NewCaptchaStore(time.Minute)

	payload, err := store.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	encoded := strings.TrimPrefix(payload.Image, "data:image/png;base64,")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}

	if img.Bounds().Dx() != 132 || img.Bounds().Dy() != 44 {
		t.Fatalf("image bounds = %v, want 132x44", img.Bounds())
	}
}

func TestGetCaptchaRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(nil, "secret", 3600)
	handler.captchaLimiter = NewRequestLimiter(1, time.Minute)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/captcha", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	ctx.Request = req

	handler.GetCaptcha(ctx)

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	req = httptest.NewRequest(http.MethodGet, "/api/auth/captcha", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	ctx.Request = req
	handler.GetCaptcha(ctx)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("GetCaptcha() second request status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
}

func TestLoginRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(nil, "secret", 3600)
	handler.loginIPLimiter = NewRequestLimiter(1, time.Minute)
	handler.loginCredentialLimit = NewRequestLimiter(1, time.Minute)
	handler.captchas.entries["captcha-1"] = captchaEntry{
		code:      "1234",
		expiresAt: time.Now().Add(time.Minute),
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"admin123","captcha_id":"captcha-1","captcha_code":"0000"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:1234"
	ctx.Request = req
	handler.Login(ctx)

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"admin123","captcha_id":"captcha-1","captcha_code":"1234"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:1234"
	ctx.Request = req
	handler.Login(ctx)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("Login() second request status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
}
