package service

import (
	"context"
	"errors"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/metrics"
)

var (
	// ErrTwoZeroes is an arbitrary business rule for the Add method.
	ErrTwoZeroes = errors.New("can't sum two zeroes")

	// ErrIntOverflow protects the Add method. We've decided that this error
	// indicates a misbehaving service and should count against e.g. circuit
	// breakers. So, we return it directly in endpoints, to illustrate the
	// difference. In a real service, this probably wouldn't be the case.
	ErrIntOverflow = errors.New("integer overflow")

	// ErrMaxSizeExceeded protects the Concat method.
	ErrMaxSizeExceeded = errors.New("result exceeds maximum size")
)

// Middleware describes a service (as opposed to endpoint) middleware.
type Middleware func(AddsvcService) AddsvcService

// Service describes a service that adds things together
// Implement yor service methods methods.
// e.x: Foo(ctx context.Context, s string)(rs string, err error)
type AddsvcService interface {
	Sum(ctx context.Context, a int64, b int64) (rs int64, err error)
	Concat(ctx context.Context, a string, b string) (rs string, err error)
}

// the concrete implementation of service interface
type stubAddsvcService struct {
	logger log.Logger `json:"logger"`
}

// New return a new instance of the service.
// If you want to add service middleware this is the place to put them.
func New(logger log.Logger, requestCount metrics.Counter, requestLatency metrics.Histogram) (s AddsvcService) {
	var svc AddsvcService
	{
		svc = &stubAddsvcService{logger: logger}
		svc = LoggingMiddleware(logger)(svc)
		svc = InstrumentingMiddleware(requestCount, requestLatency)(svc)
	}
	return svc
}

// Implement the business logic of Sum
func (ad *stubAddsvcService) Sum(ctx context.Context, a int64, b int64) (rs int64, err error) {
	return a + b, err
}

// Implement the business logic of Concat
func (ad *stubAddsvcService) Concat(ctx context.Context, a string, b string) (rs string, err error) {
	return a + b, err
}
