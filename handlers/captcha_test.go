package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
}
