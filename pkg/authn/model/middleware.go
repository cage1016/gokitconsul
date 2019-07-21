package model

import (
	"context"
	"fmt"
	"github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/model"
	"time"
)

//var _ UserRepository = (*userRepositoryMiddleware)(nil)

const (
	saveOp         = "save_op"
	retrieveByIDOp = "retrieve_by_id"
)

type userRepositoryMiddleware struct {
	tracer *zipkin.Tracer
	next   UserRepository
}

// UserRepositoryMiddleware tracks request and their latency, and adds spans
// to context.
func UserRepositoryMiddleware(tracer *zipkin.Tracer) Middleware {
	return func(next UserRepository) UserRepository {
		return userRepositoryMiddleware{
			tracer: tracer,
			next:   next,
		}
	}
}

func (urm userRepositoryMiddleware) Save(ctx context.Context, user User) error {
	span := createSpan(ctx, urm.tracer, saveOp)
	defer span.Finish()

	span.Tag("user", fmt.Sprintf("%v", user))
	span.Annotate(time.Now(), "Save:start")
	ctx = zipkin.NewContext(ctx, span)
	err := urm.next.Save(ctx, user)
	if err != nil {
		zipkin.TagError.Set(span, err.Error())
	}
	span.Annotate(time.Now(), "Save:end")
	return err
}

func (urm userRepositoryMiddleware) RetrieveByID(ctx context.Context, id string) (User, error) {
	span := createSpan(ctx, urm.tracer, retrieveByIDOp)
	defer span.Finish()

	span.Tag("id", fmt.Sprintf("%v", id))
	span.Annotate(time.Now(), "RetrieveByID:start")
	ctx = zipkin.NewContext(ctx, span)
	user, err := urm.next.RetrieveByID(ctx, id)
	if err != nil {
		zipkin.TagError.Set(span, err.Error())
	}
	span.Annotate(time.Now(), "RetrieveByID:end")
	return user, err
}

func createSpan(ctx context.Context, tracer *zipkin.Tracer, opName string) zipkin.Span {
	var sc model.SpanContext
	if parentSpan := zipkin.SpanFromContext(ctx); parentSpan != nil {
		sc = parentSpan.Context()
		ep, _ := zipkin.NewEndpoint("postgres", "authn-db:5432")
		return tracer.StartSpan(
			opName,
			zipkin.Parent(sc),
			zipkin.RemoteEndpoint(ep),
		)
	}

	return tracer.StartSpan(opName)
}
