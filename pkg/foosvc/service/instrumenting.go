package service

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/metrics"
)

//
type instrumentingMiddleware struct {
	requestCount   metrics.Counter   `json:"request_count"`
	requestLatency metrics.Histogram `json:"request_latency"`
	next           FoosvcService     `json:"next"`
}

// InstrumentingMiddleware returns a service middleware that instruments
// the number of integers summed and characters concatenated over the lifetime of
// the service.
func InstrumentingMiddleware(requestCount metrics.Counter, requestLatency metrics.Histogram) Middleware {
	return func(next FoosvcService) FoosvcService {
		return instrumentingMiddleware{
			requestCount:   requestCount,
			requestLatency: requestLatency,
			next:           next,
		}
	}
}

func (im instrumentingMiddleware) Foo(ctx context.Context, s string) (res string, err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "Foo", "error", fmt.Sprint(err != nil)}
		im.requestCount.With(lvs...).Add(1)
		im.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
	}(time.Now())
	return im.next.Foo(ctx, s)
}
