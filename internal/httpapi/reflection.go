package httpapi

import (
	"github.com/golang/glog"
	"net"

	"github.com/gin-gonic/gin"
)

// HandleGetIP is a gin HTTP handler that gather's the source IP from an
// incoming HTTP request and returns it in the response body.
func HandleGetIP(ctx *gin.Context) {
	ip, port, err := net.SplitHostPort(ctx.Request.RemoteAddr)
	if err != nil {
		glog.Error(err.Error())
		ctx.JSON(500, gin.H{"result": "internal server error"})
		return
	}

	glog.Info("Incoming request /getip:" + ip + ":" + port)
	// Only return the IP, even though we have their source ephemeral port.
	ctx.JSON(200, gin.H{"ip": ip})
}
