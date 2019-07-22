package transports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	kitjwt "github.com/go-kit/kit/auth/jwt"
	"github.com/go-kit/kit/circuitbreaker"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/ratelimit"
	"github.com/go-kit/kit/sd/lb"
	"github.com/go-kit/kit/tracing/opentracing"
	"github.com/go-kit/kit/tracing/zipkin"
	httptransport "github.com/go-kit/kit/transport/http"
	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sony/gobreaker"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/status"

	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"github.com/cage1016/gokitconsul/pkg/authn/endpoints"
	"github.com/cage1016/gokitconsul/pkg/authn/service"
)

type errorWrapper struct {
	Error string `json:"error"`
}

func JSONErrorDecoder(r *http.Response) error {
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return fmt.Errorf("expected JSON formatted error, got Content-Type %s", contentType)
	}
	var w errorWrapper
	if err := json.NewDecoder(r.Body).Decode(&w); err != nil {
		return err
	}
	return errors.New(w.Error)
}

// NewHTTPHandler returns a handler that makes a set of endpoints available on
// predefined paths.
func NewHTTPHandler(endpoints endpoints.Endpoints, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) http.Handler { // Zipkin HTTP Server Trace can either be instantiated per endpoint with a
	// provided operation name or a global tracing service can be instantiated
	// without an operation name and fed to each Go kit endpoint as ServerOption.
	// In the latter case, the operation name will be the endpoint's http method.
	// We demonstrate a global tracing service here.
	zipkinServer := zipkin.HTTPServerTrace(zipkinTracer)

	options := []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(httpEncodeError),
		httptransport.ServerErrorLogger(logger),
		zipkinServer,
	}

	m := http.NewServeMux()
	m.Handle("/login", httptransport.NewServer(
		endpoints.LoginEndpoint,
		decodeHTTPLoginRequest,
		httptransport.EncodeJSONResponse,
		append(options, httptransport.ServerBefore(opentracing.HTTPToContext(otTracer, "Login", logger)))...,
	))
	m.Handle("/logout", httptransport.NewServer(
		endpoints.LogoutEndpoint,
		decodeHTTPLogoutRequest,
		httptransport.EncodeJSONResponse,
		append(options, httptransport.ServerBefore(
			opentracing.HTTPToContext(otTracer, "Logout", logger),
			kitjwt.HTTPToContext(),
		))...,
	))
	m.Handle("/add", httptransport.NewServer(
		endpoints.AddEndpoint,
		decodeHTTPAddRequest,
		httptransport.EncodeJSONResponse,
		append(options, httptransport.ServerBefore(
			opentracing.HTTPToContext(otTracer, "Add", logger),
			kitjwt.HTTPToContext(),
		))...,
	))
	m.Handle("/batch_add", httptransport.NewServer(
		endpoints.BatchAddEndpoint,
		decodeHTTPBatchAddRequest,
		httptransport.EncodeJSONResponse,
		append(options, httptransport.ServerBefore(
			opentracing.HTTPToContext(otTracer, "BatchAdd", logger),
			kitjwt.HTTPToContext(),
		))...,
	))
	m.Handle("/metrics", promhttp.Handler())
	return m
}

// decodeHTTPLoginRequest is a transport/http.DecodeRequestFunc that decodes a
// JSON-encoded request from the HTTP request body. Primarily useful in a server.
func decodeHTTPLoginRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req endpoints.LoginRequest
	err := json.NewDecoder(r.Body).Decode(&req.User)
	return req, err
}

// decodeHTTPLogoutRequest is a transport/http.DecodeRequestFunc that decodes a
// JSON-encoded request from the HTTP request body. Primarily useful in a server.
func decodeHTTPLogoutRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req endpoints.LogoutRequest
	err := json.NewDecoder(r.Body).Decode(&req.User)
	return req, err
}

// decodeHTTPAddRequest is a transport/http.DecodeRequestFunc that decodes a
// JSON-encoded request from the HTTP request body. Primarily useful in a server.
func decodeHTTPAddRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req endpoints.AddRequest
	err := json.NewDecoder(r.Body).Decode(&req.User)
	return req, err
}

// decodeHTTPBatchAddRequest is a transport/http.DecodeRequestFunc that decodes a
// JSON-encoded request from the HTTP request body. Primarily useful in a server.
func decodeHTTPBatchAddRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req endpoints.BatchAddRequest
	err := json.NewDecoder(r.Body).Decode(&req.Users)
	return req, err
}

// NewHTTPClient returns an AddService backed by an HTTP server living at the
// remote instance. We expect instance to come from a service discovery system,
// so likely of the form "host:port". We bake-in certain middlewares,
// implementing the client library pattern.
func NewHTTPClient(instance string, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) (service.AuthnService, error) { // Quickly sanitize the instance string.
	if !strings.HasPrefix(instance, "http") {
		instance = "http://" + instance
	}
	u, err := url.Parse(instance)
	if err != nil {
		return nil, err
	}

	// We construct a single ratelimiter middleware, to limit the total outgoing
	// QPS from this client to all methods on the remote instance. We also
	// construct per-endpoint circuitbreaker middlewares to demonstrate how
	// that's done, although they could easily be combined into a single breaker
	// for the entire remote instance, too.
	limiter := ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 100))

	// Zipkin HTTP Client Trace can either be instantiated per endpoint with a
	// provided operation name or a global tracing client can be instantiated
	// without an operation name and fed to each Go kit endpoint as ClientOption.
	// In the latter case, the operation name will be the endpoint's http method.
	zipkinClient := zipkin.HTTPClientTrace(zipkinTracer)

	// global client middlewares
	options := []httptransport.ClientOption{
		zipkinClient,
	}

	e := endpoints.Endpoints{}

	// Each individual endpoint is an http/transport.Client (which implements
	// endpoint.Endpoint) that gets wrapped with various middlewares. If you
	// made your own client library, you'd do this work there, so your server
	// could rely on a consistent set of client behavior.
	// The Login endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var loginEndpoint endpoint.Endpoint
	{
		loginEndpoint = httptransport.NewClient(
			"POST",
			copyURL(u, "/login"),
			encodeHTTPLoginRequest,
			decodeHTTPLoginResponse,
			append(options, httptransport.ClientBefore(opentracing.ContextToHTTP(otTracer, logger)))...,
		).Endpoint()
		loginEndpoint = opentracing.TraceClient(otTracer, "Login")(loginEndpoint)
		loginEndpoint = zipkin.TraceEndpoint(zipkinTracer, "Login")(loginEndpoint)
		loginEndpoint = limiter(loginEndpoint)
		loginEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "Login",
			Timeout: 30 * time.Second,
		}))(loginEndpoint)
		e.LoginEndpoint = loginEndpoint
	}

	// The Logout endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var logoutEndpoint endpoint.Endpoint
	{
		logoutEndpoint = httptransport.NewClient(
			"POST",
			copyURL(u, "/logout"),
			encodeHTTPLogoutRequest,
			decodeHTTPLogoutResponse,
			append(options, httptransport.ClientBefore(opentracing.ContextToHTTP(otTracer, logger)))...,
		).Endpoint()
		logoutEndpoint = opentracing.TraceClient(otTracer, "Logout")(logoutEndpoint)
		logoutEndpoint = zipkin.TraceEndpoint(zipkinTracer, "Logout")(logoutEndpoint)
		logoutEndpoint = limiter(logoutEndpoint)
		logoutEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "Logout",
			Timeout: 30 * time.Second,
		}))(logoutEndpoint)
		e.LogoutEndpoint = logoutEndpoint
	}

	// The Add endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var addEndpoint endpoint.Endpoint
	{
		addEndpoint = httptransport.NewClient(
			"POST",
			copyURL(u, "/add"),
			encodeHTTPAddRequest,
			decodeHTTPAddResponse,
			append(options, httptransport.ClientBefore(opentracing.ContextToHTTP(otTracer, logger)))...,
		).Endpoint()
		addEndpoint = opentracing.TraceClient(otTracer, "Add")(addEndpoint)
		addEndpoint = zipkin.TraceEndpoint(zipkinTracer, "Add")(addEndpoint)
		addEndpoint = limiter(addEndpoint)
		addEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "Add",
			Timeout: 30 * time.Second,
		}))(addEndpoint)
		e.AddEndpoint = addEndpoint
	}

	// The BatchAdd endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var batchAddEndpoint endpoint.Endpoint
	{
		batchAddEndpoint = httptransport.NewClient(
			"POST",
			copyURL(u, "/batchadd"),
			encodeHTTPBatchAddRequest,
			decodeHTTPBatchAddResponse,
			append(options, httptransport.ClientBefore(opentracing.ContextToHTTP(otTracer, logger)))...,
		).Endpoint()
		batchAddEndpoint = opentracing.TraceClient(otTracer, "BatchAdd")(batchAddEndpoint)
		batchAddEndpoint = zipkin.TraceEndpoint(zipkinTracer, "BatchAdd")(batchAddEndpoint)
		batchAddEndpoint = limiter(batchAddEndpoint)
		batchAddEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "BatchAdd",
			Timeout: 30 * time.Second,
		}))(batchAddEndpoint)
		e.BatchAddEndpoint = batchAddEndpoint
	}

	// Returning the endpoint.Set as a service.Service relies on the
	// endpoint.Set implementing the Service methods. That's just a simple bit
	// of glue code.
	return e, nil
}

//
func copyURL(base *url.URL, path string) *url.URL {
	next := *base
	next.Path = path
	return &next
}

// encodeHTTPLoginRequest is a transport/http.EncodeRequestFunc that
// JSON-encodes any request to the request body. Primarily useful in a client.
func encodeHTTPLoginRequest(_ context.Context, r *http.Request, request interface{}) (err error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		return err
	}
	r.Body = ioutil.NopCloser(&buf)
	return nil
}

// decodeHTTPLoginResponse is a transport/http.DecodeResponseFunc that decodes a
// JSON-encoded sum response from the HTTP response body. If the response has a
// non-200 status code, we will interpret that as an error and attempt to decode
// the specific error message from the response body. Primarily useful in a client.
func decodeHTTPLoginResponse(_ context.Context, r *http.Response) (interface{}, error) {
	if r.StatusCode != http.StatusOK {
		return nil, JSONErrorDecoder(r)
	}
	var resp endpoints.LoginResponse
	err := json.NewDecoder(r.Body).Decode(&resp)
	return resp, err
}

// encodeHTTPLogoutRequest is a transport/http.EncodeRequestFunc that
// JSON-encodes any request to the request body. Primarily useful in a client.
func encodeHTTPLogoutRequest(_ context.Context, r *http.Request, request interface{}) (err error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		return err
	}
	r.Body = ioutil.NopCloser(&buf)
	return nil
}

// decodeHTTPLogoutResponse is a transport/http.DecodeResponseFunc that decodes a
// JSON-encoded sum response from the HTTP response body. If the response has a
// non-200 status code, we will interpret that as an error and attempt to decode
// the specific error message from the response body. Primarily useful in a client.
func decodeHTTPLogoutResponse(_ context.Context, r *http.Response) (interface{}, error) {
	if r.StatusCode != http.StatusOK {
		return nil, JSONErrorDecoder(r)
	}
	var resp endpoints.LogoutResponse
	err := json.NewDecoder(r.Body).Decode(&resp)
	return resp, err
}

// encodeHTTPAddRequest is a transport/http.EncodeRequestFunc that
// JSON-encodes any request to the request body. Primarily useful in a client.
func encodeHTTPAddRequest(_ context.Context, r *http.Request, request interface{}) (err error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		return err
	}
	r.Body = ioutil.NopCloser(&buf)
	return nil
}

// decodeHTTPAddResponse is a transport/http.DecodeResponseFunc that decodes a
// JSON-encoded sum response from the HTTP response body. If the response has a
// non-200 status code, we will interpret that as an error and attempt to decode
// the specific error message from the response body. Primarily useful in a client.
func decodeHTTPAddResponse(_ context.Context, r *http.Response) (interface{}, error) {
	if r.StatusCode != http.StatusOK {
		return nil, JSONErrorDecoder(r)
	}
	var resp endpoints.AddResponse
	err := json.NewDecoder(r.Body).Decode(&resp)
	return resp, err
}

// encodeHTTPBatchAddRequest is a transport/http.EncodeRequestFunc that
// JSON-encodes any request to the request body. Primarily useful in a client.
func encodeHTTPBatchAddRequest(_ context.Context, r *http.Request, request interface{}) (err error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		return err
	}
	r.Body = ioutil.NopCloser(&buf)
	return nil
}

// decodeHTTPBatchAddResponse is a transport/http.DecodeResponseFunc that decodes a
// JSON-encoded sum response from the HTTP response body. If the response has a
// non-200 status code, we will interpret that as an error and attempt to decode
// the specific error message from the response body. Primarily useful in a client.
func decodeHTTPBatchAddResponse(_ context.Context, r *http.Response) (interface{}, error) {
	if r.StatusCode != http.StatusOK {
		return nil, JSONErrorDecoder(r)
	}
	var resp endpoints.BatchAddResponse
	err := json.NewDecoder(r.Body).Decode(&resp)
	return resp, err
}

func httpEncodeError(_ context.Context, err error, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")

	if lberr, ok := err.(lb.RetryError); ok {
		st, _ := status.FromError(lberr.Final)
		w.WriteHeader(HTTPStatusFromCode(st.Code()))
		json.NewEncoder(w).Encode(errorWrapper{Error: st.Message()})
	} else {
		st, ok := status.FromError(err)
		if ok {
			w.WriteHeader(HTTPStatusFromCode(st.Code()))
			json.NewEncoder(w).Encode(errorWrapper{Error: st.Message()})
		} else {
			switch err {
			case io.ErrUnexpectedEOF:
				w.WriteHeader(http.StatusBadRequest)
			case io.EOF:
				w.WriteHeader(http.StatusBadRequest)
			case service.ErrConflict:
				w.WriteHeader(http.StatusConflict)
			case model.ErrMalformedEntity, service.ErrMalformedEntity:
				w.WriteHeader(http.StatusBadRequest)
			case service.ErrUnauthorizedAccess:
				w.WriteHeader(http.StatusUnauthorized)
			case service.ErrNotFound:
				w.WriteHeader(http.StatusNotFound)
			case kitjwt.ErrTokenContextMissing, kitjwt.ErrTokenExpired, kitjwt.ErrTokenInvalid:
				w.WriteHeader(http.StatusUnauthorized)
			default:
				switch err.(type) {
				case *json.SyntaxError:
					w.WriteHeader(http.StatusBadRequest)
				case *json.UnmarshalTypeError:
					w.WriteHeader(http.StatusBadRequest)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			}
			json.NewEncoder(w).Encode(errorWrapper{Error: err.Error()})
		}
	}
}
