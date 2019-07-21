package service

import (
	"context"
	"time"

	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

type loggingMiddleware struct {
	logger log.Logger   `json:""`
	next   AuthnService `json:""`
}

// LoggingMiddleware takes a logger as a dependency
// and returns a ServiceMiddleware.
func LoggingMiddleware(logger log.Logger) Middleware {
	return func(next AuthnService) AuthnService {
		return loggingMiddleware{level.Info(logger), next}
	}
}

func (lm loggingMiddleware) Login(ctx context.Context, user model.User) (token string, err error) {
	defer func(begin time.Time) {
		lm.logger.Log("method", "Login", "user", user, "err", err)
	}(time.Now())

	return lm.next.Login(ctx, user)
}

func (lm loggingMiddleware) Logout(ctx context.Context, user model.User) (err error) {
	defer func(begin time.Time) {
		lm.logger.Log("method", "Logout", "user", user, "err", err)
	}(time.Now())

	return lm.next.Logout(ctx, user)
}

func (lm loggingMiddleware) Add(ctx context.Context, user model.User) (err error) {
	defer func(begin time.Time) {
		lm.logger.Log("method", "Add", "user", user, "err", err)
	}(time.Now())

	return lm.next.Add(ctx, user)
}

func (lm loggingMiddleware) BatchAdd(ctx context.Context, users []model.User) (err error) {
	defer func(begin time.Time) {
		lm.logger.Log("method", "BatchAdd", "users", users, "err", err)
	}(time.Now())

	return lm.next.BatchAdd(ctx, users)
}
