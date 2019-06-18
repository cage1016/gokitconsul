package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/prometheus"
	"github.com/go-kit/kit/sd"
	kitgrpc "github.com/go-kit/kit/transport/grpc"
	"github.com/lightstep/lightstep-tracer-go"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkinot "github.com/openzipkin-contrib/zipkin-go-opentracing"
	"github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	"github.com/cage1016/gokitconsul"
	"github.com/cage1016/gokitconsul/pb/addsvc"
	"github.com/cage1016/gokitconsul/pkg/addsvc/endpoints"
	"github.com/cage1016/gokitconsul/pkg/addsvc/service"
	"github.com/cage1016/gokitconsul/pkg/addsvc/transports"
	"github.com/cage1016/gokitconsul/pkg/consulregister"
	"github.com/cage1016/gokitconsul/pkg/logger"
	"github.com/cage1016/gokitconsul/tools/localip"
)

const (
	defLogLevel       = "error"
	defConsulHost     = "localhost"
	defConsulPort     = "8500"
	defServiceHost    = "localhost"
	defHTTPPort       = "8180"
	defGRPCPort       = "8181"
	defServerCert     = ""
	defServerKey      = ""
	defClientTLS      = "false"
	defCACerts        = ""
	defZipkinV1URL    = ""
	defZipkinV2URL    = ""
	defLightstepToken = ""
	defAppdashAddr    = ""

	envLogLevel       = "QS_ADDSVC_LOG_LEVEL"
	envConsulHost     = "QS_CONSULT_HOST"
	envConsultPort    = "QS_CONSULT_PORT"
	envServiceHost    = "QS_ADDSVC_SERVICE_HOST"
	envHTTPPort       = "QS_ADDSVC_HTTP_PORT"
	envGRPCPort       = "QS_ADDSVC_GRPC_PORT"
	envServerCert     = "QS_ADDSVC_SERVER_CERT"
	envServerKey      = "QS_ADDSVC_SERVER_KEY"
	envClientTLS      = "QS_ADDSVC_CLIENT_TLS"
	envCACerts        = "QS_ADDSVC_CA_CERTS"
	envZipkinV1URL    = "QS_ADDSVC_ZIPKIN_V1_URL"
	envZipkinV2URL    = "QS_ADDSVC_ZIPKIN_V2_URL"
	envLightstepToken = "QS_ADDSVC_LIGHT_STEP_TOKEN"
	envAppdashAddr    = "QS_ADDSVC_APPDASH_ADDR"
)

const (
	serviceName = "addsvc"
	tag         = "gokitconsul"
)

type config struct {
	logLevel       string
	clientTLS      bool
	caCerts        string
	serviceHost    string
	httpPort       string
	grpcPort       string
	serverCert     string
	serverKey      string
	consulHost     string
	consultPort    string
	zipkinV1URL    string
	zipkinV2URL    string
	lightstepToken string
	appdashAddr    string
}

func main() {
	cfg := loadConfig()
	errs := make(chan error, 2)

	logger, err := logger.New(os.Stdout, cfg.logLevel)
	if err != nil {
		log.Fatalf(err.Error())
	}

	consulAddres := fmt.Sprintf("%s:%s", cfg.consulHost, cfg.consultPort)
	serviceIp := localip.LocalIP()
	servicePort, _ := strconv.Atoi(cfg.grpcPort)
	consulReg := consulregister.NewConsulRegister(consulAddres, serviceName, serviceIp, servicePort, []string{serviceName, tag}, logger)
	svcRegistar, err := consulReg.NewConsulGRPCRegister()
	if err != nil {
		log.Fatalf(err.Error())
	}

	grpcServer, httpHandler := NewServer(cfg, logger)

	go startHTTPServer(svcRegistar, httpHandler, cfg.httpPort, cfg.serverCert, cfg.serverKey, logger, errs)
	go startGRPCServer(svcRegistar, grpcServer, cfg.grpcPort, cfg.serverCert, cfg.serverKey, logger, errs)

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()

	err = <-errs
	svcRegistar.Deregister()
	logger.Error(fmt.Sprintf("%s service terminated: %s", serviceName, err))
}

func loadConfig() config {
	tls, err := strconv.ParseBool(gokitconsul.Env(envClientTLS, defClientTLS))
	if err != nil {
		log.Fatalf("Invalid value passed for %s\n", envClientTLS)
	}

	return config{
		logLevel:       gokitconsul.Env(envLogLevel, defLogLevel),
		clientTLS:      tls,
		caCerts:        gokitconsul.Env(envCACerts, defCACerts),
		serviceHost:    gokitconsul.Env(envServiceHost, defServiceHost),
		httpPort:       gokitconsul.Env(envHTTPPort, defHTTPPort),
		grpcPort:       gokitconsul.Env(envGRPCPort, defGRPCPort),
		serverCert:     gokitconsul.Env(envServerCert, defServerCert),
		serverKey:      gokitconsul.Env(envServerKey, defServerKey),
		consulHost:     gokitconsul.Env(envConsulHost, defConsulHost),
		consultPort:    gokitconsul.Env(envConsultPort, defConsulPort),
		zipkinV1URL:    gokitconsul.Env(envZipkinV1URL, defZipkinV1URL),
		zipkinV2URL:    gokitconsul.Env(envZipkinV2URL, defZipkinV2URL),
		lightstepToken: gokitconsul.Env(envLightstepToken, defLightstepToken),
		appdashAddr:    gokitconsul.Env(envAppdashAddr, defAppdashAddr),
	}

}

func NewServer(cfg config, logger logger.Logger) (addsvc.AddServer, http.Handler) {
	// Determine which OpenTracing tracer to use. We'll pass the tracer to all the
	// components that use it, as a dependency.
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
				hostPort    = "localhost:80"
				serviceName = "addsvc"
			)
			recorder := zipkinot.NewRecorder(collector, debug, hostPort, serviceName)
			tracer, err = zipkinot.NewTracer(recorder)
			if err != nil {
				logger.Log("err", err)
				os.Exit(1)
			}
		} else if cfg.lightstepToken != "" {
			logger.Log("tracer", "LightStep") // probably don't want to print out the token :)
			tracer = lightstep.NewTracer(lightstep.Options{
				AccessToken: cfg.lightstepToken,
			})
			defer lightstep.FlushLightStepTracer(tracer)
		} else if cfg.appdashAddr != "" {
			logger.Log("tracer", "Appdash", "addr", cfg.appdashAddr)
			tracer = appdashot.NewTracer(appdash.NewRemoteCollector(cfg.appdashAddr))
		} else {
			tracer = stdopentracing.GlobalTracer() // no-op
		}
	}

	var zipkinTracer *zipkin.Tracer
	{
		var (
			err           error
			hostPort      = "localhost:80"
			serviceName   = "addsvc"
			useNoopTracer = (cfg.zipkinV2URL == "")
			reporter      = zipkinhttp.NewReporter(cfg.zipkinV2URL)
		)
		defer reporter.Close()
		zEP, _ := zipkin.NewEndpoint(serviceName, hostPort)
		zipkinTracer, err = zipkin.NewTracer(
			reporter, zipkin.WithLocalEndpoint(zEP), zipkin.WithNoopTracer(useNoopTracer),
		)
		if err != nil {
			logger.Log("err", err)
			os.Exit(1)
		}
		if !useNoopTracer {
			logger.Log("tracer", "Zipkin", "type", "Native", "URL", cfg.zipkinV2URL)
		}
	}

	// Create the (sparse) metrics we'll use in the service. They, too, are
	// dependencies that we pass to components that use them.
	var ints, chars metrics.Counter
	{
		// Business-level metrics.
		ints = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: "example",
			Subsystem: "addsvc",
			Name:      "integers_summed",
			Help:      "Total count of integers summed via the Sum method.",
		}, []string{})
		chars = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: "example",
			Subsystem: "addsvc",
			Name:      "characters_concatenated",
			Help:      "Total count of characters concatenated via the Concat method.",
		}, []string{})
	}
	var duration metrics.Histogram
	{
		// Endpoint-level metrics.
		duration = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
			Namespace: "example",
			Subsystem: "addsvc",
			Name:      "request_duration_seconds",
			Help:      "Request duration in seconds.",
		}, []string{"method", "success"})
	}
	http.DefaultServeMux.Handle("/metrics", promhttp.Handler())

	// Build the layers of the service "onion" from the inside out. First, the
	// business logic service; then, the set of endpoints that wrap the service;
	// and finally, a series of concrete transport adapters. The adapters, like
	// the HTTP handler or the gRPC server, are the bridge between Go kit and
	// the interfaces that the transports expect. Note that we're not binding
	// them to ports or anything yet; we'll do that next.
	var (
		service     = service.New(logger, ints, chars)
		endpoints   = endpoints.New(service, logger, duration, tracer, zipkinTracer)
		httpHandler = transports.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger)
		grpcServer  = transports.NewGRPCServer(endpoints, tracer, zipkinTracer, logger)
	)

	return grpcServer, httpHandler
}

func startHTTPServer(registar sd.Registrar, httpHandler http.Handler, port string, certFile string, keyFile string, logger logger.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	if certFile != "" || keyFile != "" {
		logger.Info(fmt.Sprintf("Users service started using https, cert %s key %s, exposed port %s", certFile, keyFile, port))
		//启动前执行注册
		registar.Register()
		errs <- http.ListenAndServeTLS(p, certFile, keyFile, httpHandler)
	} else {
		logger.Info(fmt.Sprintf("Users service started using http, exposed port %s", port))
		//启动前执行注册
		registar.Register()
		errs <- http.ListenAndServe(p, httpHandler)
	}
}

func startGRPCServer(registar sd.Registrar, grpcServer addsvc.AddServer, port string, certFile string, keyFile string, logger logger.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	listener, err := net.Listen("tcp", p)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to listen on port %s: %s", port, err))
	}

	var server *grpc.Server
	if certFile != "" || keyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to load %s certificates: %s", serviceName, err))
			os.Exit(1)
		}
		logger.Info(fmt.Sprintf("%s gRPC service started using https on port %s with cert %s key %s", serviceName, port, certFile, keyFile))
		server = grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor), grpc.Creds(creds))
	} else {
		logger.Info(fmt.Sprintf("%s gRPC service started using http on port %s", serviceName, port))
		server = grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor))
	}
	grpc_health_v1.RegisterHealthServer(server, service.NewBasicService())
	addsvc.RegisterAddServer(server, grpcServer)
	registar.Register()
	logger.Info(fmt.Sprintf("%s gRPC service started, exposed port %s", serviceName, port))
	errs <- server.Serve(listener)
}
