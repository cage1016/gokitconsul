package model

import (
	"context"
	"errors"

	"github.com/asaskevich/govalidator"
)

var (
	// ErrMalformedEntity indicates malformed entity specification (e.g.
	// invalid username or password).
	ErrMalformedEntity = errors.New("malformed entity specification")
)

type Middleware func(UserRepository) UserRepository

// User represents a Mainflux user account. Each user is identified given its
// email and password.
type User struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Validate returns an error if user representation is invalid.
func (u User) Validate() error {
	if u.Email == "" || u.Password == "" {
		return ErrMalformedEntity
	}

	if !govalidator.IsEmail(u.Email) {
		return ErrMalformedEntity
	}

	return nil
}

// UserRepository specifies an account persistence API.
type UserRepository interface {
	// Save persists the user account. A non-nil error is returned to indicate
	// operation failure.
	Save(context.Context, User) error

	// RetrieveByID retrieves user by its unique identifier (i.e. email).
	RetrieveByID(context.Context, string) (User, error)
}
