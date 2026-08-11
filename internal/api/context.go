package api

import (
	"context"

	"github.com/patrickrho-patty/pccp/internal/identity"
)

type contextKey string

const claimsKey contextKey = "claims"

func ctxWithClaims(ctx context.Context, claims *identity.Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

func claimsFromCtx(ctx context.Context) (*identity.Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*identity.Claims)
	return claims, ok
}
