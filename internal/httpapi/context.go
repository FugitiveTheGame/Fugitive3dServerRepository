package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"
)

// The following context types and functions are implemented based on the
// instructions of the `context.Context` type documentation.

// contextKey is an unexported type for context keys defined in this package.
// This prevents collisions with keys defined in other packages.
type contextKey string

// Context keys
const (
	metaContextKey contextKey = "meta"
)

// Meta defines HTTP API meta values that are passed to routed requests via
// the request's context.
type Meta struct {
	// Embed the gin context so that we can use its methods.
	//
	// TODO: Move away from using Gin's context. Its a flawed implementation and
	// has numerous issues when used outside of a request scope...
	//
	// See: https://github.com/gin-gonic/gin/pull/2029
	*gin.Context
}

// NewContextWithMeta returns a new context.Context with a Meta value.
func NewContextWithMeta(ctx context.Context, meta *Meta) context.Context {
	return context.WithValue(ctx, metaContextKey, meta)
}

// MetaFromContext takes a context.Context and returns the stored Meta value, if
// any exists. If none exists, the returned bool value will be false.
func MetaFromContext(ctx context.Context) (*Meta, bool) {
	meta, ok := ctx.Value(metaContextKey).(*Meta)

	return meta, ok
}
