package auth

import (
	"context"

	"github.com/haasonsaas/nexus/pkg/models"
)

type userContextKey struct{}

// WithUser attaches a user to the context.
func WithUser(ctx context.Context, user *models.User) context.Context {
	if user == nil {
		return ctx
	}
	return context.WithValue(ctx, userContextKey{}, user)
}

// UserFromContext retrieves a user from the context.
func UserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(userContextKey{}).(*models.User)
	return user, ok
}
