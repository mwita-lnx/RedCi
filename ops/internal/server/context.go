package server

import (
	"context"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

type ctxKey int

const (
	ctxKeyUser ctxKey = iota
	ctxKeyRequestID
)

// withUser stores the authenticated user on the request context.
func withUser(ctx context.Context, u store.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, u)
}

// userFrom returns the authenticated user, if any.
func userFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(store.User)
	return u, ok
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}
