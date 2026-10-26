package auth

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bahr/secunda/internal/entity"
)

type JWT struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
}

func NewJWT(secret, issuer, audience string, ttl time.Duration) *JWT {
	return &JWT{secret: []byte(secret), issuer: issuer, audience: audience, ttl: ttl}
}

func (j *JWT) Issue(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{Subject: strconv.FormatInt(userID, 10), Issuer: j.issuer,
		Audience: jwt.ClaimStrings{j.audience}, IssuedAt: jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl))}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return token, nil
}

func (j *JWT) Verify(raw string) (int64, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(_ *jwt.Token) (any, error) { return j.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(j.issuer),
		jwt.WithAudience(j.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return 0, entity.ErrUnauthenticated
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, entity.ErrUnauthenticated
	}
	return id, nil
}
