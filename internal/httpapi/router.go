package httpapi

import (
	"net"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// trustedProxies are the peers whose forwarding headers are believed.
//
// This is a security boundary, not a convenience setting. Every mutating
// endpoint authenticates the caller by source IP alone, so anyone whose
// X-Forwarded-For is trusted can claim to be any game server and hijack or
// deregister its listing. gin trusts every proxy by default, so this list must
// stay narrow: only a reverse proxy on this host.
var trustedProxies = []string{"127.0.0.1", "::1"}

// NewRouter builds the application's router. main and the tests both use it,
// so the route table and the trusted-proxy configuration cannot drift apart.
func NewRouter(repository *srvrepo.ServerRepository) (*gin.Engine, error) {
	router := gin.New()

	if err := router.SetTrustedProxies(trustedProxies); err != nil {
		return nil, err
	}

	router.Use(Logger())
	router.Use(gin.Recovery())
	router.Use(gzip.Gzip(gzip.DefaultCompression))

	controller := NewServerController(repository)

	router.GET("/reflection/ip", HandleGetIP)
	router.GET("/servers", controller.HandleList)
	router.POST("/servers/:server_id", controller.HandleRegister)
	router.PUT("/servers/:server_id", controller.HandleUpdate)
	router.DELETE("/servers/:server_id", controller.HandleRemove)

	return router, nil
}

// clientIP returns the address a request came from, reading a forwarding
// header only when the immediate peer is one of trustedProxies and falling
// back to the connection's own address otherwise.
//
// It returns nil when the address cannot be determined, which fails the
// source-IP checks closed rather than open.
func clientIP(ctx *gin.Context) net.IP {
	return net.ParseIP(ctx.ClientIP())
}
