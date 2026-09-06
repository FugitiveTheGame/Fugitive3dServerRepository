package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
)

// HandleGetIP is a gin HTTP handler that gathers the source IP from an
// incoming HTTP request and returns it in the response body. Game servers call
// it to discover the address they should register under, so it has to report
// the caller's own address rather than a reverse proxy's.
func HandleGetIP(ctx *gin.Context) {
	ip := clientIP(ctx)
	if ip == nil {
		glog.Errorf("could not determine client IP from remote address %q", ctx.Request.RemoteAddr)
		ctx.JSON(http.StatusInternalServerError, gin.H{"result": "internal server error"})
		return
	}

	glog.Info("Incoming request /reflection/ip: " + ip.String())
	// Only return the IP, even though we have their source ephemeral port.
	ctx.JSON(http.StatusOK, gin.H{"ip": ip.String()})
}
