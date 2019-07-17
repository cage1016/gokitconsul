package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/lightstep/lightstep-tracer-go"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkinot "github.com/openzipkin-contrib/zipkin-go-opentracing"
	"github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	"github.com/cage1016/gokitconsul/pkg/foosvc/service"
	foosvctransports "github.com/cage1016/gokitconsul/pkg/foosvc/transports"
	"github.com/cage1016/gokitconsul/pkg/shared_package/grpclb"
)

func main() {
	fs := flag.NewFlagSet("foocli", flag.ExitOnError)
	var (
		//nameSpace      = fs.String("name-space", "gokitconsul", "")
		serviceName    = fs.String("service-name", "foo-cli", "")
		caCerts        = fs.String("ca-certs", "/Users/cage/qnap/quai/gokitconsul/deployments/docker/ssl/localhost+3.pem", "tls based credential")
		httpAddr       = fs.String("http-addr", "", "HTTP address of foosvc")
		grpcAddr       = fs.String("grpc-addr", "", "gRPC address of foosvc")
		consulHost     = fs.String("consul-host", "", "")
		consultPort    = fs.Int("consult-port", 0, "")
		zipkinV2URL    = fs.String("zipkin-v2-url", "", "")
		zipkinV1URL    = fs.String("zipkin-v1-url", "", "Enable Zipkin v1 tracing (zipkin-go-opentracing) via a collector URL e.g. http://localhost:9411/api/v1/spans")
		lightstepToken = fs.String("lightstep-token", "", "Enable LightStep tracing via a LightStep access token")
		appdashAddr    = fs.String("appdash-addr", "", "Enable Appdash tracing via an Appdash server host:port")
	)
	fs.Usage = usageFor(fs, os.Args[0]+" [flags] <s>")
	fs.Parse(os.Args[1:])
	if len(fs.Args()) != 1 {
		fs.Usage()
		os.Exit(1)
	}

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = level.NewFilter(logger, level.AllowInfo())
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	var tracer stdopentracing.Tracer
	{
		if *zipkinV1URL != "" && *zipkinV2URL == "" {
			logger.Log("tracer", "Zipkin", "type", "OpenTracing", "URL", *zipkinV1URL)
			collector, err := zipkinot.NewHTTPCollector(*zipkinV1URL)
			if err != nil {
				logger.Log("err", err)
				os.Exit(1)
			}
			defer collector.Close()
			var (
				debug       = false
				hostPort    = "localhost:0"
				serviceName = *serviceName
			)
			recorder := zipkinot.NewRecorder(collector, debug, hostPort, serviceName)
			tracer, err = zipkinot.NewTracer(recorder)
			if err != nil {
				logger.Log("err", err)
				os.Exit(1)
			}
		} else if *lightstepToken != "" {
			logger.Log("tracer", "LightStep")
			tracer = lightstep.NewTracer(lightstep.Options{AccessToken: *lightstepToken})
			defer lightstep.FlushLightStepTracer(tracer)
		} else if *appdashAddr != "" {
			logger.Log("tracer", "Appdash", "addr", *appdashAddr)
			tracer = appdashot.NewTracer(appdash.NewRemoteCollector(*appdashAddr))
		} else {
			tracer = stdopentracing.GlobalTracer()
		}
	}

	var zipkinTracer *zipkin.Tracer
	{
		var (
			err           error
			hostPort      = "" // if host:port is unknown we can keep this empty
			serviceName   = *serviceName
			useNoopTracer = (*zipkinV2URL == "")
			reporter      = zipkinhttp.NewReporter(*zipkinV2URL)
		)
		zEP, _ := zipkin.NewEndpoint(serviceName, hostPort)
		zipkinTracer, err = zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(zEP), zipkin.WithNoopTracer(useNoopTracer))
		if err != nil {
			logger.Log("err", err)
			os.Exit(1)
		}
		if !useNoopTracer {
			logger.Log("tracer", "Zipkin", "type", "Native", "URL", *zipkinV2URL)
		}
	}

	var (
		svc service.FoosvcService
		err error
	)
	if *httpAddr != "" {
		svc, err = foosvctransports.NewHTTPClient(*httpAddr, tracer, zipkinTracer, log.NewNopLogger())
	} else if *grpcAddr != "" {
		var conn *grpc.ClientConn
		if *consulHost != "" && *consultPort != 0 {
			conn, err = grpc.Dial(
				"",
				grpc.WithInsecure(),
				grpc.WithUnaryInterceptor(
					grpc_retry.UnaryClientInterceptor(
						grpc_retry.WithBackoff(grpc_retry.BackoffLinear(time.Duration(1)*time.Millisecond)),
						grpc_retry.WithMax(3),
						grpc_retry.WithPerRetryTimeout(time.Duration(5)*time.Millisecond),
						grpc_retry.WithCodes(codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded),
					),
				),
				grpc.WithBalancer(grpc.RoundRobin(grpclb.NewConsulResolver(
					fmt.Sprintf("%v:%d", *consulHost, *consultPort), "grpc.health.v1.foosvc",
				))),
			)
			if err != nil {
				level.Error(logger).Log("serviceName", "grpc.health.v1.foosvc", "error", err)
				os.Exit(1)
			}
		} else {
			if *caCerts != "" {
				creds, err := credentials.NewClientTLSFromFile(*caCerts, "")
				if err != nil {
					fmt.Sprintf("failed to load credentials: %v", err)
				}
				conn, err = grpc.Dial(*grpcAddr, grpc.WithTransportCredentials(creds), grpc.WithTimeout(time.Second))
			} else {
				conn, err = grpc.Dial(*grpcAddr, grpc.WithInsecure(), grpc.WithTimeout(time.Second))
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v", err)
				os.Exit(1)
			}
			defer conn.Close()
		}
		svc = foosvctransports.NewGRPCClient(conn, tracer, zipkinTracer, logger)
	}

	s := fs.Args()[0]
	res, err := svc.Foo(context.Background(), s)
	if err != nil {
		st, ok := status.FromError(err)
		if !ok {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprintf(os.Stdout, "Foo %s = %s\n", s, res)
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", st.Message())
		}

		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "Foo %s = %s\n", s, res)
}

func usageFor(fs *flag.FlagSet, short string) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "USAGE\n")
		fmt.Fprintf(os.Stderr, "  %s\n", short)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "FLAGS\n")
		w := tabwriter.NewWriter(os.Stderr, 0, 2, 2, ' ', 0)
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(w, "\t-%s %s\t%s\n", f.Name, f.DefValue, f.Usage)
		})
		w.Flush()
		fmt.Fprintf(os.Stderr, "\n")
	}
}
