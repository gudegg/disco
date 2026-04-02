package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
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
}

func NewCaptchaStore(ttl time.Duration) *captchaStore {
	return &captchaStore{
		entries: make(map[string]captchaEntry),
		ttl:     ttl,
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
	var svg bytes.Buffer
	svg.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="132" height="44" viewBox="0 0 132 44">`)
	svg.WriteString(`<rect width="132" height="44" rx="6" fill="#f7f9fc"/>`)
	svg.WriteString(`<rect x="1" y="1" width="130" height="42" rx="5" fill="none" stroke="#d9e2f2"/>`)

	for i := 0; i < 5; i++ {
		x1, err := randomRange(4, 120)
		if err != nil {
			return "", err
		}
		y1, err := randomRange(6, 38)
		if err != nil {
			return "", err
		}
		x2, err := randomRange(12, 128)
		if err != nil {
			return "", err
		}
		y2, err := randomRange(6, 38)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&svg, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#c7d6ee" stroke-width="1"/>`, x1, y1, x2, y2)
	}

	for i, ch := range code {
		x := 18 + (i * 26)
		y, err := randomRange(28, 34)
		if err != nil {
			return "", err
		}
		rotate, err := randomRange(-15, 15)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(
			&svg,
			`<text x="%d" y="%d" text-anchor="middle" font-size="24" font-weight="700" font-family="Verdana,Arial,sans-serif" fill="#2f5aa8" transform="rotate(%d %d %d)">%c</text>`,
			x,
			y,
			rotate,
			x,
			y,
			ch,
		)
	}

	svg.WriteString(`</svg>`)

	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg.Bytes()), nil
}
