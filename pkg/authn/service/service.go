package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-kit/kit/log/level"

	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/metrics"
)

var (
	// ErrConflict indicates usage of the existing email during account
	// registration.
	ErrConflict = errors.New("email already taken")

	// ErrMalformedEntity indicates malformed entity specification (e.g.
	// invalid username or password).
	ErrMalformedEntity = errors.New("malformed entity specification")

	// ErrUnauthorizedAccess indicates missing or invalid credentials provided
	// when accessing a protected resource.
	ErrUnauthorizedAccess = errors.New("missing or invalid credentials provided")

	// ErrNotFound indicates a non-existent entity request.
	ErrNotFound = errors.New("non-existent entity")
)

// Middleware describes a service (as opposed to endpoint) middleware.
type Middleware func(AuthnService) AuthnService

// Service describes a service that adds things together
// Implement yor service methods methods.
// e.x: Foo(ctx context.Context, s string)(rs string, err error)
type AuthnService interface {
	Login(ctx context.Context, user model.User) (token string, err error)
	Logout(ctx context.Context, user model.User) (err error)
	Add(ctx context.Context, user model.User) (err error)
	BatchAdd(ctx context.Context, users []model.User) (err error)
}

// the concrete implementation of service interface
type stubAuthnService struct {
	logger log.Logger `json:"logger"`
	users  model.UserRepository
	hasher Hasher
	idp    IdentityProvider
}

// New return a new instance of the service.
// If you want to add service middleware this is the place to put them.
func New(
	logger log.Logger,
	requestCount metrics.Counter,
	requestLatency metrics.Histogram,
	users model.UserRepository,
	hasher Hasher,
	idp IdentityProvider) (s AuthnService) {
	var svc AuthnService
	{
		svc = &stubAuthnService{
			logger: logger,
			users:  users,
			hasher: hasher,
			idp:    idp,
		}
		svc = LoggingMiddleware(logger)(svc)
		svc = InstrumentingMiddleware(requestCount, requestLatency)(svc)
	}
	return svc
}

// Implement the business logic of Login
func (au *stubAuthnService) Login(ctx context.Context, user model.User) (token string, err error) {
	level.Info(au.logger).Log("login_user", fmt.Sprintf("%v", user))

	dbUser, err := au.users.RetrieveByID(ctx, user.Email)
	if err != nil {
		return "", ErrNotFound
	}

	if err := au.hasher.Compare(user.Password, dbUser.Password); err != nil {
		return "", ErrUnauthorizedAccess
	}

	return au.idp.TemporaryKey(user.Email)
}

// Implement the business logic of Logout
func (au *stubAuthnService) Logout(ctx context.Context, user model.User) (err error) {
	return err
}

// Implement the business logic of Add
func (au *stubAuthnService) Add(ctx context.Context, user model.User) (err error) {
	hash, err := au.hasher.Hash(user.Password)
	if err != nil {
		return ErrMalformedEntity
	}

	user.Password = hash
	return au.users.Save(ctx, user)
}

// Implement the business logic of BatchAdd
func (au *stubAuthnService) BatchAdd(ctx context.Context, users []model.User) (err error) {
	return err
}
