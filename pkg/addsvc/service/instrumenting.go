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
	next           AddsvcService     `json:"next"`
}

// InstrumentingMiddleware returns a service middleware that instruments
// the number of integers summed and characters concatenated over the lifetime of
// the service.
func InstrumentingMiddleware(requestCount metrics.Counter, requestLatency metrics.Histogram) Middleware {
	return func(next AddsvcService) AddsvcService {
		return instrumentingMiddleware{
			requestCount:   requestCount,
			requestLatency: requestLatency,
			next:           next,
		}
	}
}

func (im instrumentingMiddleware) Sum(ctx context.Context, a int64, b int64) (rs int64, err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "Sum", "error", fmt.Sprint(err != nil)}
		im.requestCount.With(lvs...).Add(1)
		im.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
	}(time.Now())
	return im.next.Sum(ctx, a, b)
}

func (im instrumentingMiddleware) Concat(ctx context.Context, a string, b string) (rs string, err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "Concat", "error", fmt.Sprint(err != nil)}
		im.requestCount.With(lvs...).Add(1)
		im.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
	}(time.Now())
	return im.next.Concat(ctx, a, b)
}
