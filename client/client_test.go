package client

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type countedConfig struct {
	Enabled bool `json:"enabled"`
}

var countedConfigUnmarshalCalls int

func (c *countedConfig) UnmarshalJSON(data []byte) error {
	type alias countedConfig
	countedConfigUnmarshalCalls++
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = countedConfig(decoded)
	return nil
}

func TestClientNewAutoLoadAndWatch(t *testing.T) {
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
			fmt.Fprintf(w, `{"service":"order-service","env":"prod","version":%d,"configs":{"app.name":"demo","app.json":"{\"enabled\":true}","app.port":"8080","feature.enabled":"true","request.timeout":"5s"}}`, currentVersion)
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

	updates := make(chan *Snapshot, 4)
	errs := make(chan error, 1)
	client, err := New(ctx, Options{
		ServerURL: server.URL,
		Service:   "order-service",
		Env:       "prod",
		Token:     token,
		OnUpdate: func(snapshot *Snapshot) {
			updates <- snapshot
		},
		OnError: func(err error) {
			errs <- err
		},
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	var snapshots []*Snapshot
	for len(snapshots) < 2 {
		select {
		case snapshot := <-updates:
			snapshots = append(snapshots, snapshot)
			if len(snapshots) == 2 {
				cancel()
			}
		case err := <-errs:
			t.Fatalf("unexpected error: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for updates")
		}
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
	if got := client.Version(); got != 2 {
		t.Fatalf("Version() = %d, want 2", got)
	}
	if got, ok := client.GetString("app.name"); !ok || got != "demo" {
		t.Fatalf("GetString(app.name) = %q, %v, want demo, true", got, ok)
	}
	if got, err := client.GetInt("app.port"); err != nil || got != 8080 {
		t.Fatalf("GetInt(app.port) = %d, %v, want 8080, nil", got, err)
	}
	if got, err := client.GetBool("feature.enabled"); err != nil || !got {
		t.Fatalf("GetBool(feature.enabled) = %v, %v, want true, nil", got, err)
	}
	if got, err := client.GetDuration("request.timeout"); err != nil || got != 5*time.Second {
		t.Fatalf("GetDuration(request.timeout) = %v, %v, want 5s, nil", got, err)
	}

	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := client.DecodeJSON("app.json", &payload); err != nil {
		t.Fatalf("decode json failed: %v", err)
	}
	if !payload.Enabled {
		t.Fatalf("expected enabled=true")
	}

	appCfg, err := GetJSON[countedConfig](client, "app.json")
	if err != nil {
		t.Fatalf("GetJSON(app.json) error = %v", err)
	}
	if !appCfg.Enabled {
		t.Fatalf("GetJSON(app.json) enabled = false, want true")
	}
}

func TestClientAccessorsRequireLoadedConfig(t *testing.T) {
	client, err := NewLazy(Options{
		ServerURL: "http://example.com",
		Service:   "order-service",
		Env:       "prod",
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	if got, ok := client.GetString("app.name"); ok || got != "" {
		t.Fatalf("GetString() = %q, %v, want empty false", got, ok)
	}
	if got := client.Version(); got != 0 {
		t.Fatalf("Version() = %d, want 0", got)
	}
	if _, err := client.GetInt("app.port"); !errors.Is(err, ErrConfigNotLoaded) {
		t.Fatalf("GetInt() error = %v, want %v", err, ErrConfigNotLoaded)
	}
	if _, err := client.GetBool("feature.enabled"); !errors.Is(err, ErrConfigNotLoaded) {
		t.Fatalf("GetBool() error = %v, want %v", err, ErrConfigNotLoaded)
	}
	if _, err := client.GetDuration("request.timeout"); !errors.Is(err, ErrConfigNotLoaded) {
		t.Fatalf("GetDuration() error = %v, want %v", err, ErrConfigNotLoaded)
	}

	var payload struct{}
	if err := client.DecodeJSON("app.json", &payload); !errors.Is(err, ErrConfigNotLoaded) {
		t.Fatalf("DecodeJSON() error = %v, want %v", err, ErrConfigNotLoaded)
	}
}

func TestClientTypedAccessorsParseErrors(t *testing.T) {
	client, err := NewLazy(Options{
		ServerURL: "http://example.com",
		Service:   "order-service",
		Env:       "prod",
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 1,
		Configs: map[string]string{
			"bad.int":      "abc",
			"bad.bool":     "not-bool",
			"bad.duration": "later",
		},
	})

	if _, err := client.GetInt("missing"); !errors.Is(err, ErrConfigKeyNotFound) {
		t.Fatalf("GetInt(missing) error = %v, want ErrConfigKeyNotFound", err)
	}
	if _, err := client.GetInt("bad.int"); err == nil {
		t.Fatalf("GetInt(bad.int) error = nil, want parse error")
	}
	if _, err := client.GetBool("bad.bool"); err == nil {
		t.Fatalf("GetBool(bad.bool) error = nil, want parse error")
	}
	if _, err := client.GetDuration("bad.duration"); err == nil {
		t.Fatalf("GetDuration(bad.duration) error = nil, want parse error")
	}
}

func TestClientAddListener(t *testing.T) {
	client, err := NewLazy(Options{
		ServerURL: "http://example.com",
		Service:   "order-service",
		Env:       "prod",
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	var values []string
	cancel, err := client.AddListener("app.name", func(value string) {
		values = append(values, value)
	})
	if err != nil {
		t.Fatalf("AddListener() error = %v", err)
	}

	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 1,
		Configs: map[string]string{
			"app.name": "demo",
		},
	})
	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 2,
		Configs: map[string]string{
			"app.name": "demo",
		},
	})
	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 3,
		Configs: map[string]string{
			"app.name": "demo-v2",
		},
	})
	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 4,
		Configs: map[string]string{},
	})

	cancel()

	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 5,
		Configs: map[string]string{
			"app.name": "demo-v3",
		},
	})

	if len(values) != 3 {
		t.Fatalf("listener values = %d, want 3", len(values))
	}
	if values[0] != "demo" {
		t.Fatalf("first value = %q, want demo", values[0])
	}
	if values[1] != "demo-v2" {
		t.Fatalf("second value = %q, want demo-v2", values[1])
	}
	if values[2] != "" {
		t.Fatalf("third value = %q, want empty string", values[2])
	}
}

func TestClientAddListenerValidation(t *testing.T) {
	client, err := NewLazy(Options{
		ServerURL: "http://example.com",
		Service:   "order-service",
		Env:       "prod",
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	if _, err := client.AddListener("", func(string) {}); !errors.Is(err, ErrListenerKeyRequired) {
		t.Fatalf("AddListener(empty) error = %v, want %v", err, ErrListenerKeyRequired)
	}
	if _, err := client.AddListener("app.name", nil); !errors.Is(err, ErrListenerRequired) {
		t.Fatalf("AddListener(nil) error = %v, want %v", err, ErrListenerRequired)
	}
}

func TestClientGetJSONUsesCache(t *testing.T) {
	countedConfigUnmarshalCalls = 0

	client, err := NewLazy(Options{
		ServerURL: "http://example.com",
		Service:   "order-service",
		Env:       "prod",
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 1,
		Configs: map[string]string{
			"app.json": `{"enabled":true}`,
		},
	})

	first, err := GetJSON[countedConfig](client, "app.json")
	if err != nil {
		t.Fatalf("first GetJSON() error = %v", err)
	}
	second, err := GetJSON[countedConfig](client, "app.json")
	if err != nil {
		t.Fatalf("second GetJSON() error = %v", err)
	}

	if !first.Enabled || !second.Enabled {
		t.Fatalf("cached values = %+v %+v", first, second)
	}
	if countedConfigUnmarshalCalls != 1 {
		t.Fatalf("unmarshal calls = %d, want 1", countedConfigUnmarshalCalls)
	}

	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 2,
		Configs: map[string]string{
			"app.json": `{"enabled":false}`,
		},
	})

	third, err := GetJSON[countedConfig](client, "app.json")
	if err != nil {
		t.Fatalf("third GetJSON() error = %v", err)
	}
	if third.Enabled {
		t.Fatalf("third GetJSON() enabled = true, want false")
	}
	if countedConfigUnmarshalCalls != 2 {
		t.Fatalf("unmarshal calls after refresh = %d, want 2", countedConfigUnmarshalCalls)
	}
}

func TestClientGetJSONValidation(t *testing.T) {
	client, err := NewLazy(Options{
		ServerURL: "http://example.com",
		Service:   "order-service",
		Env:       "prod",
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	if _, err := GetJSON[countedConfig](client, "app.json"); !errors.Is(err, ErrConfigNotLoaded) {
		t.Fatalf("GetJSON() error = %v, want %v", err, ErrConfigNotLoaded)
	}

	client.setCurrent(&Snapshot{
		Service: "order-service",
		Env:     "prod",
		Version: 1,
		Configs: map[string]string{
			"bad.json": "not-json",
		},
	})

	if _, err := GetJSON[countedConfig](client, "missing"); !errors.Is(err, ErrConfigKeyNotFound) {
		t.Fatalf("GetJSON(missing) error = %v, want ErrConfigKeyNotFound", err)
	}
	if _, err := GetJSON[countedConfig](client, "bad.json"); err == nil {
		t.Fatalf("GetJSON(bad.json) error = nil, want parse error")
	}
}

func TestClientStartRejectsSecondWatcher(t *testing.T) {
	client, err := NewLazy(Options{
		ServerURL: "http://example.com",
		Service:   "order-service",
		Env:       "prod",
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	client.beginWatching()
	defer client.endWatching()

	if err := client.Start(context.Background()); !errors.Is(err, ErrWatcherAlreadyStarted) {
		t.Fatalf("Start() error = %v, want %v", err, ErrWatcherAlreadyStarted)
	}
}

func TestClientStreamHTTPClientDisablesTimeout(t *testing.T) {
	client, err := NewLazy(Options{
		ServerURL:  "http://example.com",
		Service:    "order-service",
		Env:        "prod",
		Token:      "test-token",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	})
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	streamClient := client.streamHTTPClient()
	if streamClient.Timeout != 0 {
		t.Fatalf("stream client timeout = %v, want 0", streamClient.Timeout)
	}
	if streamClient.Transport != client.httpClient.Transport {
		t.Fatalf("stream client should reuse original transport")
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
