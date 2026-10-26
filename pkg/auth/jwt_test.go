package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTValidation(t *testing.T) {
	secret := "test-secret-at-least-32-characters-long"
	service := NewJWT(secret, "secunda", "secunda-api", time.Hour)
	raw, err := service.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := service.Verify(raw)
	if err != nil || id != 42 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	for _, tc := range []struct {
		name   string
		claims jwt.MapClaims
		method jwt.SigningMethod
		key    string
	}{
		{"expired", jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}, jwt.SigningMethodHS256, secret},
		{"missing expiration", jwt.MapClaims{"exp": nil}, jwt.SigningMethodHS256, secret},
		{"wrong issuer", jwt.MapClaims{"iss": "other"}, jwt.SigningMethodHS256, secret},
		{"wrong audience", jwt.MapClaims{"aud": "other"}, jwt.SigningMethodHS256, secret},
		{"wrong subject", jwt.MapClaims{"sub": "0"}, jwt.SigningMethodHS256, secret},
		{"future activation", jwt.MapClaims{"nbf": time.Now().Add(time.Hour).Unix()}, jwt.SigningMethodHS256, secret},
		{"wrong algorithm", jwt.MapClaims{}, jwt.SigningMethodHS384, secret},
		{"wrong signature", jwt.MapClaims{}, jwt.SigningMethodHS256, "another-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"sub": "42", "iss": "secunda", "aud": "secunda-api", "exp": time.Now().Add(time.Hour).Unix()}
			for k, v := range tc.claims {
				if v == nil {
					delete(claims, k)
				} else {
					claims[k] = v
				}
			}
			raw, err := jwt.NewWithClaims(tc.method, claims).SignedString([]byte(tc.key))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Verify(raw); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
}
