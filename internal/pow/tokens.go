package pow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMalformed = errors.New("malformed token")
	ErrBadMAC    = errors.New("bad mac")
	ErrExpired   = errors.New("expired")
)

type Challenge struct {
	Difficulty int
	Expires    time.Time
	ID         string
}

func GenerateToken(difficulty int, clientIP string, ttl time.Duration, now func() time.Time, secret []byte) (string, error) {
	expires := now().Add(ttl).Unix()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	body := fmt.Sprintf("v1.%d.%d.%x", difficulty, expires, buf)
	mac := sign(secret, body, clientIP)

	// v1.<difficulty>.<expires_unix>.<random_hex>.<mac_hex>
	return body + "." + mac, nil
}

func GenerateTestSecret() ([]byte, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return buf, err
	}
	return buf, nil
}

func VerifyToken(token, clientIP string, secret []byte, now func() time.Time) (Challenge, error) {
	idx := strings.LastIndexByte(token, '.')
	if idx < 0 {
		return Challenge{}, ErrMalformed
	}
	body, macHex := token[:idx], token[idx+1:]

	got, err := hex.DecodeString(macHex)
	if err != nil {
		return Challenge{}, ErrMalformed
	}

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(body + "." + clientIP))
	if !hmac.Equal(got, h.Sum(nil)) {
		return Challenge{}, ErrBadMAC
	}

	parts := strings.Split(body, ".") // v1.difficulty.expires.random
	if len(parts) != 4 || parts[0] != "v1" {
		return Challenge{}, ErrMalformed
	}

	difficulty, err := strconv.Atoi(parts[1])
	if err != nil {
		return Challenge{}, ErrMalformed
	}

	expires, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Challenge{}, ErrMalformed
	}

	if now().Unix() > expires {
		return Challenge{}, ErrExpired
	}

	return Challenge{
		Difficulty: difficulty,
		Expires:    time.Unix(expires, 0),
		ID:         parts[3],
	}, nil
}

func sign(secret []byte, body, clientIP string) string {
	// mac = HMAC-SHA256(secret, version.difficulty.expires.random.clientIP)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(body + "." + clientIP))
	return hex.EncodeToString(h.Sum(nil))
}
