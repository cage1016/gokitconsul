// Package bcrypt provides a hasher implementation utilising bcrypt.
package bcrypt

import (
	"github.com/cage1016/gokitconsul/pkg/authn/service"
	"github.com/qeek-dev/quaistudio/users"
	"golang.org/x/crypto/bcrypt"
)

const cost int = 10

var _ service.Hasher = (*bcryptHasher)(nil)

type bcryptHasher struct{}

// New instantiates a bcrypt-based hasher implementation.
func New() users.Hasher {
	return &bcryptHasher{}
}

func (bh *bcryptHasher) Hash(pwd string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func (bh *bcryptHasher) Compare(plain, hashed string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
}
