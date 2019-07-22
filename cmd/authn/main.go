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
	consulsd "github.com/go-kit/kit/sd/consul"
	kitgrpc "github.com/go-kit/kit/transport/grpc"
	"github.com/jmoiron/sqlx"
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

	pb "github.com/cage1016/gokitconsul/pb/authn"
	"github.com/cage1016/gokitconsul/pkg/authn/bcrypt"
	"github.com/cage1016/gokitconsul/pkg/authn/endpoints"
	"github.com/cage1016/gokitconsul/pkg/authn/jwt"
	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"github.com/cage1016/gokitconsul/pkg/authn/postgres"
	"github.com/cage1016/gokitconsul/pkg/authn/service"
	"github.com/cage1016/gokitconsul/pkg/authn/transports"
	"github.com/cage1016/gokitconsul/pkg/shared_package/grpcsr"
)

const (
	defConsulHost     = ""
	defConsulPort     = ""
	defZipkinV1URL    = ""
	defZipkinV2URL    = ""
	defLightstepToken = ""
	defAppdashAddr    = ""
	defNameSpace      = "gokitconsul"
	defServiceName    = "authn"
	defLogLevel       = "error"
	defServiceHost    = "localhost"
	defHTTPPort       = "6020"
	defGRPCPort       = "6021"
	defServerCert     = ""
	defServerKey      = ""
	defClientTLS      = "false"
	defCACerts        = ""
	defDBHost         = "localhost"
	defDBPort         = "5432"
	defDBUser         = "gokitconsul"
	defDBPass         = "gokitconsul"
	defDBName         = "authn"
	defDBSSLMode      = "disable"
	defDBSSLCert      = ""
	defDBSSLKey       = ""
	defDBSSLRootCert  = ""
	defSecret         = "gokitconsul-authn"
	envConsulHost     = "QS_CONSULT_HOST"
	envConsultPort    = "QS_CONSULT_PORT"
	envZipkinV1URL    = "QS_ZIPKIN_V1_URL"
	envZipkinV2URL    = "QS_ZIPKIN_V2_URL"
	envLightstepToken = "QS_LIGHT_STEP_TOKEN"
	envAppdashAddr    = "QS_APPDASH_ADDR"
	envNameSpace      = "QS_AUTHN_NAMESPACE"
	envServiceName    = "QS_AUTHN_SERVICE_NAME"
	envLogLevel       = "QS_AUTHN_LOG_LEVEL"
	envServiceHost    = "QS_AUTHN_SERVICE_HOST"
	envHTTPPort       = "QS_AUTHN_HTTP_PORT"
	envGRPCPort       = "QS_AUTHN_GRPC_PORT"
	envServerCert     = "QS_AUTHN_SERVER_CERT"
	envServerKey      = "QS_AUTHN_SERVER_KEY"
	envClientTLS      = "QS_AUTHN_CLIENT_TLS"
	envCACerts        = "QS_AUTHN_CA_CERTS"
	envDBHost         = "QS_AUTHN_DB_HOST"
	envDBPort         = "QS_AUTHN_DB_PORT"
	envDBUser         = "QS_AUTHN_DB_USER"
	envDBPass         = "QS_AUTHN_DB_PASS"
	envDBName         = "QS_AUTHN_DB"
	envDBSSLMode      = "QS_AUTHN_DB_SSL_MODE"
	envDBSSLCert      = "QS_AUTHN_DB_SSL_CERT"
	envDBSSLKey       = "QS_AUTHN_DB_SSL_KEY"
	envDBSSLRootCert  = "QS_AUTHN_DB_SSL_ROOT_CERT"
	envSecret         = "QS_AUTHN_SECRET"
)

type config struct {
	dbConfig       postgres.Config
	secret         string
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
}

// Env reads specified environment variable. If no value has been found,
// fallback is returned.
func env(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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

	db := connectToDB(cfg.dbConfig, logger)
	defer db.Close()

	{
		svcRegistar, err := initConsul(cfg.nameSpace, cfg.serviceName, cfg.consulHost, cfg.consultPort, cfg.httpPort, cfg.grpcPort, logger)
		if err != nil {
			level.Error(logger).Log(
				"consult", fmt.Sprintf("%s:%s", cfg.consulHost, cfg.consultPort),
				"serviceName", cfg.serviceName,
				"grpcPort", cfg.grpcPort,
				"metricsPort", cfg.httpPort,
				"tags", []string{cfg.nameSpace, cfg.serviceName},
				"err", err,
			)
		} else {
			defer svcRegistar.Deregister()
			svcRegistar.Register()
		}
	}

	tracer := initOpentracing(cfg.serviceName, cfg.httpPort, cfg.zipkinV1URL, cfg.zipkinV2URL, cfg.lightstepToken, cfg.appdashAddr, logger)
	zipkinTracer := initZipkin(cfg.serviceName, cfg.httpPort, cfg.zipkinV2URL, logger)
	duration := prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{Namespace: cfg.nameSpace, Subsystem: cfg.serviceName, Name: "request_duration_ns", Help: "Request duration in nanoseconds."}, []string{"method", "success"})

	service := NewServer(cfg.nameSpace, cfg.serviceName, db, zipkinTracer, cfg.secret, logger)
	endpoints := endpoints.New(service, duration, tracer, zipkinTracer, cfg.secret, logger)
	errs := make(chan error, 2)

	go startHTTPServer(endpoints, tracer, zipkinTracer, cfg.httpPort, cfg.serverCert, cfg.serverKey, logger, errs)
	go startGRPCServer(endpoints, tracer, zipkinTracer, cfg.grpcPort, cfg.serverCert, cfg.serverKey, logger, errs)

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()

	err := <-errs
	level.Info(logger).Log("serviceName", cfg.serviceName, "terminated", err)
}

func loadConfig(logger log.Logger) (cfg config) {
	tls, err := strconv.ParseBool(env(envClientTLS, defClientTLS))
	if err != nil {
		level.Error(logger).Log("envClientTLS", envClientTLS, "error", err)
	}

	dbConfig := postgres.Config{
		Host:        env(envDBHost, defDBHost),
		Port:        env(envDBPort, defDBPort),
		User:        env(envDBUser, defDBUser),
		Pass:        env(envDBPass, defDBPass),
		Name:        env(envDBName, defDBName),
		SSLMode:     env(envDBSSLMode, defDBSSLMode),
		SSLCert:     env(envDBSSLCert, defDBSSLCert),
		SSLKey:      env(envDBSSLKey, defDBSSLKey),
		SSLRootCert: env(envDBSSLRootCert, defDBSSLRootCert),
	}

	cfg.dbConfig = dbConfig
	cfg.secret = env(envSecret, defSecret)
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
	return cfg
}

func initConsul(nameSpace, serviceName, consulHost, consultPort, httpPort, grpcPort string, logger log.Logger) (svcRegistar *consulsd.Registrar, err error) {
	if consulHost != "" && consultPort != "" {
		consulAddres := fmt.Sprintf("%s:%s", consulHost, consultPort)
		grpcPort, _ := strconv.Atoi(grpcPort)
		metricsPort, _ := strconv.Atoi(httpPort)
		consulReg := grpcsr.NewConsulRegister(consulAddres, serviceName, grpcPort, metricsPort, []string{nameSpace, serviceName}, logger)
		svcRegistar, err = consulReg.NewConsulGRPCRegister()
	}
	return
}

func connectToDB(cfg postgres.Config, logger log.Logger) *sqlx.DB {
	db, err := postgres.Connect(cfg)
	if err != nil {
		level.Error(logger).Log(
			"host", cfg.Host,
			"port", cfg.Port,
			"user", cfg.User,
			"dbname", cfg.Name,
			"password", cfg.Pass,
			"sslmode", cfg.SSLMode,
			"SSLCert", cfg.SSLCert,
			"SSLKey", cfg.SSLKey,
			"SSLRootCert", cfg.SSLRootCert,
			"err", err,
		)
		os.Exit(1)
	}
	return db
}

func initOpentracing(serviceName, httpPort, zipkinV1URL, zipkinV2URL, lightstepToken, appdashAddr string, logger log.Logger) (tracer stdopentracing.Tracer) {
	if zipkinV1URL != "" && zipkinV2URL == "" {
		logger.Log("tracer", "Zipkin", "type", "OpenTracing", "URL", zipkinV1URL)
		collector, err := zipkinot.NewHTTPCollector(zipkinV1URL)
		if err != nil {
			logger.Log("err", err)
			os.Exit(1)
		}
		defer collector.Close()
		var (
			debug       = false
			hostPort    = fmt.Sprintf("localhost:%s", httpPort)
			serviceName = serviceName
		)
		recorder := zipkinot.NewRecorder(collector, debug, hostPort, serviceName)
		tracer, err = zipkinot.NewTracer(recorder)
		if err != nil {
			logger.Log("err", err)
			os.Exit(1)
		}
	} else if lightstepToken != "" {
		logger.Log("tracer", "LightStep")
		tracer = lightstep.NewTracer(lightstep.Options{AccessToken: lightstepToken})
		defer lightstep.FlushLightStepTracer(tracer)
	} else if appdashAddr != "" {
		logger.Log("tracer", "Appdash", "addr", appdashAddr)
		tracer = appdashot.NewTracer(appdash.NewRemoteCollector(appdashAddr))
	} else {
		tracer = stdopentracing.GlobalTracer()
	}

	return
}

func initZipkin(serviceName, httpPort, zipkinV2URL string, logger log.Logger) (zipkinTracer *zipkin.Tracer) {
	var (
		err           error
		hostPort      = fmt.Sprintf("localhost:%s", httpPort)
		useNoopTracer = (zipkinV2URL == "")
		reporter      = zipkinhttp.NewReporter(zipkinV2URL)
	)
	zEP, _ := zipkin.NewEndpoint(serviceName, hostPort)
	zipkinTracer, err = zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(zEP), zipkin.WithNoopTracer(useNoopTracer))
	if err != nil {
		logger.Log("err", err)
		os.Exit(1)
	}
	if !useNoopTracer {
		logger.Log("tracer", "Zipkin", "type", "Native", "URL", zipkinV2URL)
	}

	return
}

func NewServer(nameSpace, serviceName string, db *sqlx.DB, zipkinTracer *zipkin.Tracer, secret string, logger log.Logger) service.AuthnService {
	var (
		requestCount   metrics.Counter
		requestLatency metrics.Histogram
		fieldKeys      []string
	)
	{
		fieldKeys = []string{"method", "error"}
		requestCount = prometheus.NewCounterFrom(stdprometheus.CounterOpts{Namespace: nameSpace, Subsystem: serviceName, Name: "request_count", Help: "Number of requests received."}, fieldKeys)
		requestLatency = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{Namespace: nameSpace, Subsystem: serviceName, Name: "request_latency_microseconds", Help: "Total duration of requests in microseconds."}, fieldKeys)
	}

	repo := model.UserRepositoryMiddleware(zipkinTracer, "postgres", "authn-db:5432")(postgres.New(db, logger))
	hasher := bcrypt.New()
	idp := jwt.New(secret)

	svc := service.New(logger, requestCount, requestLatency, repo, hasher, idp)
	return svc
}

func startHTTPServer(endpoints endpoints.Endpoints, tracer stdopentracing.Tracer, zipkinTracer *zipkin.Tracer, port string, certFile string, keyFile string, logger log.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	if certFile != "" || keyFile != "" {
		level.Info(logger).Log("protocol", "HTTP", "exposed", port, "certFile", certFile, "keyFile", keyFile)
		errs <- http.ListenAndServeTLS(p, certFile, keyFile, transports.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger))
	} else {
		level.Info(logger).Log("protocol", "HTTP", "exposed", port)
		errs <- http.ListenAndServe(p, transports.NewHTTPHandler(endpoints, tracer, zipkinTracer, logger))
	}
}

func startGRPCServer(endpoints endpoints.Endpoints, tracer stdopentracing.Tracer, zipkinTracer *zipkin.Tracer, port string, certFile string, keyFile string, logger log.Logger, errs chan error) {
	p := fmt.Sprintf(":%s", port)
	listener, err := net.Listen("tcp", p)
	if err != nil {
		level.Error(logger).Log("protocol", "GRPC", "listen", port, "err", err)
		os.Exit(1)
	}

	var server *grpc.Server
	if certFile != "" || keyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			level.Error(logger).Log("protocol", "GRPC", "certificates", creds, "err", err)
			os.Exit(1)
		}
		level.Info(logger).Log("protocol", "GRPC", "exposed", port, "certFile", certFile, "keyFile", keyFile)
		server = grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor), grpc.Creds(creds))
	} else {
		level.Info(logger).Log("protocol", "GRPC", "protocol", "GRPC", "exposed", port)
		server = grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor))
	}
	pb.RegisterAuthnServer(server, transports.MakeGRPCServer(endpoints, tracer, zipkinTracer, logger))
	grpc_health_v1.RegisterHealthServer(server, &service.HealthImpl{})
	errs <- server.Serve(listener)
}
