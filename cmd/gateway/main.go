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
	"github.com/lightstep/lightstep-tracer-go"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkinot "github.com/openzipkin-contrib/zipkin-go-opentracing"
	"github.com/openzipkin/zipkin-go"
	opzipkin "github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	"github.com/cage1016/gokitconsul/pkg/gateway/gatewaytransport"
)

const (
	serviceName       = "gateway"
	defLogLevel       = "error"
	defHTTPPort       = "8000"
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
