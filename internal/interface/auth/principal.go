package auth

import (
	"context"
	"smapass-back/internal/domain/entity"
)

type principalKey struct{}

func WithPrincipal(ctx context.Context, p entity.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func Principal(ctx context.Context) (entity.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(entity.Principal)
	return p, ok
}
