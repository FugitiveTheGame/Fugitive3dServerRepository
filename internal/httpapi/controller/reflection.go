package controller

import (
	"fmt"
	"net"
	"net/http"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/internal/httpapi"
	"github.com/gin-gonic/gin"
)

// reflectionController is an HTTP API controller for reflective endpoints.
type reflectionController struct {
}

// NewReflectionController constructs a new httpapi.Controller for controlling
// reflective endpoints.
func NewReflectionController() httpapi.Controller {
	return &reflectionController{}
}

// Routes satisfies the httpapi.Controller interface and returns the routes that
// this controller controls.
func (c *reflectionController) Routes() []httpapi.Route {
	return []httpapi.Route{
		httpapi.Route{http.MethodGet, "/getip", http.HandlerFunc(c.handleGetIP)},
	}
}

func (c *reflectionController) handleGetIP(w http.ResponseWriter, r *http.Request) {
	meta, _ := httpapi.MetaFromContext(r.Context())

	ip, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		fmt.Println(err.Error())
		meta.JSON(500, gin.H{"result": "internal server error"})
		return
	}

	fmt.Println("Incoming request /getip:", ip+":"+port)
	// Only return the IP, even though we have their source ephemeral port.
	meta.JSON(200, gin.H{"ip": ip})
}
