package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	tokenTypeAccess = "access"
	tokenScopeUser  = "user"
)

var (
	ErrTokenInvalid = errors.New("auth: token invalid")
	ErrTokenExpired = errors.New("auth: token expired")
)

type AccessClaims struct {
	Type         string `json:"typ"`
	Subject      string `json:"sub"`
	SessionID    string `json:"sid"`
	DeviceID     string `json:"deviceId"`
	IssuedAt     int64  `json:"iat"`
	ExpiresAt    int64  `json:"exp"`
	Scope        string `json:"scope"`
	TokenVersion int    `json:"tokenVersion"`
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func signAccessToken(secret string, claims AccessClaims) (string, error) {
	header := tokenHeader{Algorithm: "HS256", Type: "JWT"}

	encodedHeader, err := encodeTokenJSON(header)
	if err != nil {
		return "", err
	}

	encodedClaims, err := encodeTokenJSON(claims)
	if err != nil {
		return "", err
	}

	signingInput := encodedHeader + "." + encodedClaims
	signature := signTokenInput(secret, signingInput)
	return signingInput + "." + signature, nil
}

func verifyAccessToken(secret string, token string, now time.Time) (AccessClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessClaims{}, ErrTokenInvalid
	}

	signingInput := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(signTokenInput(secret, signingInput))) {
		return AccessClaims{}, ErrTokenInvalid
	}

	var header tokenHeader
	if err := decodeTokenJSON(parts[0], &header); err != nil {
		return AccessClaims{}, ErrTokenInvalid
	}
	if header.Algorithm != "HS256" || header.Type != "JWT" {
		return AccessClaims{}, ErrTokenInvalid
	}

	var claims AccessClaims
	if err := decodeTokenJSON(parts[1], &claims); err != nil {
		return AccessClaims{}, ErrTokenInvalid
	}
	if claims.Type != tokenTypeAccess || claims.Subject == "" || claims.SessionID == "" || claims.Scope != tokenScopeUser {
		return AccessClaims{}, ErrTokenInvalid
	}
	if claims.ExpiresAt <= now.Unix() {
		return AccessClaims{}, ErrTokenExpired
	}

	return claims, nil
}

func newRefreshToken() (string, error) {
	random := make([]byte, 48)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "nlr_" + base64.RawURLEncoding.EncodeToString(random), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func encodeTokenJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeTokenJSON(encoded string, dst any) error {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func signTokenInput(secret string, signingInput string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func expiresInSeconds(ttl time.Duration) int {
	return int(ttl.Round(time.Second) / time.Second)
}
