package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
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
	"google.golang.org/grpc/status"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	"github.com/cage1016/gokitconsul/pkg/addsvc/service"
	addsvctransports "github.com/cage1016/gokitconsul/pkg/addsvc/transports"
	"github.com/cage1016/gokitconsul/pkg/shared_package/grpclb"
)

func main() {
	fs := flag.NewFlagSet("addcli", flag.ExitOnError)
	var (
		//nameSpace      = fs.String("name-space", "gokitconsul", "")
		serviceName    = fs.String("service-name", "foo-cli", "")
		httpAddr       = fs.String("http-addr", "", "HTTP address of addsvc")
		grpcAddr       = fs.String("grpc-addr", "", "gRPC address of addsvc")
		consulHost     = fs.String("consul-host", "", "")
		consultPort    = fs.Int("consult-port", 0, "")
		zipkinV2URL    = fs.String("zipkin-v2-url", "", "")
		zipkinV1URL    = fs.String("zipkin-v1-url", "", "Enable Zipkin v1 tracing (zipkin-go-opentracing) via a collector URL e.g. http://localhost:9411/api/v1/spans")
		lightstepToken = fs.String("lightstep-token", "", "Enable LightStep tracing via a LightStep access token")
		appdashAddr    = fs.String("appdash-addr", "", "Enable Appdash tracing via an Appdash server host:port")
		method         = fs.String("method", "sum", "sum, concat")
	)
	fs.Usage = usageFor(fs, os.Args[0]+" [flags] <a> <b>")
	fs.Parse(os.Args[1:])
	if len(fs.Args()) != 2 {
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
		svc service.AddsvcService
		err error
	)
	if *httpAddr != "" {
		svc, err = addsvctransports.NewHTTPClient(*httpAddr, tracer, zipkinTracer, log.NewNopLogger())
	} else if *grpcAddr != "" {
		var conn *grpc.ClientConn
		if *consulHost != "" && *consultPort != 0 {
			conn, err = grpc.Dial(
				"",
				grpc.WithInsecure(),
				// 开启 grpc 中间件的重试功能
				grpc.WithUnaryInterceptor(
					grpc_retry.UnaryClientInterceptor(
						grpc_retry.WithBackoff(grpc_retry.BackoffLinear(time.Duration(1)*time.Millisecond)),      // 重试间隔时间
						grpc_retry.WithMax(3),                                                                    // 重试次数
						grpc_retry.WithPerRetryTimeout(time.Duration(5)*time.Millisecond),                        // 重试时间
						grpc_retry.WithCodes(codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded), // 返回码为如下值时重试
					),
				),
				// 负载均衡，使用 consul 作服务发现
				grpc.WithBalancer(grpc.RoundRobin(grpclb.NewConsulResolver(
					fmt.Sprintf("%v:%d", *consulHost, *consultPort), "grpc.health.v1.addsvc",
				))),
			)
			if err != nil {
				level.Error(logger).Log("serviceName", "grpc.health.v1.addsvc", "error", err)
				os.Exit(1)
			}
		} else {
			conn, err = grpc.Dial(*grpcAddr, grpc.WithInsecure(), grpc.WithTimeout(time.Second))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v", err)
				os.Exit(1)
			}
			defer conn.Close()
		}
		svc = addsvctransports.NewGRPCClient(conn, tracer, zipkinTracer, logger)
	}

	switch *method {
	case "sum":
		a, _ := strconv.ParseInt(fs.Args()[0], 10, 64)
		b, _ := strconv.ParseInt(fs.Args()[1], 10, 64)
		v, err := svc.Sum(context.Background(), a, b)
		if err != nil {
			st, ok := status.FromError(err)
			if !ok {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				fmt.Fprintf(os.Stdout, "%d + %d = %d\n", a, b, v)
			} else {
				fmt.Fprintf(os.Stderr, "error: %v\n", st.Message())
			}
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%d + %d = %d\n", a, b, v)
	case "concat":
		a := fs.Args()[0]
		b := fs.Args()[1]
		v, err := svc.Concat(context.Background(), a, b)
		if err != nil {
			st, ok := status.FromError(err)
			if !ok {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				fmt.Fprintf(os.Stdout, "%q + %q = %q\n", a, b, v)
			} else {
				fmt.Fprintf(os.Stderr, "error: %v\n", st.Message())
			}
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%q + %q = %q\n", a, b, v)
	default:
		fmt.Fprintf(os.Stderr, "error: invalid method %q\n", *method)
		os.Exit(1)
	}
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
