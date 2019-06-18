package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/go-kit/kit/sd/consul"
	consulsd "github.com/go-kit/kit/sd/consul"
	"github.com/hashicorp/consul/api"
	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"

	"github.com/cage1016/gokitconsul"
	"github.com/cage1016/gokitconsul/pkg/gateway/gatewaytransport"
	"github.com/cage1016/gokitconsul/pkg/logger"
)

const (
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

const (
	serviceName = "gateway"
)

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
	cfg := loadConfig()

	logger, err := logger.New(os.Stdout, cfg.logLevel)
	if err != nil {
		log.Fatalf(err.Error())
	}

	client := newConsulClient(cfg.consulHost, cfg.consultPort, logger)

	tracer := stdopentracing.GlobalTracer() // no-op
	zipkinTracer, _ := stdzipkin.NewTracer(nil, stdzipkin.WithNoopTracer(true))

	ctx := context.Background()
	errs := make(chan error, 2)

	go startHTTPServer(ctx, client, cfg.retryMax, cfg.retryTimeout, tracer, zipkinTracer, cfg.httpPort, cfg.serverCert, cfg.serverKey, logger, errs)

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()

	err = <-errs
	logger.Error(fmt.Sprintf("%s service terminated: %s", serviceName, err))
}

func loadConfig() config {
	tls, err := strconv.ParseBool(gokitconsul.Env(envClientTLS, defClientTLS))
	if err != nil {
		log.Fatalf("Invalid value passed for %s\n", envClientTLS)
	}

	retryMax, err := strconv.ParseInt(gokitconsul.Env(envRetryMax, defRretryMax), 10, 0)
	if err != nil {
		log.Fatalf("Invalid value passed for %s\n", envRetryMax)
	}

	retryTimeout, err := strconv.ParseInt(gokitconsul.Env(envRetryTimeout, defRretryTimeout), 10, 0)
	if err != nil {
		log.Fatalf("Invalid value passed for %s\n", envRetryTimeout)
	}

	return config{
		logLevel:     gokitconsul.Env(envLogLevel, defLogLevel),
		clientTLS:    tls,
		httpPort:     gokitconsul.Env(envHTTPPort, defHTTPPort),
		serverCert:   gokitconsul.Env(envServerCert, defServerCert),
		serverKey:    gokitconsul.Env(envServerKey, defServerKey),
		consulHost:   gokitconsul.Env(envConsulHost, defConsulHost),
		consultPort:  gokitconsul.Env(envconsultPort, defConsulPort),
		retryMax:     retryMax,
		retryTimeout: retryTimeout,
	}
}

func newConsulClient(consulHost, consulPort string, logger logger.Logger) consulsd.Client {
	// Service discovery domain. In this example we use Consul.
	var client consulsd.Client
	{
		consulConfig := api.DefaultConfig()
		consulConfig.Address = fmt.Sprintf("%s:%s", consulHost, consulPort)
		logger.Debug(fmt.Sprintf("consulConfig.Address %s", consulConfig.Address))
		consulClient, err := api.NewClient(consulConfig)
		if err != nil {
			logger.Log("err", err)
			os.Exit(1)
		}
		client = consulsd.NewClient(consulClient)
	}
	return client
}

func startHTTPServer(ctx context.Context, client consul.Client, retryMax, retryTimeout int64, tracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, port string, certFile string, keyFile string, logger logger.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	if certFile != "" || keyFile != "" {
		logger.Info(fmt.Sprintf("%s service started using https, cert %s key %s, exposed port %s", serviceName, certFile, keyFile, port))
		errs <- http.ListenAndServeTLS(p, certFile, keyFile, gatewaytransport.MakeHandler(ctx, client, retryMax, retryTimeout, tracer, zipkinTracer, logger))
	} else {
		logger.Info(fmt.Sprintf("%s service started using http, exposed port %s", serviceName, port))
		errs <- http.ListenAndServe(p, gatewaytransport.MakeHandler(ctx, client, retryMax, retryTimeout, tracer, zipkinTracer, logger))
	}
}
