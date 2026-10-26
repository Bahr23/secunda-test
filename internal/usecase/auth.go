package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/bahr/secunda/internal/entity"
)

type Auth struct {
	users     UserRepository
	passwords PasswordHasher
	tokens    TokenIssuer
	dummyHash string
}

func NewAuth(users UserRepository, passwords PasswordHasher, tokens TokenIssuer) (*Auth, error) {
	hash, err := passwords.Hash("dummy-password-for-timing-equalization")
	if err != nil {
		return nil, fmt.Errorf("initialize password verification: %w", err)
	}
	return &Auth{users: users, passwords: passwords, tokens: tokens, dummyHash: hash}, nil
}

func (a *Auth) Register(ctx context.Context, email, password, name string) (entity.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return entity.User{}, fmt.Errorf("%w: email must be a valid address", entity.ErrInvalid)
	}
	if len(password) < 12 || len(password) > 72 {
		return entity.User{}, fmt.Errorf("%w: password must contain 12–72 bytes", entity.ErrInvalid)
	}
	name = strings.TrimSpace(name)
	if err := validateText(name, "name", 100, true); err != nil {
		return entity.User{}, err
	}
	hash, err := a.passwords.Hash(password)
	if err != nil {
		return entity.User{}, fmt.Errorf("hash password: %w", err)
	}
	u := entity.User{Email: email, PasswordHash: hash, Name: name, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := a.users.Create(ctx, &u); err != nil {
		return entity.User{}, fmt.Errorf("register user: %w", err)
	}
	return u, nil
}

func (a *Auth) Login(ctx context.Context, email, password string) (string, error) {
	if len(password) > 72 || len(email) > 254 {
		return "", entity.ErrUnauthenticated
	}
	u, err := a.users.ByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, entity.ErrNotFound) {
		a.passwords.Compare(a.dummyHash, password)
		return "", entity.ErrUnauthenticated
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	if !a.passwords.Compare(u.PasswordHash, password) {
		return "", entity.ErrUnauthenticated
	}
	return a.tokens.Issue(u.ID)
}
