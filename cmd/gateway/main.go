package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/sd/consul"
	consulsd "github.com/go-kit/kit/sd/consul"
	kitgrpc "github.com/go-kit/kit/transport/grpc"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/hashicorp/consul/api"
	"github.com/lightstep/lightstep-tracer-go"
	"github.com/mwitkow/grpc-proxy/proxy"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkinot "github.com/openzipkin-contrib/zipkin-go-opentracing"
	"github.com/openzipkin/zipkin-go"
	opzipkin "github.com/openzipkin/zipkin-go"
	zipkingrpc "github.com/openzipkin/zipkin-go/middleware/grpc"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	"github.com/cage1016/gokitconsul/pkg/gateway/gatewaytransport"
	"github.com/cage1016/gokitconsul/pkg/shared_package/grpclb"
)

const (
	serviceName       = "gateway"
	defLogLevel       = "error"
	defHTTPPort       = "8000"
	defGRPCPort       = "8001"
	defRretryTimeout  = "500" // time.Millisecond
	defRretryMax      = "3"
	defServerCert     = ""
	defServerKey      = ""
	defClientTLS      = "false"
	defZipkinV1URL    = ""
	defZipkinV2URL    = ""
	defLightstepToken = ""
	defAppdashAddr    = ""
	defConsulHost     = "localhost"
	defConsulPort     = "8500"
	envLogLevel       = "QS_GATEWAY_LOG_LEVEL"
	envHTTPPort       = "QS_GATEWAY_HTTP_PORT"
	envGRPCPort       = "QS_GATEWAY_GRPC_PORT"
	envClientTLS      = "QS_GATEWAY_CLIENT_TLS"
	envServerCert     = "QS_GATEWAY_SERVER_CERT"
	envServerKey      = "QS_GATEWAY_SERVER_KEY"
	envRetryMax       = "QS_GATEWAY_RETRY_MAX"
	envRetryTimeout   = "QS_GATEWAY_RETRY_TIMEOUT"
	envZipkinV1URL    = "QS_ZIPKIN_V1_URL"
	envZipkinV2URL    = "QS_ZIPKIN_V2_URL"
	envLightstepToken = "QS_GATEWAY_LIGHT_STEP_TOKEN"
	envAppdashAddr    = "QS_GATEWAY_APPDASH_ADDR"
	envConsulHost     = "QS_CONSULT_HOST"
	envconsultPort    = "QS_CONSULT_PORT"
)

// Env reads specified environment variable. If no value has been found,
// fallback is returned.
func env(key string, fallback string) (s0 string) {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type config struct {
	logLevel       string
	clientTLS      bool
	consulHost     string
	consultPort    string
	httpPort       string
	grpcPort       string
	serverCert     string
	serverKey      string
	retryMax       int64
	retryTimeout   int64
	zipkinV1URL    string
	zipkinV2URL    string
	lightstepToken string
	appdashAddr    string
}

func main() {
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = level.NewFilter(logger, level.AllowInfo())
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}
	cfg := loadConfig(logger)

	client := newConsulClient(cfg.consulHost, cfg.consultPort, logger)

	var tracer stdopentracing.Tracer
	{
		if cfg.zipkinV1URL != "" && cfg.zipkinV2URL == "" {
			logger.Log("tracer", "Zipkin", "type", "OpenTracing", "URL", cfg.zipkinV1URL)
			collector, err := zipkinot.NewHTTPCollector(cfg.zipkinV1URL)
			if err != nil {
				logger.Log("err", err)
				os.Exit(1)
			}
			defer collector.Close()
			var (
				debug       = false
				hostPort    = fmt.Sprintf("localhost:%s", cfg.httpPort)
				serviceName = serviceName
			)
			recorder := zipkinot.NewRecorder(collector, debug, hostPort, serviceName)
			tracer, err = zipkinot.NewTracer(recorder)
			if err != nil {
				logger.Log("err", err)
				os.Exit(1)
			}
		} else if cfg.lightstepToken != "" {
			logger.Log("tracer", "LightStep")
			tracer = lightstep.NewTracer(lightstep.Options{AccessToken: cfg.lightstepToken})
			defer lightstep.FlushLightStepTracer(tracer)
		} else if cfg.appdashAddr != "" {
			logger.Log("tracer", "Appdash", "addr", cfg.appdashAddr)
			tracer = appdashot.NewTracer(appdash.NewRemoteCollector(cfg.appdashAddr))
		} else {
			tracer = stdopentracing.GlobalTracer()
		}
	}

	var zipkinTracer *zipkin.Tracer
	{
		var (
			err           error
			hostPort      = fmt.Sprintf("localhost:%s", cfg.httpPort)
			serviceName   = serviceName
			useNoopTracer = (cfg.zipkinV2URL == "")
			reporter      = zipkinhttp.NewReporter(cfg.zipkinV2URL)
		)
		defer reporter.Close()
		zEP, _ := zipkin.NewEndpoint(serviceName, hostPort)
		zipkinTracer, err = zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(zEP), zipkin.WithNoopTracer(useNoopTracer))
		if err != nil {
			logger.Log("err", err)
			os.Exit(1)
		}
		if !useNoopTracer {
			logger.Log("tracer", "Zipkin", "type", "Native", "URL", cfg.zipkinV2URL)
		}
	}

	ctx := context.Background()
	consultAddress := fmt.Sprintf("%s:%s", cfg.consulHost, cfg.consultPort)
	errs := make(chan error, 2)

	go startHTTPServer(ctx, client, cfg.retryMax, cfg.retryTimeout, tracer, zipkinTracer, cfg.httpPort, cfg.serverCert, cfg.serverKey, logger, errs)
	go startGRPCServer(consultAddress, tracer, zipkinTracer, cfg.grpcPort, cfg.serverCert, cfg.serverKey, logger, errs)

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()

	err := <-errs
	level.Info(logger).Log("serviceName", serviceName, "terminated", err)
}

func loadConfig(logger log.Logger) (cfg config) {
	tls, err := strconv.ParseBool(env(envClientTLS, defClientTLS))
	if err != nil {
		level.Error(logger).Log("envClientTLS", envClientTLS, "error", err)
	}

	retryMax, err := strconv.ParseInt(env(envRetryMax, defRretryMax), 10, 0)
	if err != nil {
		level.Error(logger).Log("envRetryMax", envRetryMax, "error", err)
	}

	retryTimeout, err := strconv.ParseInt(env(envRetryTimeout, defRretryTimeout), 10, 0)
	if err != nil {
		level.Error(logger).Log("envRetryTimeout", envRetryTimeout, "error", err)
	}

	cfg.logLevel = env(envLogLevel, defLogLevel)
	cfg.clientTLS = tls
	cfg.httpPort = env(envHTTPPort, defHTTPPort)
	cfg.grpcPort = env(envGRPCPort, defGRPCPort)
	cfg.serverCert = env(envServerCert, defServerCert)
	cfg.serverKey = env(envServerKey, defServerKey)
	cfg.consulHost = env(envConsulHost, defConsulHost)
	cfg.consultPort = env(envconsultPort, defConsulPort)
	cfg.retryMax = retryMax
	cfg.retryTimeout = retryTimeout
	cfg.zipkinV1URL = env(envZipkinV1URL, defZipkinV1URL)
	cfg.zipkinV2URL = env(envZipkinV2URL, defZipkinV2URL)
	cfg.lightstepToken = env(envLightstepToken, defLightstepToken)
	cfg.appdashAddr = env(envAppdashAddr, defAppdashAddr)
	return
}

func newConsulClient(consulHost, consulPort string, logger log.Logger) consulsd.Client {
	// Service discovery domain. In this example we use Consul.
	var client consulsd.Client
	{
		consulConfig := api.DefaultConfig()
		consulConfig.Address = fmt.Sprintf("%s:%s", consulHost, consulPort)
		level.Debug(logger).Log("consulConfig.Address", consulConfig.Address)
		consulClient, err := api.NewClient(consulConfig)
		if err != nil {
			logger.Log("err", err)
			os.Exit(1)
		}
		client = consulsd.NewClient(consulClient)
	}
	return client
}

func startHTTPServer(ctx context.Context, client consul.Client, retryMax, retryTimeout int64, tracer stdopentracing.Tracer, zipkinTracer *opzipkin.Tracer, port string, certFile string, keyFile string, logger log.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	if certFile != "" || keyFile != "" {
		level.Info(logger).Log("serviceName", serviceName, "protocol", "HTTP", "exposed", port, "certFile", certFile, "keyFile", keyFile)
		errs <- http.ListenAndServeTLS(p, certFile, keyFile, gatewaytransport.MakeHandler(ctx, client, retryMax, retryTimeout, tracer, zipkinTracer, logger))
	} else {
		level.Info(logger).Log("serviceName", serviceName, "protocol", "HTTP", "exposed", port)
		errs <- http.ListenAndServe(p, gatewaytransport.MakeHandler(ctx, client, retryMax, retryTimeout, tracer, zipkinTracer, logger))
	}
}

func startGRPCServer(consultAddress string, tracer stdopentracing.Tracer, zipkinTracer *opzipkin.Tracer, port string, certFile string, keyFile string, logger log.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	listener, err := net.Listen("tcp", p)
	if err != nil {
		level.Error(logger).Log("GRPC", "proxy", "listen", port, "err", err)
		os.Exit(1)
	}

	re := regexp.MustCompile(`([a-zA-Z]+)/`)
	director := func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
		serviceName := func(fullMethodName string) string {
			x := re.FindSubmatch([]byte(fullMethodName))
			return fmt.Sprintf("grpc.health.v1.%v", strings.ToLower(string(x[1])))
		}(fullMethodName)

		// Make sure we never forward internal services.
		if strings.HasPrefix(fullMethodName, "/com.example.internal.") {
			return nil, nil, grpc.Errorf(codes.Unimplemented, "Unknown method")
		}

		md, ok := metadata.FromIncomingContext(ctx)
		// Copy the inbound metadata explicitly.
		outCtx, _ := context.WithCancel(ctx)
		outCtx = metadata.NewOutgoingContext(outCtx, md.Copy())

		if ok {
			conn, err := grpc.DialContext(
				ctx,
				"",
				grpc.WithInsecure(),
				grpc.WithStatsHandler(zipkingrpc.NewClientHandler(zipkinTracer)),
				grpc.WithUnaryInterceptor(
					grpc_middleware.ChainUnaryClient(
						otgrpc.OpenTracingClientInterceptor(tracer),
						grpc_retry.UnaryClientInterceptor(
							grpc_retry.WithBackoff(grpc_retry.BackoffLinear(time.Duration(1)*time.Millisecond)),
							grpc_retry.WithMax(3),
							grpc_retry.WithPerRetryTimeout(time.Duration(5)*time.Millisecond),
							grpc_retry.WithCodes(codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded),
						),
					),
				),
				grpc.WithDefaultCallOptions(grpc.CallCustomCodec(proxy.Codec()), grpc.FailFast(false)),
				grpc.WithBalancer(grpc.RoundRobin(grpclb.NewConsulResolver(consultAddress, serviceName))),
			)
			return outCtx, conn, err
		}
		return nil, nil, grpc.Errorf(codes.Unimplemented, "Unknown method")
	}

	var server *grpc.Server
	if certFile != "" || keyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			level.Error(logger).Log("certificates", creds, "err", err)
			os.Exit(1)
		}
		level.Info(logger).Log("GRPC", "proxy", "exposed", port, "certFile", certFile, "keyFile", keyFile)
		server = grpc.NewServer(
			grpc.UnaryInterceptor(kitgrpc.Interceptor),
			grpc.CustomCodec(proxy.Codec()),
			grpc.UnknownServiceHandler(proxy.TransparentHandler(director)),
			grpc.Creds(creds),
			grpc.StatsHandler(zipkingrpc.NewServerHandler(zipkinTracer)),
		)
	} else {
		level.Info(logger).Log("GRPC", "proxy", "exposed", port)
		server = grpc.NewServer(
			grpc.CustomCodec(proxy.Codec()),
			grpc.UnknownServiceHandler(proxy.TransparentHandler(director)),
			grpc.UnaryInterceptor(kitgrpc.Interceptor),
			grpc.StatsHandler(zipkingrpc.NewServerHandler(zipkinTracer)),
		)
	}
	errs <- server.Serve(listener)
}
