package client

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Options struct {
	ServerURL         string
	Service           string
	Env               string
	Token             string
	HTTPClient        *http.Client
	ReconnectInterval time.Duration
	OnUpdate          func(*Snapshot)
	OnError           func(error)
}

type Snapshot struct {
	Service string            `json:"service"`
	Env     string            `json:"env"`
	Version int               `json:"version"`
	Configs map[string]string `json:"configs"`
}

func (s *Snapshot) Get(key string) (string, bool) {
	if s == nil {
		return "", false
	}
	value, ok := s.Configs[key]
	return value, ok
}

func (s *Snapshot) DecodeJSON(key string, target any) error {
	if s == nil {
		return errors.New("snapshot is nil")
	}
	value, ok := s.Configs[key]
	if !ok {
		return fmt.Errorf("config key %q not found", key)
	}
	return json.Unmarshal([]byte(value), target)
}

type message struct {
	Type    string `json:"type"`
	Service string `json:"service"`
	Env     string `json:"env"`
	Version int    `json:"version"`
}

type Client struct {
	opts       Options
	httpClient *http.Client

	mu      sync.RWMutex
	current *Snapshot
}

func New(options Options) (*Client, error) {
	options.ServerURL = strings.TrimRight(options.ServerURL, "/")
	if options.ServerURL == "" {
		return nil, errors.New("server url is required")
	}
	if options.Service == "" {
		return nil, errors.New("service is required")
	}
	if options.Env == "" {
		return nil, errors.New("env is required")
	}
	if options.Token == "" {
		return nil, errors.New("token is required")
	}
	if options.ReconnectInterval <= 0 {
		options.ReconnectInterval = 3 * time.Second
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		opts:       options,
		httpClient: httpClient,
	}, nil
}

func (c *Client) Current() *Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneSnapshot(c.current)
}

func (c *Client) Load(ctx context.Context) (*Snapshot, error) {
	requestURL := fmt.Sprintf("%s/api/client/configs/%s/%s", c.opts.ServerURL, url.PathEscape(c.opts.Service), url.PathEscape(c.opts.Env))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.Token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("load config failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var snapshot Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}
	if snapshot.Configs == nil {
		snapshot.Configs = make(map[string]string)
	}
	c.setCurrent(&snapshot)
	return cloneSnapshot(&snapshot), nil
}

func (c *Client) Start(ctx context.Context) error {
	snapshot, err := c.Load(ctx)
	if err != nil {
		return err
	}
	c.emitUpdate(snapshot)

	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := c.subscribeOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.emitError(err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(c.opts.ReconnectInterval):
			}
		}
	}
}

func (c *Client) subscribeOnce(ctx context.Context) error {
	requestURL := fmt.Sprintf(
		"%s/sse/configs?service=%s&env=%s",
		c.opts.ServerURL,
		url.QueryEscape(c.opts.Service),
		url.QueryEscape(c.opts.Env),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.Token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("subscribe config failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		encrypted := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if encrypted == "" {
			continue
		}
		payload, err := decrypt(c.opts.Token, encrypted)
		if err != nil {
			return err
		}
		if err := c.handleMessage(ctx, payload); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (c *Client) handleMessage(ctx context.Context, payload string) error {
	var msg message
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return err
	}
	switch msg.Type {
	case "connected", "heartbeat":
		return nil
	case "config_changed":
		snapshot, err := c.Load(ctx)
		if err != nil {
			return err
		}
		c.emitUpdate(snapshot)
		return nil
	default:
		return nil
	}
}

func (c *Client) setCurrent(snapshot *Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = cloneSnapshot(snapshot)
}

func (c *Client) emitUpdate(snapshot *Snapshot) {
	if c.opts.OnUpdate != nil {
		c.opts.OnUpdate(cloneSnapshot(snapshot))
	}
}

func (c *Client) emitError(err error) {
	if err == nil {
		return
	}
	if c.opts.OnError != nil {
		c.opts.OnError(err)
	}
}

func cloneSnapshot(snapshot *Snapshot) *Snapshot {
	if snapshot == nil {
		return nil
	}
	configs := make(map[string]string, len(snapshot.Configs))
	for key, value := range snapshot.Configs {
		configs[key] = value
	}
	return &Snapshot{
		Service: snapshot.Service,
		Env:     snapshot.Env,
		Version: snapshot.Version,
		Configs: configs,
	}
}

func decrypt(secret, ciphertext string) (string, error) {
	key := sha256.Sum256([]byte(secret))
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
		return "", errors.New("ciphertext too short")
	}
	nonce, cipherBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, cipherBytes, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
