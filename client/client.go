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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrConfigNotLoaded = errors.New("config not loaded")
var ErrConfigKeyNotFound = errors.New("config key not found")
var ErrWatcherAlreadyStarted = errors.New("config watcher already started")
var ErrListenerKeyRequired = errors.New("listener key is required")
var ErrListenerRequired = errors.New("listener is required")

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

type ChangeEvent struct {
	Key       string
	OldValue  string
	OldExists bool
	NewValue  string
	NewExists bool
	Version   int
}

type jsonCacheEntry struct {
	raw   string
	value any
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

	mu             sync.RWMutex
	current        *Snapshot
	jsonCache      map[string]map[reflect.Type]jsonCacheEntry
	listenerMu     sync.RWMutex
	listeners      map[string]map[uint64]func(string)
	nextListenerID uint64
	watchMu        sync.Mutex
	watching       bool
}

func New(ctx context.Context, options Options) (*Client, error) {
	client, err := NewLazy(options)
	if err != nil {
		return nil, err
	}
	if _, err := client.Load(ctx); err != nil {
		return nil, err
	}
	client.emitUpdate(client.Current())
	client.startBackground(ctx)
	return client, nil
}

func NewLazy(options Options) (*Client, error) {
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

func (c *Client) GetString(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return "", false
	}
	return c.current.Get(key)
}

func (c *Client) MustGetString(key string) string {
	value, ok := c.GetString(key)
	if !ok {
		panic(fmt.Sprintf("config key %q not found", key))
	}
	return value
}

func (c *Client) AddListener(key string, listener func(string)) (func(), error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrListenerKeyRequired
	}
	if listener == nil {
		return nil, ErrListenerRequired
	}

	c.listenerMu.Lock()
	defer c.listenerMu.Unlock()

	if c.listeners == nil {
		c.listeners = make(map[string]map[uint64]func(string))
	}
	c.nextListenerID++
	id := c.nextListenerID
	if c.listeners[key] == nil {
		c.listeners[key] = make(map[uint64]func(string))
	}
	c.listeners[key][id] = listener

	return func() {
		c.removeListener(key, id)
	}, nil
}

func (c *Client) GetInt(key string) (int, error) {
	value, err := c.getRequiredValue(key)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("config key %q parse int: %w", key, err)
	}
	return parsed, nil
}

func (c *Client) GetBool(key string) (bool, error) {
	value, err := c.getRequiredValue(key)
	if err != nil {
		return false, err
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("config key %q parse bool: %w", key, err)
	}
	return parsed, nil
}

func (c *Client) GetDuration(key string) (time.Duration, error) {
	value, err := c.getRequiredValue(key)
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("config key %q parse duration: %w", key, err)
	}
	return parsed, nil
}

func (c *Client) DecodeJSON(key string, target any) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return ErrConfigNotLoaded
	}
	return c.current.DecodeJSON(key, target)
}

func (c *Client) Version() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return 0
	}
	return c.current.Version
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
	if !c.beginWatching() {
		return ErrWatcherAlreadyStarted
	}
	defer c.endWatching()

	snapshot := c.Current()
	if snapshot == nil {
		var err error
		snapshot, err = c.Load(ctx)
		if err != nil {
			return err
		}
	}
	c.emitUpdate(snapshot)

	return c.watch(ctx)
}

func (c *Client) watch(ctx context.Context) error {
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

func (c *Client) startBackground(ctx context.Context) {
	if !c.beginWatching() {
		return
	}

	go func() {
		defer c.endWatching()
		if err := c.watch(ctx); err != nil && ctx.Err() == nil {
			c.emitError(err)
		}
	}()
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
	resp, err := c.streamHTTPClient().Do(req)
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
	newSnapshot := cloneSnapshot(snapshot)

	c.mu.Lock()
	oldSnapshot := c.current
	c.current = newSnapshot
	c.jsonCache = make(map[string]map[reflect.Type]jsonCacheEntry)
	c.mu.Unlock()

	c.notifyListeners(oldSnapshot, newSnapshot)
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

func (c *Client) removeListener(key string, id uint64) {
	c.listenerMu.Lock()
	defer c.listenerMu.Unlock()

	bucket, ok := c.listeners[key]
	if !ok {
		return
	}
	delete(bucket, id)
	if len(bucket) == 0 {
		delete(c.listeners, key)
	}
}

func (c *Client) notifyListeners(oldSnapshot, newSnapshot *Snapshot) {
	type dispatch struct {
		callback func(string)
		value    string
	}

	c.listenerMu.RLock()
	if len(c.listeners) == 0 {
		c.listenerMu.RUnlock()
		return
	}

	var dispatches []dispatch
	for key, bucket := range c.listeners {
		oldValue, oldExists := getSnapshotValue(oldSnapshot, key)
		newValue, newExists := getSnapshotValue(newSnapshot, key)
		if oldExists == newExists && oldValue == newValue {
			continue
		}

		for _, callback := range bucket {
			dispatches = append(dispatches, dispatch{
				callback: callback,
				value:    newValue,
			})
		}
	}
	c.listenerMu.RUnlock()

	for _, item := range dispatches {
		item.callback(item.value)
	}
}

func (c *Client) beginWatching() bool {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	if c.watching {
		return false
	}
	c.watching = true
	return true
}

func (c *Client) endWatching() {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	c.watching = false
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

func getSnapshotValue(snapshot *Snapshot, key string) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	return snapshot.Get(key)
}

func (c *Client) streamHTTPClient() *http.Client {
	if c.httpClient == nil {
		return &http.Client{}
	}
	cloned := *c.httpClient
	cloned.Timeout = 0
	return &cloned
}

func GetJSON[T any](c *Client, key string) (T, error) {
	var zero T

	if c == nil {
		return zero, ErrConfigNotLoaded
	}

	typeKey := reflect.TypeOf((*T)(nil)).Elem()

	c.mu.RLock()
	if c.current == nil {
		c.mu.RUnlock()
		return zero, ErrConfigNotLoaded
	}

	raw, ok := c.current.Get(key)
	if !ok {
		c.mu.RUnlock()
		return zero, fmt.Errorf("%w: %s", ErrConfigKeyNotFound, key)
	}

	if bucket, ok := c.jsonCache[key]; ok {
		if entry, ok := bucket[typeKey]; ok && entry.raw == raw {
			if cached, ok := entry.value.(T); ok {
				c.mu.RUnlock()
				return cached, nil
			}
		}
	}
	c.mu.RUnlock()

	var parsed T
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return zero, fmt.Errorf("config key %q parse json: %w", key, err)
	}

	c.mu.Lock()
	if c.jsonCache == nil {
		c.jsonCache = make(map[string]map[reflect.Type]jsonCacheEntry)
	}
	if c.jsonCache[key] == nil {
		c.jsonCache[key] = make(map[reflect.Type]jsonCacheEntry)
	}
	c.jsonCache[key][typeKey] = jsonCacheEntry{
		raw:   raw,
		value: parsed,
	}
	c.mu.Unlock()

	return parsed, nil
}

func MustGetJSON[T any](c *Client, key string) T {
	value, err := GetJSON[T](c, key)
	if err != nil {
		panic(err)
	}
	return value
}

func (c *Client) getRequiredValue(key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return "", ErrConfigNotLoaded
	}
	value, ok := c.current.Get(key)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrConfigKeyNotFound, key)
	}
	return value, nil
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
