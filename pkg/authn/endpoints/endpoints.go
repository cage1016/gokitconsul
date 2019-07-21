package endpoints

import (
	"context"
	"time"

	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"github.com/cage1016/gokitconsul/pkg/authn/service"
	"github.com/go-kit/kit/circuitbreaker"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/ratelimit"
	"github.com/go-kit/kit/tracing/opentracing"
	"github.com/go-kit/kit/tracing/zipkin"
	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"
	"github.com/sony/gobreaker"
	"golang.org/x/time/rate"
)

// Endpoints collects all of the endpoints that compose the authn service. It's
// meant to be used as a helper struct, to collect all of the endpoints into a
// single parameter.
type Endpoints struct {
	LoginEndpoint    endpoint.Endpoint `json:""`
	LogoutEndpoint   endpoint.Endpoint `json:""`
	AddEndpoint      endpoint.Endpoint `json:""`
	BatchAddEndpoint endpoint.Endpoint `json:""`
}

// New return a new instance of the endpoint that wraps the provided service.
func New(svc service.AuthnService, logger log.Logger, duration metrics.Histogram, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer) (ep Endpoints) {
	var loginEndpoint endpoint.Endpoint
	{
		method := "login"
		loginEndpoint = MakeLoginEndpoint(svc)
		loginEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 100))(loginEndpoint)
		loginEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(loginEndpoint)
		loginEndpoint = opentracing.TraceServer(otTracer, method)(loginEndpoint)
		loginEndpoint = zipkin.TraceEndpoint(zipkinTracer, method)(loginEndpoint)
		loginEndpoint = LoggingMiddleware(log.With(logger, "method", method))(loginEndpoint)
		loginEndpoint = InstrumentingMiddleware(duration.With("method", method))(loginEndpoint)
		ep.LoginEndpoint = loginEndpoint
	}

	var logoutEndpoint endpoint.Endpoint
	{
		method := "logout"
		logoutEndpoint = MakeLogoutEndpoint(svc)
		logoutEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 100))(logoutEndpoint)
		logoutEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(logoutEndpoint)
		logoutEndpoint = opentracing.TraceServer(otTracer, method)(logoutEndpoint)
		logoutEndpoint = zipkin.TraceEndpoint(zipkinTracer, method)(logoutEndpoint)
		logoutEndpoint = LoggingMiddleware(log.With(logger, "method", method))(logoutEndpoint)
		logoutEndpoint = InstrumentingMiddleware(duration.With("method", method))(logoutEndpoint)
		ep.LogoutEndpoint = logoutEndpoint
	}

	var addEndpoint endpoint.Endpoint
	{
		method := "add"
		addEndpoint = MakeAddEndpoint(svc)
		addEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 100))(addEndpoint)
		addEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(addEndpoint)
		addEndpoint = opentracing.TraceServer(otTracer, method)(addEndpoint)
		addEndpoint = zipkin.TraceEndpoint(zipkinTracer, method)(addEndpoint)
		addEndpoint = LoggingMiddleware(log.With(logger, "method", method))(addEndpoint)
		addEndpoint = InstrumentingMiddleware(duration.With("method", method))(addEndpoint)
		ep.AddEndpoint = addEndpoint
	}

	var batchAddEndpoint endpoint.Endpoint
	{
		method := "batchAdd"
		batchAddEndpoint = MakeBatchAddEndpoint(svc)
		batchAddEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 100))(batchAddEndpoint)
		batchAddEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(batchAddEndpoint)
		batchAddEndpoint = opentracing.TraceServer(otTracer, method)(batchAddEndpoint)
		batchAddEndpoint = zipkin.TraceEndpoint(zipkinTracer, method)(batchAddEndpoint)
		batchAddEndpoint = LoggingMiddleware(log.With(logger, "method", method))(batchAddEndpoint)
		batchAddEndpoint = InstrumentingMiddleware(duration.With("method", method))(batchAddEndpoint)
		ep.BatchAddEndpoint = batchAddEndpoint
	}

	return ep
}

// MakeLoginEndpoint returns an endpoint that invokes Login on the service.
// Primarily useful in a server.
func MakeLoginEndpoint(svc service.AuthnService) (ep endpoint.Endpoint) {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(LoginRequest)
		if err := req.validate(); err != nil {
			return LoginResponse{}, err
		}
		token, err := svc.Login(ctx, req.User)
		return LoginResponse{Token: token}, err
	}
}

// Login implements the service interface, so Endpoints may be used as a service.
// This is primarily useful in the context of a client library.
func (e Endpoints) Login(ctx context.Context, user model.User) (token string, err error) {
	resp, err := e.LoginEndpoint(ctx, LoginRequest{User: user})
	if err != nil {
		return
	}
	response := resp.(LoginResponse)
	return response.Token, nil
}

// MakeLogoutEndpoint returns an endpoint that invokes Logout on the service.
// Primarily useful in a server.
func MakeLogoutEndpoint(svc service.AuthnService) (ep endpoint.Endpoint) {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(LogoutRequest)
		if err := req.validate(); err != nil {
			return LogoutResponse{}, err
		}
		err := svc.Logout(ctx, req.User)
		return LogoutResponse{}, err
	}
}

// Logout implements the service interface, so Endpoints may be used as a service.
// This is primarily useful in the context of a client library.
func (e Endpoints) Logout(ctx context.Context, user model.User) (err error) {
	resp, err := e.LogoutEndpoint(ctx, LogoutRequest{User: user})
	if err != nil {
		return
	}
	response := resp.(LogoutResponse)
	return response.Err
}

// MakeAddEndpoint returns an endpoint that invokes Add on the service.
// Primarily useful in a server.
func MakeAddEndpoint(svc service.AuthnService) (ep endpoint.Endpoint) {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(AddRequest)
		if err := req.validate(); err != nil {
			return AddResponse{}, err
		}
		err := svc.Add(ctx, req.User)
		return AddResponse{}, err
	}
}

// Add implements the service interface, so Endpoints may be used as a service.
// This is primarily useful in the context of a client library.
func (e Endpoints) Add(ctx context.Context, user model.User) (err error) {
	resp, err := e.AddEndpoint(ctx, AddRequest{User: user})
	if err != nil {
		return
	}
	response := resp.(AddResponse)
	return response.Err
}

// MakeBatchAddEndpoint returns an endpoint that invokes BatchAdd on the service.
// Primarily useful in a server.
func MakeBatchAddEndpoint(svc service.AuthnService) (ep endpoint.Endpoint) {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(BatchAddRequest)
		if err := req.validate(); err != nil {
			return BatchAddResponse{}, err
		}
		err := svc.BatchAdd(ctx, req.Users)
		return BatchAddResponse{}, err
	}
}

// BatchAdd implements the service interface, so Endpoints may be used as a service.
// This is primarily useful in the context of a client library.
func (e Endpoints) BatchAdd(ctx context.Context, users []model.User) (err error) {
	resp, err := e.BatchAddEndpoint(ctx, BatchAddRequest{Users: users})
	if err != nil {
		return
	}
	response := resp.(BatchAddResponse)
	return response.Err
}
