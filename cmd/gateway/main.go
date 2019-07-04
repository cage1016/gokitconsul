package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/sd/consul"
	consulsd "github.com/go-kit/kit/sd/consul"
	"github.com/hashicorp/consul/api"
	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"

	"github.com/cage1016/gokitconsul/pkg/gateway/gatewaytransport"
)

const (
	serviceName      = "gateway"
	defLogLevel      = "error"
	defConsulHost    = "localhost"
	defConsulPort    = "8500"
	defHTTPPort      = "9001"
	defRretryTimeout = "500" // time.Millisecond
	defRretryMax     = "3"
	defServerCert    = ""
	defServerKey     = ""
	defClientTLS     = "false"
	envLogLevel      = "QS_GATEWAY_LOG_LEVEL"
	envHTTPPort      = "QS_GATEWAY_HTTP_PORT"
	envClientTLS     = "QS_GATEWAY_CLIENT_TLS"
	envServerCert    = "QS_GATEWAY_SERVER_CERT"
	envServerKey     = "QS_GATEWAY_SERVER_KEY"
	envRetryMax      = "QS_GATEWAY_RETRY_MAX"
	envRetryTimeout  = "QS_GATEWAY_RETRY_TIMEOUT"
	envConsulHost    = "QS_CONSULT_HOST"
	envconsultPort   = "QS_CONSULT_PORT"
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
	logLevel     string
	clientTLS    bool
	consulHost   string
	consultPort  string
	httpPort     string
	serverCert   string
	serverKey    string
	retryMax     int64
	retryTimeout int64
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

	tracer := stdopentracing.GlobalTracer() // no-op
	zipkinTracer, _ := stdzipkin.NewTracer(nil, stdzipkin.WithNoopTracer(true))

	ctx := context.Background()
	errs := make(chan error, 1)

	go startHTTPServer(ctx, client, cfg.retryMax, cfg.retryTimeout, tracer, zipkinTracer, cfg.httpPort, cfg.serverCert, cfg.serverKey, logger, errs)

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()

	err := <-errs
	level.Info(logger).Log("serviceName", serviceName, "terminated", err)
}

func loadConfig(logger log.Logger) config {
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

	return config{
		logLevel:     env(envLogLevel, defLogLevel),
		clientTLS:    tls,
		httpPort:     env(envHTTPPort, defHTTPPort),
		serverCert:   env(envServerCert, defServerCert),
		serverKey:    env(envServerKey, defServerKey),
		consulHost:   env(envConsulHost, defConsulHost),
		consultPort:  env(envconsultPort, defConsulPort),
		retryMax:     retryMax,
		retryTimeout: retryTimeout,
	}
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

func startHTTPServer(ctx context.Context, client consul.Client, retryMax, retryTimeout int64, tracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, port string, certFile string, keyFile string, logger log.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	if certFile != "" || keyFile != "" {
		level.Info(logger).Log("serviceName", serviceName, "protocol", "HTTP", "exposed", port, "certFile", certFile, "keyFile", keyFile)
		errs <- http.ListenAndServeTLS(p, certFile, keyFile, gatewaytransport.MakeHandler(ctx, client, retryMax, retryTimeout, tracer, zipkinTracer, logger))
	} else {
		level.Info(logger).Log("serviceName", serviceName, "protocol", "HTTP", "exposed", port)
		errs <- http.ListenAndServe(p, gatewaytransport.MakeHandler(ctx, client, retryMax, retryTimeout, tracer, zipkinTracer, logger))
	}
}
