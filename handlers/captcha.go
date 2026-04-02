package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"strings"
	"sync"
	"time"
)

type captchaEntry struct {
	code      string
	expiresAt time.Time
}

type CaptchaPayload struct {
	ID        string `json:"id"`
	Image     string `json:"image"`
	ExpiresIn int    `json:"expires_in"`
}

type captchaStore struct {
	mu      sync.Mutex
	entries map[string]captchaEntry
	ttl     time.Duration
	maxSize int
}

func NewCaptchaStore(ttl time.Duration) *captchaStore {
	return &captchaStore{
		entries: make(map[string]captchaEntry),
		ttl:     ttl,
		maxSize: 2048,
	}
}

func (s *captchaStore) Generate() (*CaptchaPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked()

	code, err := randomDigits(4)
	if err != nil {
		return nil, err
	}

	id, err := randomToken(16)
	if err != nil {
		return nil, err
	}

	image, err := buildCaptchaImage(code)
	if err != nil {
		return nil, err
	}

	s.evictIfNeededLocked()

	s.entries[id] = captchaEntry{
		code:      code,
		expiresAt: time.Now().Add(s.ttl),
	}

	return &CaptchaPayload{
		ID:        id,
		Image:     image,
		ExpiresIn: int(s.ttl.Seconds()),
	}, nil
}

func (s *captchaStore) Verify(id, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked()

	entry, ok := s.entries[id]
	delete(s.entries, id)
	if !ok {
		return false
	}

	return normalizeCaptchaCode(code) == entry.code
}

func (s *captchaStore) cleanupExpiredLocked() {
	now := time.Now()
	for id, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, id)
		}
	}
}

func (s *captchaStore) evictIfNeededLocked() {
	if s.maxSize <= 0 || len(s.entries) < s.maxSize {
		return
	}

	var oldestID string
	var oldestExpiry time.Time
	for id, entry := range s.entries {
		if oldestID == "" || entry.expiresAt.Before(oldestExpiry) {
			oldestID = id
			oldestExpiry = entry.expiresAt
		}
	}
	if oldestID != "" {
		delete(s.entries, oldestID)
	}
}

func randomDigits(length int) (string, error) {
	var builder strings.Builder
	builder.Grow(length)

	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + n.Int64()))
	}

	return builder.String(), nil
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomRange(min, max int64) (int64, error) {
	if max <= min {
		return min, nil
	}

	n, err := rand.Int(rand.Reader, big.NewInt(max-min+1))
	if err != nil {
		return 0, err
	}
	return min + n.Int64(), nil
}

func normalizeCaptchaCode(code string) string {
	return strings.TrimSpace(code)
}

func buildCaptchaImage(code string) (string, error) {
	const width = 132
	const height = 44

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	fillRect(img, 0, 0, width, height, color.RGBA{0xF7, 0xF9, 0xFC, 0xFF})
	drawBorder(img, color.RGBA{0xD9, 0xE2, 0xF2, 0xFF})

	for i := 0; i < 8; i++ {
		x1, err := randomRange(4, width-12)
		if err != nil {
			return "", err
		}
		y1, err := randomRange(4, height-4)
		if err != nil {
			return "", err
		}
		x2, err := randomRange(4, width-4)
		if err != nil {
			return "", err
		}
		y2, err := randomRange(4, height-4)
		if err != nil {
			return "", err
		}
		drawLine(img, int(x1), int(y1), int(x2), int(y2), color.RGBA{0xC6, 0xD6, 0xEE, 0xFF})
	}

	for i := 0; i < 24; i++ {
		x, err := randomRange(3, width-3)
		if err != nil {
			return "", err
		}
		y, err := randomRange(3, height-3)
		if err != nil {
			return "", err
		}
		fillRect(img, int(x), int(y), 2, 2, color.RGBA{0xD1, 0xDE, 0xF2, 0xFF})
	}

	for i, ch := range code {
		offsetX, err := randomRange(-2, 2)
		if err != nil {
			return "", err
		}
		offsetY, err := randomRange(-2, 2)
		if err != nil {
			return "", err
		}
		drawDigit(img, 12+(i*28)+int(offsetX), 7+int(offsetY), ch, color.RGBA{0x2F, 0x5A, 0xA8, 0xFF})
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return "", err
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

func drawDigit(img *image.RGBA, x, y int, ch rune, digitColor color.RGBA) {
	segments, ok := digitSegments[ch]
	if !ok {
		return
	}

	if segments[0] {
		fillRect(img, x+4, y, 12, 4, digitColor)
	}
	if segments[1] {
		fillRect(img, x, y+3, 4, 11, digitColor)
	}
	if segments[2] {
		fillRect(img, x+16, y+3, 4, 11, digitColor)
	}
	if segments[3] {
		fillRect(img, x+4, y+12, 12, 4, digitColor)
	}
	if segments[4] {
		fillRect(img, x, y+15, 4, 11, digitColor)
	}
	if segments[5] {
		fillRect(img, x+16, y+15, 4, 11, digitColor)
	}
	if segments[6] {
		fillRect(img, x+4, y+24, 12, 4, digitColor)
	}
}

func drawBorder(img *image.RGBA, border color.RGBA) {
	bounds := img.Bounds()
	maxX := bounds.Max.X - 1
	maxY := bounds.Max.Y - 1
	for x := bounds.Min.X; x <= maxX; x++ {
		img.SetRGBA(x, bounds.Min.Y, border)
		img.SetRGBA(x, maxY, border)
	}
	for y := bounds.Min.Y; y <= maxY; y++ {
		img.SetRGBA(bounds.Min.X, y, border)
		img.SetRGBA(maxX, y, border)
	}
}

func fillRect(img *image.RGBA, x, y, width, height int, fill color.RGBA) {
	bounds := img.Bounds()
	for px := maxInt(x, bounds.Min.X); px < minInt(x+width, bounds.Max.X); px++ {
		for py := maxInt(y, bounds.Min.Y); py < minInt(y+height, bounds.Max.Y); py++ {
			img.SetRGBA(px, py, fill)
		}
	}
}

func drawLine(img *image.RGBA, x1, y1, x2, y2 int, lineColor color.RGBA) {
	dx := absInt(x2 - x1)
	dy := -absInt(y2 - y1)
	sx := -1
	if x1 < x2 {
		sx = 1
	}
	sy := -1
	if y1 < y2 {
		sy = 1
	}
	err := dx + dy

	for {
		if image.Pt(x1, y1).In(img.Bounds()) {
			img.SetRGBA(x1, y1, lineColor)
		}
		if x1 == x2 && y1 == y2 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

var digitSegments = map[rune][7]bool{
	'0': {true, true, true, false, true, true, true},
	'1': {false, false, true, false, false, true, false},
	'2': {true, false, true, true, true, false, true},
	'3': {true, false, true, true, false, true, true},
	'4': {false, true, true, true, false, true, false},
	'5': {true, true, false, true, false, true, true},
	'6': {true, true, false, true, true, true, true},
	'7': {true, false, true, false, false, true, false},
	'8': {true, true, true, true, true, true, true},
	'9': {true, true, true, true, false, true, true},
}
