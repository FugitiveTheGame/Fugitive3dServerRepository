package httpapi

import (
	"net/http"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// Route defines the structure of an HTTP route that the Router can route
// requests to.
type Route struct {
	Method  string
	Path    string
	Handler http.Handler
}

// Controller defines an interface for controllers of different routes.
type Controller interface {
	Routes() []Route
}

// Router is an HTTP API request router (mulitplexer).
type Router struct {
	mux *gin.Engine
}

// NewRouter creates a new Router with and returns its pointer.
func NewRouter() *Router {
	// TODO: We likely will want to have a `NewRouter` and `NewDefaultRouter` or
	// something along those lines in the future, for better DI and testing.
	mux := gin.Default()
	mux.Use(gzip.Gzip(gzip.DefaultCompression))
	mux.Use(bindMetaInContext())

	return &Router{
		mux: mux,
	}
}

// ServeHTTP satifies the net/http.Handler interface and routes the incoming
// requests to the registered route, if any exist.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Route registers a route to our router.
func (r *Router) Route(route Route) {
	// Wrap the standard http.Handler as a gin.HandlerFunc, for compatibility
	// with the gin routing, to allow for our handlers to be written
	// agnostically of the underyling router/framework (and therefore being more
	// compatible with the rest of the standard library).
	ginHandler := gin.WrapH(route.Handler)

	r.mux.Handle(route.Method, route.Path, ginHandler)
}

// Control registers a controller to our router.
func (r *Router) Control(controller Controller) {
	for _, route := range controller.Routes() {
		r.Route(route)
	}
}

// bindMetaInContext returns a gin middleware that binds the Meta value into the
// request context, so later handlers can access the Meta value.
func bindMetaInContext() gin.HandlerFunc {
	return func(ginCtx *gin.Context) {
		req := ginCtx.Request
		meta := &Meta{ginCtx}
		ctx := NewContextWithMeta(req.Context(), meta)

		// Update the request with a request containing the new context
		*req = *req.WithContext(ctx)

		ginCtx.Next()
	}
}
