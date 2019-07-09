package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	pb "github.com/cage1016/gokitconsul/pb/foosvc"
	addsvctransports "github.com/cage1016/gokitconsul/pkg/addsvc/transports"
	"github.com/cage1016/gokitconsul/pkg/consulregister"
	"github.com/cage1016/gokitconsul/pkg/foosvc/endpoints"
	"github.com/cage1016/gokitconsul/pkg/foosvc/service"
	"github.com/cage1016/gokitconsul/pkg/foosvc/transports"
)

const (
	defConsulHost     string = "localhost"
	defConsulPort     string = "8500"
	defZipkinV1URL    string = ""
	defZipkinV2URL    string = ""
	defLightstepToken string = ""
	defAppdashAddr    string = ""
	defNameSpace      string = "gokitconsul"
	defServiceName    string = "foosvc"
	defLogLevel       string = "error"
	defServiceHost    string = "localhost"
	defHTTPPort       string = "8180"
	defGRPCPort       string = "8181"
	defServerCert     string = ""
	defServerKey      string = ""
	defClientTLS      string = "false"
	defCACerts        string = ""
	defAddSvceURL     string = ""
	envConsulHost     string = "QS_CONSULT_HOST"
	envConsultPort    string = "QS_CONSULT_PORT"
	envZipkinV1URL    string = "QS_ZIPKIN_V1_URL"
	envZipkinV2URL    string = "QS_ZIPKIN_V2_URL"
	envLightstepToken string = "QS_LIGHT_STEP_TOKEN"
	envAppdashAddr    string = "QS_APPDASH_ADDR"
	envNameSpace      string = "QS_foosvc_NAMESPACE"
	envServiceName    string = "QS_foosvc_SERVICE_NAME"
	envLogLevel       string = "QS_FOOSVC_LOG_LEVEL"
	envServiceHost    string = "QS_FOOSVC_SERVICE_HOST"
	envHTTPPort       string = "QS_FOOSVC_HTTP_PORT"
	envGRPCPort       string = "QS_FOOSVC_GRPC_PORT"
	envServerCert     string = "QS_FOOSVC_SERVER_CERT"
	envServerKey      string = "QS_FOOSVC_SERVER_KEY"
	envClientTLS      string = "QS_FOOSVC_CLIENT_TLS"
	envCACerts        string = "QS_FOOSVC_CA_CERTS"
	envAddSvcURL      string = "QS_ADDSVC_URL"
)

type config struct {
	nameSpace      string `json:"name_space"`
	serviceName    string `json:"service_name"`
	logLevel       string `json:"log_level"`
	clientTLS      bool   `json:"client_tls"`
	caCerts        string `json:"ca_certs"`
	serviceHost    string `json:"service_host"`
	httpPort       string `json:"http_port"`
	grpcPort       string `json:"grpc_port"`
	serverCert     string `json:"server_cert"`
	serverKey      string `json:"server_key"`
	consulHost     string `json:"consul_host"`
	consultPort    string `json:"consult_port"`
	zipkinV1URL    string `json:"zipkin_v1url"`
	zipkinV2URL    string `json:"zipkin_v2url"`
	lightstepToken string `json:"lightstep_token"`
	appdashAddr    string `json:"appdash_addr"`
	addSvceURL     string
}

// Env reads specified environment variable. If no value has been found,
// fallback is returned.
func env(key string, fallback string) (s0 string) {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func localIP() (s0 string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
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

	consulAddres := fmt.Sprintf("%s:%s", cfg.consulHost, cfg.consultPort)
	serviceIp := localIP()
	servicePort, _ := strconv.Atoi(cfg.grpcPort)
	consulReg := consulregister.NewConsulRegister(consulAddres, cfg.serviceName, serviceIp, servicePort, []string{cfg.nameSpace, cfg.serviceName}, logger)
	svcRegistar, err := consulReg.NewConsulGRPCRegister()
	defer svcRegistar.Deregister()
	if err != nil {
		level.Error(logger).Log(
			"consulAddres", consulAddres,
			"serviceName", cfg.serviceName,
			"serviceIp", serviceIp,
			"servicePort", servicePort,
			"tags", []string{cfg.nameSpace, cfg.serviceName},
			"err", err,
		)
	}

	conn := connectToAddsvc(cfg, logger)
	defer conn.Close()

	errs := make(chan error, 2)
	grpcServer, httpHandler := NewServer(cfg, conn, logger)
	go startHTTPServer(cfg, httpHandler, logger, errs)
	go startGRPCServer(cfg, svcRegistar, grpcServer, logger, errs)

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()

	err = <-errs
	level.Info(logger).Log("serviceName", cfg.serviceName, "terminated", err)
}

func loadConfig(logger log.Logger) (cfg config) {
	tls, err := strconv.ParseBool(env(envClientTLS, defClientTLS))
	if err != nil {
		level.Error(logger).Log("envClientTLS", envClientTLS, "error", err)
	}

	cfg.nameSpace = env(envNameSpace, defNameSpace)
	cfg.serviceName = env(envServiceName, defServiceName)
	cfg.logLevel = env(envLogLevel, defLogLevel)
	cfg.clientTLS = tls
	cfg.caCerts = env(envCACerts, defCACerts)
	cfg.serviceHost = env(envServiceHost, defServiceHost)
	cfg.httpPort = env(envHTTPPort, defHTTPPort)
	cfg.grpcPort = env(envGRPCPort, defGRPCPort)
	cfg.serverCert = env(envServerCert, defServerCert)
	cfg.serverKey = env(envServerKey, defServerKey)
	cfg.consulHost = env(envConsulHost, defConsulHost)
	cfg.consultPort = env(envConsultPort, defConsulPort)
	cfg.zipkinV1URL = env(envZipkinV1URL, defZipkinV1URL)
	cfg.zipkinV2URL = env(envZipkinV2URL, defZipkinV2URL)
	cfg.lightstepToken = env(envLightstepToken, defLightstepToken)
	cfg.appdashAddr = env(envAppdashAddr, defAppdashAddr)
	cfg.addSvceURL = env(envAddSvcURL, defAddSvceURL)
	return cfg
}

func NewServer(cfg config, conn *grpc.ClientConn, logger log.Logger) (pb.FoosvcServer, http.Handler) {
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
				serviceName = cfg.serviceName
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
			serviceName   = cfg.serviceName
			useNoopTracer = (cfg.zipkinV2URL == "")
			reporter      = zipkinhttp.NewReporter(cfg.zipkinV2URL)
		)
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

	var (
		requestCount   metrics.Counter
		requestLatency metrics.Histogram
		fieldKeys      []string
	)
	{
		fieldKeys = []string{"method", "error"}
		requestCount = prometheus.NewCounterFrom(stdprometheus.CounterOpts{Namespace: cfg.nameSpace, Subsystem: cfg.serviceName, Name: "request_count", Help: "Number of requests received."}, fieldKeys)
		requestLatency = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{Namespace: cfg.nameSpace, Subsystem: cfg.serviceName, Name: "request_latency_microseconds", Help: "Total duration of requests in microseconds."}, fieldKeys)
	}

	var duration metrics.Histogram
	{
		duration = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{Namespace: cfg.nameSpace, Subsystem: cfg.serviceName, Name: "request_duration_ns", Help: "Request duration in nanoseconds."}, []string{"method", "success"})
	}

	addsvcservice := addsvctransports.NewGRPCClient(conn, tracer, zipkinTracer, logger)
	service := service.New(addsvcservice, logger, requestCount, requestLatency)
	endpoints := endpoints.New(service, logger, duration, tracer, zipkinTracer)
	httpHandler := transports.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger)
	grpcServer := transports.MakeGRPCServer(endpoints, tracer, zipkinTracer, logger)

	return grpcServer, httpHandler
}

func connectToAddsvc(cfg config, logger log.Logger) *grpc.ClientConn {
	var opts []grpc.DialOption
	if cfg.clientTLS {
		if cfg.caCerts != "" {
			tpc, err := credentials.NewClientTLSFromFile(cfg.caCerts, "")
			if err != nil {
				level.Error(logger).Log("serviceName", cfg.serviceName, "tls", err)
				os.Exit(1)
			}
			opts = append(opts, grpc.WithTransportCredentials(tpc))
		}
	} else {
		opts = append(opts, grpc.WithInsecure())
		level.Info(logger).Log("serviceName", cfg.serviceName, "gRPC", "communication is not encrypted")
	}

	conn, err := grpc.Dial(cfg.addSvceURL, opts...)
	if err != nil {
		level.Error(logger).Log("serviceName", cfg.serviceName, "connect", cfg.addSvceURL, "error", err)
		os.Exit(1)
	}

	return conn
}

func startHTTPServer(cfg config, httpHandler http.Handler, logger log.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", cfg.httpPort)
	if cfg.serverCert != "" || cfg.serverKey != "" {
		level.Info(logger).Log("serviceName", cfg.serviceName, "protocol", "HTTP", "exposed", cfg.httpPort, "certFile", cfg.serverCert, "keyFile", cfg.serverKey)
		errs <- http.ListenAndServeTLS(p, cfg.serverCert, cfg.serverKey, httpHandler)
	} else {
		level.Info(logger).Log("serviceName", cfg.serviceName, "protocol", "HTTP", "exposed", cfg.httpPort)
		errs <- http.ListenAndServe(p, httpHandler)
	}
}

func startGRPCServer(cfg config, registar sd.Registrar, grpcServer pb.FoosvcServer, logger log.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", cfg.grpcPort)
	listener, err := net.Listen("tcp", p)
	if err != nil {
		level.Error(logger).Log("serviceName", cfg.serviceName, "protocol", "GRPC", "listen", cfg.grpcPort, "err", err)
		os.Exit(1)
	}

	var server *grpc.Server
	if cfg.serverCert != "" || cfg.serverKey != "" {
		creds, err := credentials.NewServerTLSFromFile(cfg.serverCert, cfg.serverKey)
		if err != nil {
			level.Error(logger).Log("serviceName", cfg.serviceName, "certificates", creds, "err", err)
			os.Exit(1)
		}
		level.Info(logger).Log("serviceName", cfg.serviceName, "protocol", "GRPC", "exposed", cfg.grpcPort, "certFile", cfg.serverCert, "keyFile", cfg.serverKey)
		server = grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor), grpc.Creds(creds))
	} else {
		level.Info(logger).Log("serviceName", cfg.serviceName, "protocol", "GRPC", "exposed", cfg.grpcPort)
		server = grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor))
	}
	pb.RegisterFoosvcServer(server, grpcServer)
	grpc_health_v1.RegisterHealthServer(server, &service.HealthImpl{})
	registar.Register()
	errs <- server.Serve(listener)
}
