package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"github.com/go-kit/kit/metrics"
)

//
type instrumentingMiddleware struct {
	requestCount   metrics.Counter   `json:"request_count"`
	requestLatency metrics.Histogram `json:"request_latency"`
	next           AuthnService      `json:"next"`
}

// InstrumentingMiddleware returns a service middleware that instruments
// the number of integers summed and characters concatenated over the lifetime of
// the service.
func InstrumentingMiddleware(requestCount metrics.Counter, requestLatency metrics.Histogram) Middleware {
	return func(next AuthnService) AuthnService {
		return instrumentingMiddleware{
			requestCount:   requestCount,
			requestLatency: requestLatency,
			next:           next,
		}
	}
}

func (im instrumentingMiddleware) Login(ctx context.Context, user model.User) (token string, err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "Login", "error", fmt.Sprint(err != nil)}
		im.requestCount.With(lvs...).Add(1)
		im.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
	}(time.Now())
	return im.next.Login(ctx, user)
}

func (im instrumentingMiddleware) Logout(ctx context.Context, user model.User) (err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "Logout", "error", fmt.Sprint(err != nil)}
		im.requestCount.With(lvs...).Add(1)
		im.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
	}(time.Now())
	return im.next.Logout(ctx, user)
}

func (im instrumentingMiddleware) Add(ctx context.Context, user model.User) (err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "Add", "error", fmt.Sprint(err != nil)}
		im.requestCount.With(lvs...).Add(1)
		im.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
	}(time.Now())
	return im.next.Add(ctx, user)
}

func (im instrumentingMiddleware) BatchAdd(ctx context.Context, users []model.User) (err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "BatchAdd", "error", fmt.Sprint(err != nil)}
		im.requestCount.With(lvs...).Add(1)
		im.requestLatency.With(lvs...).Observe(time.Since(begin).Seconds())
	}(time.Now())
	return im.next.BatchAdd(ctx, users)
}
