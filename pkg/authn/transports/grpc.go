package transports

import (
	"context"
	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"time"

	pb "github.com/cage1016/gokitconsul/pb/authn"
	"github.com/cage1016/gokitconsul/pkg/authn/endpoints"
	"github.com/cage1016/gokitconsul/pkg/authn/service"
	"github.com/go-kit/kit/circuitbreaker"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/ratelimit"
	"github.com/go-kit/kit/tracing/opentracing"
	"github.com/go-kit/kit/tracing/zipkin"
	grpctransport "github.com/go-kit/kit/transport/grpc"
	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"
	"github.com/sony/gobreaker"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type grpcServer struct {
	login    grpctransport.Handler `json:""`
	logout   grpctransport.Handler `json:""`
	add      grpctransport.Handler `json:""`
	batchAdd grpctransport.Handler `json:""`
}

func (s *grpcServer) Login(ctx context.Context, req *pb.LoginRequest) (rep *pb.LoginReply, err error) {
	_, rp, err := s.login.ServeGRPC(ctx, req)
	if err != nil {
		return nil, grpcEncodeError(err)
	}
	rep = rp.(*pb.LoginReply)
	return rep, nil
}

func (s *grpcServer) Logout(ctx context.Context, req *pb.LogoutRequest) (rep *pb.LogoutReply, err error) {
	_, rp, err := s.logout.ServeGRPC(ctx, req)
	if err != nil {
		return nil, grpcEncodeError(err)
	}
	rep = rp.(*pb.LogoutReply)
	return rep, nil
}

func (s *grpcServer) Add(ctx context.Context, req *pb.AddRequest) (rep *pb.AddReply, err error) {
	_, rp, err := s.add.ServeGRPC(ctx, req)
	if err != nil {
		return nil, grpcEncodeError(err)
	}
	rep = rp.(*pb.AddReply)
	return rep, nil
}

func (s *grpcServer) BatchAdd(ctx context.Context, req *pb.BatchAddRequest) (rep *pb.BatchAddReply, err error) {
	_, rp, err := s.batchAdd.ServeGRPC(ctx, req)
	if err != nil {
		return nil, grpcEncodeError(err)
	}
	rep = rp.(*pb.BatchAddReply)
	return rep, nil
}

// MakeGRPCServer makes a set of endpoints available as a gRPC server.
func MakeGRPCServer(endpoints endpoints.Endpoints, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) (req pb.AuthnServer) { // Zipkin GRPC Server Trace can either be instantiated per gRPC method with a
	// provided operation name or a global tracing service can be instantiated
	// without an operation name and fed to each Go kit gRPC server as a
	// ServerOption.
	// In the latter case, the operation name will be the endpoint's grpc method
	// path if used in combination with the Go kit gRPC Interceptor.
	//
	// In this example, we demonstrate a global Zipkin tracing service with
	// Go kit gRPC Interceptor.
	zipkinServer := zipkin.GRPCServerTrace(zipkinTracer)

	options := []grpctransport.ServerOption{
		grpctransport.ServerErrorLogger(logger),
		zipkinServer,
	}

	return &grpcServer{
		login: grpctransport.NewServer(
			endpoints.LoginEndpoint,
			decodeGRPCLoginRequest,
			encodeGRPCLoginResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "Login", logger)))...,
		),

		logout: grpctransport.NewServer(
			endpoints.LogoutEndpoint,
			decodeGRPCLogoutRequest,
			encodeGRPCLogoutResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "Logout", logger)))...,
		),

		add: grpctransport.NewServer(
			endpoints.AddEndpoint,
			decodeGRPCAddRequest,
			encodeGRPCAddResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "Add", logger)))...,
		),

		batchAdd: grpctransport.NewServer(
			endpoints.BatchAddEndpoint,
			decodeGRPCBatchAddRequest,
			encodeGRPCBatchAddResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "BatchAdd", logger)))...,
		),
	}
}

// decodeGRPCLoginRequest is a transport/grpc.DecodeRequestFunc that converts a
// gRPC request to a user-domain request. Primarily useful in a server.
func decodeGRPCLoginRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.LoginRequest)
	return endpoints.LoginRequest{User: model.User{
		Email:    req.User.GetEmail(),
		Password: req.User.GetPassword(),
	}}, nil
}

// encodeGRPCLoginResponse is a transport/grpc.EncodeResponseFunc that converts a
// user-domain response to a gRPC reply. Primarily useful in a server.
func encodeGRPCLoginResponse(_ context.Context, grpcReply interface{}) (res interface{}, err error) {
	reply := grpcReply.(endpoints.LoginResponse)
	return &pb.LoginReply{Token: reply.Token}, grpcEncodeError(reply.Err)
}

// decodeGRPCLogoutRequest is a transport/grpc.DecodeRequestFunc that converts a
// gRPC request to a user-domain request. Primarily useful in a server.
func decodeGRPCLogoutRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.LogoutRequest)
	return endpoints.LogoutRequest{User: model.User{
		Email:    req.User.GetEmail(),
		Password: req.User.GetPassword(),
	}}, nil
}

// encodeGRPCLogoutResponse is a transport/grpc.EncodeResponseFunc that converts a
// user-domain response to a gRPC reply. Primarily useful in a server.
func encodeGRPCLogoutResponse(_ context.Context, grpcReply interface{}) (res interface{}, err error) {
	reply := grpcReply.(endpoints.LogoutResponse)
	return &pb.LogoutReply{}, grpcEncodeError(reply.Err)
}

// decodeGRPCAddRequest is a transport/grpc.DecodeRequestFunc that converts a
// gRPC request to a user-domain request. Primarily useful in a server.
func decodeGRPCAddRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.AddRequest)
	return endpoints.AddRequest{User: model.User{
		Email:    req.User.GetEmail(),
		Password: req.User.GetPassword(),
	}}, nil
}

// encodeGRPCAddResponse is a transport/grpc.EncodeResponseFunc that converts a
// user-domain response to a gRPC reply. Primarily useful in a server.
func encodeGRPCAddResponse(_ context.Context, grpcReply interface{}) (res interface{}, err error) {
	reply := grpcReply.(endpoints.AddResponse)
	return &pb.AddReply{}, grpcEncodeError(reply.Err)
}

// decodeGRPCBatchAddRequest is a transport/grpc.DecodeRequestFunc that converts a
// gRPC request to a user-domain request. Primarily useful in a server.
func decodeGRPCBatchAddRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.BatchAddRequest)
	users := []model.User{}
	for _, u := range req.Users {
		users = append(users, model.User{
			Email:    u.GetEmail(),
			Password: u.GetPassword(),
		})
	}
	return endpoints.BatchAddRequest{Users: users}, nil
}

// encodeGRPCBatchAddResponse is a transport/grpc.EncodeResponseFunc that converts a
// user-domain response to a gRPC reply. Primarily useful in a server.
func encodeGRPCBatchAddResponse(_ context.Context, grpcReply interface{}) (res interface{}, err error) {
	reply := grpcReply.(endpoints.BatchAddResponse)
	return &pb.BatchAddReply{}, grpcEncodeError(reply.Err)
}

// NewGRPCClient returns an AddService backed by a gRPC server at the other end
// of the conn. The caller is responsible for constructing the conn, and
// eventually closing the underlying transport. We bake-in certain middlewares,
// implementing the client library pattern.
func NewGRPCClient(conn *grpc.ClientConn, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) service.AuthnService { // We construct a single ratelimiter middleware, to limit the total outgoing
	// QPS from this client to all methods on the remote instance. We also
	// construct per-endpoint circuitbreaker middlewares to demonstrate how
	// that's done, although they could easily be combined into a single breaker
	// for the entire remote instance, too.
	limiter := ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 100))

	// Zipkin GRPC Client Trace can either be instantiated per gRPC method with a
	// provided operation name or a global tracing client can be instantiated
	// without an operation name and fed to each Go kit client as ClientOption.
	// In the latter case, the operation name will be the endpoint's grpc method
	// path.
	//
	// In this example, we demonstrace a global tracing client.
	zipkinClient := zipkin.GRPCClientTrace(zipkinTracer)

	// global client middlewares
	options := []grpctransport.ClientOption{
		zipkinClient,
	}

	// The Login endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var loginEndpoint endpoint.Endpoint
	{
		loginEndpoint = grpctransport.NewClient(
			conn,
			"pb.Authn",
			"Login",
			encodeGRPCLoginRequest,
			decodeGRPCLoginResponse,
			pb.LoginReply{},
			append(options, grpctransport.ClientBefore(opentracing.ContextToGRPC(otTracer, logger)))...,
		).Endpoint()
		loginEndpoint = opentracing.TraceClient(otTracer, "Login")(loginEndpoint)
		loginEndpoint = limiter(loginEndpoint)
		loginEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "Login",
			Timeout: 30 * time.Second,
		}))(loginEndpoint)
	}

	// The Logout endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var logoutEndpoint endpoint.Endpoint
	{
		logoutEndpoint = grpctransport.NewClient(
			conn,
			"pb.Authn",
			"Logout",
			encodeGRPCLogoutRequest,
			decodeGRPCLogoutResponse,
			pb.LogoutReply{},
			append(options, grpctransport.ClientBefore(opentracing.ContextToGRPC(otTracer, logger)))...,
		).Endpoint()
		logoutEndpoint = opentracing.TraceClient(otTracer, "Logout")(logoutEndpoint)
		logoutEndpoint = limiter(logoutEndpoint)
		logoutEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "Logout",
			Timeout: 30 * time.Second,
		}))(logoutEndpoint)
	}

	// The Add endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var addEndpoint endpoint.Endpoint
	{
		addEndpoint = grpctransport.NewClient(
			conn,
			"pb.Authn",
			"Add",
			encodeGRPCAddRequest,
			decodeGRPCAddResponse,
			pb.AddReply{},
			append(options, grpctransport.ClientBefore(opentracing.ContextToGRPC(otTracer, logger)))...,
		).Endpoint()
		addEndpoint = opentracing.TraceClient(otTracer, "Add")(addEndpoint)
		addEndpoint = limiter(addEndpoint)
		addEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "Add",
			Timeout: 30 * time.Second,
		}))(addEndpoint)
	}

	// The BatchAdd endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var batchAddEndpoint endpoint.Endpoint
	{
		batchAddEndpoint = grpctransport.NewClient(
			conn,
			"pb.Authn",
			"BatchAdd",
			encodeGRPCBatchAddRequest,
			decodeGRPCBatchAddResponse,
			pb.BatchAddReply{},
			append(options, grpctransport.ClientBefore(opentracing.ContextToGRPC(otTracer, logger)))...,
		).Endpoint()
		batchAddEndpoint = opentracing.TraceClient(otTracer, "BatchAdd")(batchAddEndpoint)
		batchAddEndpoint = limiter(batchAddEndpoint)
		batchAddEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    "BatchAdd",
			Timeout: 30 * time.Second,
		}))(batchAddEndpoint)
	}

	return endpoints.Endpoints{
		LoginEndpoint:    loginEndpoint,
		LogoutEndpoint:   logoutEndpoint,
		AddEndpoint:      addEndpoint,
		BatchAddEndpoint: batchAddEndpoint,
	}
}

// encodeGRPCLoginRequest is a transport/grpc.EncodeRequestFunc that converts a
// user-domain Login request to a gRPC Login request. Primarily useful in a client.
func encodeGRPCLoginRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(endpoints.LoginRequest)
	return &pb.LoginRequest{User: &pb.UserRecord{
		Email:    req.User.Email,
		Password: req.User.Password,
	}}, nil
}

// decodeGRPCLoginResponse is a transport/grpc.DecodeResponseFunc that converts a
// gRPC Login reply to a user-domain Login response. Primarily useful in a client.
func decodeGRPCLoginResponse(_ context.Context, grpcReply interface{}) (interface{}, error) {
	reply := grpcReply.(*pb.LoginReply)
	return endpoints.LoginResponse{Token: reply.Token}, nil
}

// encodeGRPCLogoutRequest is a transport/grpc.EncodeRequestFunc that converts a
// user-domain Logout request to a gRPC Logout request. Primarily useful in a client.
func encodeGRPCLogoutRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(endpoints.LogoutRequest)
	return &pb.LogoutRequest{User: &pb.UserRecord{
		Email:    req.User.Email,
		Password: req.User.Password,
	}}, nil
}

// decodeGRPCLogoutResponse is a transport/grpc.DecodeResponseFunc that converts a
// gRPC Logout reply to a user-domain Logout response. Primarily useful in a client.
func decodeGRPCLogoutResponse(_ context.Context, grpcReply interface{}) (interface{}, error) {
	//reply := grpcReply.(*pb.LogoutReply)
	return endpoints.LogoutResponse{}, nil
}

// encodeGRPCAddRequest is a transport/grpc.EncodeRequestFunc that converts a
// user-domain Add request to a gRPC Add request. Primarily useful in a client.
func encodeGRPCAddRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(endpoints.AddRequest)
	return &pb.AddRequest{User: &pb.UserRecord{
		Email:    req.User.Email,
		Password: req.User.Password,
	}}, nil
}

// decodeGRPCAddResponse is a transport/grpc.DecodeResponseFunc that converts a
// gRPC Add reply to a user-domain Add response. Primarily useful in a client.
func decodeGRPCAddResponse(_ context.Context, grpcReply interface{}) (interface{}, error) {
	//reply := grpcReply.(*pb.AddReply)
	return endpoints.AddResponse{}, nil
}

// encodeGRPCBatchAddRequest is a transport/grpc.EncodeRequestFunc that converts a
// user-domain BatchAdd request to a gRPC BatchAdd request. Primarily useful in a client.
func encodeGRPCBatchAddRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(endpoints.BatchAddRequest)
	users := []*pb.UserRecord{}
	for _, u := range req.Users {
		users = append(users, &pb.UserRecord{
			Email:    u.Email,
			Password: u.Password,
		})
	}

	return &pb.BatchAddRequest{Users: users}, nil
}

// decodeGRPCBatchAddResponse is a transport/grpc.DecodeResponseFunc that converts a
// gRPC BatchAdd reply to a user-domain BatchAdd response. Primarily useful in a client.
func decodeGRPCBatchAddResponse(_ context.Context, grpcReply interface{}) (interface{}, error) {
	//reply := grpcReply.(*pb.BatchAddReply)
	return endpoints.BatchAddResponse{}, nil
}

func grpcEncodeError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if ok {
		return status.Error(st.Code(), st.Message())
	}
	switch err {
	case service.ErrConflict:
		return status.Error(codes.Aborted, err.Error())
	case model.ErrMalformedEntity, service.ErrMalformedEntity:
		return status.Error(codes.InvalidArgument, err.Error())
	case service.ErrNotFound:
		return status.Error(codes.NotFound, err.Error())
	case service.ErrUnauthorizedAccess:
		return status.Error(codes.Unauthenticated, err.Error())
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
