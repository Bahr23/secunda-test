package auth

import "golang.org/x/crypto/bcrypt"

type Passwords struct{ Cost int }

func (p Passwords) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), p.Cost)
	return string(hash), err
}

func (p Passwords) Compare(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
