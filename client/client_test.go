package client

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestClientStart(t *testing.T) {
	token := "test-token"

	var mu sync.Mutex
	version := 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/client/configs/order-service/prod":
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			mu.Lock()
			currentVersion := version
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"service":"order-service","env":"prod","version":%d,"configs":{"app.name":"demo","app.json":"{\"enabled\":true}"}}`, currentVersion)
		case r.URL.Path == "/sse/configs":
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatalf("response writer does not support flush")
			}
			connected, err := encrypt(token, `{"type":"connected","service":"order-service","env":"prod"}`)
			if err != nil {
				t.Fatalf("encrypt connected failed: %v", err)
			}
			fmt.Fprintf(w, "event:message\n")
			fmt.Fprintf(w, "data:%s\n\n", connected)
			flusher.Flush()

			mu.Lock()
			version = 2
			mu.Unlock()

			changed, err := encrypt(token, `{"type":"config_changed","service":"order-service","env":"prod","version":2}`)
			if err != nil {
				t.Fatalf("encrypt changed failed: %v", err)
			}
			fmt.Fprintf(w, "event:message\n")
			fmt.Fprintf(w, "data:%s\n\n", changed)
			flusher.Flush()

			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var snapshots []*Snapshot
	client, err := New(Options{
		ServerURL: server.URL,
		Service:   "order-service",
		Env:       "prod",
		Token:     token,
		OnUpdate: func(snapshot *Snapshot) {
			snapshots = append(snapshots, snapshot)
			if len(snapshots) == 2 {
				cancel()
			}
		},
		OnError: func(err error) {
			t.Fatalf("unexpected error: %v", err)
		},
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}
	if snapshots[0].Version != 1 {
		t.Fatalf("expected initial version 1, got %d", snapshots[0].Version)
	}
	if snapshots[1].Version != 2 {
		t.Fatalf("expected updated version 2, got %d", snapshots[1].Version)
	}

	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := snapshots[1].DecodeJSON("app.json", &payload); err != nil {
		t.Fatalf("decode json failed: %v", err)
	}
	if !payload.Enabled {
		t.Fatalf("expected enabled=true")
	}
}

func encrypt(secret, plaintext string) (string, error) {
	key := sha256.Sum256([]byte(secret))
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
