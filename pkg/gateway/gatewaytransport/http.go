package gatewaytransport

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/sd"
	"github.com/go-kit/kit/sd/consul"
	consulsd "github.com/go-kit/kit/sd/consul"
	"github.com/go-kit/kit/sd/lb"
	"github.com/gorilla/mux"
	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"
	"google.golang.org/grpc"

	addsvcendpoint "github.com/cage1016/gokitconsul/pkg/addsvc/endpoints"
	addsvcservice "github.com/cage1016/gokitconsul/pkg/addsvc/service"
	addsvctransports "github.com/cage1016/gokitconsul/pkg/addsvc/transports"

	foosvcendpoint "github.com/cage1016/gokitconsul/pkg/foosvc/endpoints"
	foosvcservice "github.com/cage1016/gokitconsul/pkg/foosvc/service"
	foosvctransports "github.com/cage1016/gokitconsul/pkg/foosvc/transports"

	authnendpoint "github.com/cage1016/gokitconsul/pkg/authn/endpoints"
	authnservice "github.com/cage1016/gokitconsul/pkg/authn/service"
	authntransports "github.com/cage1016/gokitconsul/pkg/authn/transports"
)

func MakeHandler(_ context.Context, client consul.Client, retryMax, retryTimeout int64, tracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) http.Handler {
	r := mux.NewRouter()

	// addsvc
	{
		var (
			namespace   = "gokitconsul"
			serviceName = "addsvc"
			tags        = []string{namespace, serviceName}
			passingOnly = true
			endpoints   = addsvcendpoint.Endpoints{}
			instancer   = consulsd.NewInstancer(client, logger, "grpc.health.v1.addsvc", tags, passingOnly)
		)
		{
			factory := addSvcFactory(addsvcendpoint.MakeSumEndpoint, tracer, zipkinTracer, logger)
			endpointer := sd.NewEndpointer(instancer, factory, logger)
			balancer := lb.NewRoundRobin(endpointer)
			retry := lb.Retry(int(retryMax), time.Duration(retryTimeout)*time.Millisecond, balancer)
			endpoints.SumEndpoint = retry
		}
		{
			factory := addSvcFactory(addsvcendpoint.MakeConcatEndpoint, tracer, zipkinTracer, logger)
			endpointer := sd.NewEndpointer(instancer, factory, logger)
			balancer := lb.NewRoundRobin(endpointer)
			retry := lb.Retry(int(retryMax), time.Duration(retryTimeout)*time.Millisecond, balancer)
			endpoints.ConcatEndpoint = retry
		}
		r.PathPrefix("/addsvc").Handler(http.StripPrefix("/addsvc", addsvctransports.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger)))
	}

	// foo
	{
		var (
			namespace   = "gokitconsul"
			serviceName = "foosvc"
			tags        = []string{namespace, serviceName}
			passingOnly = true
			endpoints   = foosvcendpoint.Endpoints{}
			instancer   = consulsd.NewInstancer(client, logger, "grpc.health.v1.foosvc", tags, passingOnly)
		)
		{
			factory := fooSvcFactory(foosvcendpoint.MakeFooEndpoint, tracer, zipkinTracer, logger)
			endpointer := sd.NewEndpointer(instancer, factory, logger)
			balancer := lb.NewRoundRobin(endpointer)
			retry := lb.Retry(int(retryMax), time.Duration(retryTimeout)*time.Millisecond, balancer)
			endpoints.FooEndpoint = retry
		}
		r.PathPrefix("/foosvc").Handler(http.StripPrefix("/foosvc", foosvctransports.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger)))
	}

	// authn
	{
		var (
			namespace   = "gokitconsul"
			serviceName = "authn"
			tags        = []string{namespace, serviceName}
			passingOnly = true
			endpoints   = authnendpoint.Endpoints{}
			instancer   = consulsd.NewInstancer(client, logger, "grpc.health.v1.authn", tags, passingOnly)
		)
		{
			factory := authnFactory(authnendpoint.MakeLoginEndpoint, tracer, zipkinTracer, logger)
			endpointer := sd.NewEndpointer(instancer, factory, logger)
			balancer := lb.NewRoundRobin(endpointer)
			retry := lb.Retry(int(retryMax), time.Duration(retryTimeout)*time.Millisecond, balancer)
			endpoints.LoginEndpoint = retry
		}
		{
			factory := authnFactory(authnendpoint.MakeLogoutEndpoint, tracer, zipkinTracer, logger)
			endpointer := sd.NewEndpointer(instancer, factory, logger)
			balancer := lb.NewRoundRobin(endpointer)
			retry := lb.Retry(int(retryMax), time.Duration(retryTimeout)*time.Millisecond, balancer)
			endpoints.LogoutEndpoint = retry
		}
		{
			factory := authnFactory(authnendpoint.MakeAddEndpoint, tracer, zipkinTracer, logger)
			endpointer := sd.NewEndpointer(instancer, factory, logger)
			balancer := lb.NewRoundRobin(endpointer)
			retry := lb.Retry(int(retryMax), time.Duration(retryTimeout)*time.Millisecond, balancer)
			endpoints.AddEndpoint = retry
		}
		r.PathPrefix("/authn").Handler(http.StripPrefix("/authn", authntransports.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger)))
	}

	return r
}

func accessControl(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type")
		if r.Method == "OPTIONS" {
			return
		}
		h.ServeHTTP(w, r)
	})
}

func addSvcFactory(makeEndpoint func(addsvcservice.AddsvcService) endpoint.Endpoint, tracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) sd.Factory {
	return func(instance string) (endpoint.Endpoint, io.Closer, error) {
		// We could just as easily use the HTTP or Thrift client package to make
		// the connection to addsvc. We've chosen gRPC arbitrarily. Note that
		// the transport is an implementation detail: it doesn't leak out of
		// this function. Nice!

		conn, err := grpc.Dial(instance, grpc.WithInsecure())
		if err != nil {
			return nil, nil, err
		}
		service := addsvctransports.NewGRPCClient(conn, tracer, zipkinTracer, logger)

		// Notice that the addsvc gRPC client converts the connection to a
		// complete addsvc, and we just throw away everything except the method
		// we're interested in. A smarter factory would mux multiple methods
		// over the same connection. But that would require more work to manage
		// the returned io.Closer, e.g. reference counting. Since this is for
		// the purposes of demonstration, we'll just keep it simple.

		return makeEndpoint(service), conn, nil
	}
}

func fooSvcFactory(makeEndpoint func(foosvcservice.FoosvcService) endpoint.Endpoint, tracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) sd.Factory {
	return func(instance string) (endpoint.Endpoint, io.Closer, error) {
		// We could just as easily use the HTTP or Thrift client package to make
		// the connection to addsvc. We've chosen gRPC arbitrarily. Note that
		// the transport is an implementation detail: it doesn't leak out of
		// this function. Nice!

		conn, err := grpc.Dial(instance, grpc.WithInsecure())
		if err != nil {
			return nil, nil, err
		}
		service := foosvctransports.NewGRPCClient(conn, tracer, zipkinTracer, logger)

		// Notice that the addsvc gRPC client converts the connection to a
		// complete addsvc, and we just throw away everything except the method
		// we're interested in. A smarter factory would mux multiple methods
		// over the same connection. But that would require more work to manage
		// the returned io.Closer, e.g. reference counting. Since this is for
		// the purposes of demonstration, we'll just keep it simple.

		return makeEndpoint(service), conn, nil
	}
}

func authnFactory(makeEndpoint func(authnservice.AuthnService) endpoint.Endpoint, tracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) sd.Factory {
	return func(instance string) (endpoint.Endpoint, io.Closer, error) {
		// We could just as easily use the HTTP or Thrift client package to make
		// the connection to addsvc. We've chosen gRPC arbitrarily. Note that
		// the transport is an implementation detail: it doesn't leak out of
		// this function. Nice!

		conn, err := grpc.Dial(instance, grpc.WithInsecure())
		if err != nil {
			return nil, nil, err
		}
		service := authntransports.NewGRPCClient(conn, tracer, zipkinTracer, logger)

		// Notice that the addsvc gRPC client converts the connection to a
		// complete addsvc, and we just throw away everything except the method
		// we're interested in. A smarter factory would mux multiple methods
		// over the same connection. But that would require more work to manage
		// the returned io.Closer, e.g. reference counting. Since this is for
		// the purposes of demonstration, we'll just keep it simple.

		return makeEndpoint(service), conn, nil
	}
}
