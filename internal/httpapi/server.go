package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-gonic/gin"
)

// ServerController is an HTTP API controller for server resources.
type ServerController struct {
	repository *srvrepo.ServerRepository
}

// NewServerController constructs a new Controller for controlling
// server resources.
func NewServerController(repository *srvrepo.ServerRepository) *ServerController {
	return &ServerController{
		repository: repository,
	}
}

// HandleList is a gin HTTP handler that returns a list of the registered
// servers in the response body.
func (c *ServerController) HandleList(ctx *gin.Context) {
	serverList := c.repository.List()

	// Send server list to client
	ctx.JSON(http.StatusOK, serverList)
}

// HandleRegister is a gin HTTP handler that allows servers to update
// their registration to keep things fresh
func (c *ServerController) HandleUpdate(ctx *gin.Context) {
	requestAddr, _ := srvrepo.ParseServerAddress(ctx.Request.RemoteAddr)
	var serverData srvrepo.Server

	body, _ := ioutil.ReadAll(ctx.Request.Body)
	if err := json.Unmarshal(body, &serverData); err != nil {
		glog.Error("Server Update: invalid request JSON")
		ctx.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	serverAddr, err := srvrepo.ParseServerAddress(ctx.Param("server_id"))
	if err != nil {
		glog.Error("Server Update: invalid server ID")
		// 404, since the ID is a URL param
		ctx.JSON(http.StatusBadRequest, gin.H{"result": "invalid server ID"})
		return
	}

	/*
		Don't check to see if they existed already, just note whether or not they exist.
		we need to handle the case where they've registered but the repo restarted for some reason.
	*/
	existed := c.repository.Has(srvrepo.ServerID(serverAddr.String()))

	// Make sure that the provided address is what's set in the data, so that
	// the server data and ID match.
	serverData.ServerAddress = serverAddr

	// Update the last-seen value to "now"
	serverData.Seen()

	if err := serverData.Validate(); err != nil {
		glog.Error("error during input validation: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
		return
	}

	if !serverData.IP.Equal(requestAddr.IP) {
		glog.Info("Server Update: request IP address does not match client IP address")
		err := fmt.Errorf("request IP address does not match client IP address")

		glog.Error("error during request validation: %v\n", err)
		ctx.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	glog.Infof("A server is attempting update: %s:%d", serverData.IP, serverData.Port)

	existed, err = c.repository.Register(serverData)
	if err != nil {
		glog.Errorf("error registering server: %v\n", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"result": "internal server error"})
	} else if existed {
		glog.Infof("This server updated: %s:%d", serverData.IP, serverData.Port)
		ctx.JSON(http.StatusAccepted, gin.H{"result": "updated"})
	} else {
		glog.Info("New server registered via update: %s:%d", serverData.IP, serverData.Port)
		ctx.JSON(http.StatusCreated, gin.H{"result": "registered"})
	}
}

// HandleRegister is a gin HTTP handler that allows servers to register
// themselves in the repository. This call will also dial back to the port
// being registered and confirm that the port is accessible.
func (c *ServerController) HandleRegister(ctx *gin.Context) {
	serverAddr, err := srvrepo.ParseServerAddress(ctx.Param("server_id"))
	if err != nil {
		// 404, since the ID is a URL param
		ctx.JSON(http.StatusNotAcceptable, gin.H{"result": "invalid server ID"})
		return
	}

	glog.Infof("A server is attempting registration: %s:%d", serverAddr.IP, serverAddr.Port)

	destinationAddress, _ := net.ResolveUDPAddr("udp", serverAddr.String())
	connection, err := net.DialUDP("udp", nil, destinationAddress)
	if err != nil {
		glog.Fatal(err)
		ctx.JSON(http.StatusPreconditionFailed, gin.H{"result": "Repository could not ping you."})
	}
	defer connection.Close()

	err = connection.SetReadDeadline(time.Now().Add(time.Second * 5))
	if err != nil {
		glog.Error("Error SetReadDeadline")
	}

	glog.Info("Pinging new server...")

	// We're sending 10 of these because of UDP
	// Only one actually needs to be received
	var buffer bytes.Buffer
	buffer.WriteString("ping")
	for ii := 0; ii < 10; ii++ {
		connection.Write(buffer.Bytes())
	}

	glog.Info("Waiting for reponse...")

	// Wait and read out the response from the game server
	readBuff := make([]byte, 8)
	_, err = bufio.NewReader(connection).Read(readBuff)

	if err != nil {
		ctx.JSON(http.StatusGatewayTimeout, gin.H{"result": "no ping response received, is your port not properly forwarded?"})
		return
	}
	response := string(readBuff[0:4])

	glog.Infof("Response received: '%s'", response)

	// If the response is all good, handle the registration
	if response == "pong" {
		var serverData srvrepo.Server
		body, _ := ioutil.ReadAll(ctx.Request.Body)
		if err := json.Unmarshal(body, &serverData); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
		}

		// Make sure that the provided address is what's set in the data, so that
		// the server data and ID match.
		serverData.ServerAddress = serverAddr

		// Update the last-seen value to "now"
		serverData.Seen()

		if err := serverData.Validate(); err != nil {
			glog.Errorf("error during input validation: %v\n", err)
			ctx.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
			return
		}

		requestAddr, _ := srvrepo.ParseServerAddress(ctx.Request.RemoteAddr)
		if !serverData.IP.Equal(requestAddr.IP) {
			err := fmt.Errorf("request IP address does not match client IP address")

			glog.Errorf("error during request validation: %v\n", err)
			ctx.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
			return
		}

		_, err = c.repository.Register(serverData)
		if err != nil {
			glog.Errorf("error registering server: %v\n", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"result": "internal server error"})
		} else {
			ctx.JSON(http.StatusOK, gin.H{"result": "registration complete"})
		}
	} else {
		glog.Errorf("error registering server, bad ping response: %s\n", response)
		ctx.JSON(http.StatusNotAcceptable, gin.H{"result": "Bad ping response"})
	}
}

// HandleRemove is a gin HTTP handler that allows servers to remove themselves
// from the repository.
func (c *ServerController) HandleRemove(ctx *gin.Context) {
	requestAddr, _ := srvrepo.ParseServerAddress(ctx.Request.RemoteAddr)

	serverAddr, err := srvrepo.ParseServerAddress(ctx.Param("server_id"))
	if err != nil {
		glog.Errorf("Invalid server ID: %v", err)
		// 404, since the ID is a URL param
		ctx.JSON(http.StatusNotFound, gin.H{"result": "invalid server ID"})
		return
	}

	if !serverAddr.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		glog.Errorf("error during request validation: %v", err)
		ctx.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	glog.Info("A server is being removed.")

	exists := c.repository.Remove(srvrepo.ServerID(serverAddr.String()))

	if !exists {
		glog.Warning("The server was not found.")
		ctx.JSON(http.StatusNotFound, gin.H{"result": "failure"})
		return
	}

	glog.Infof("This server is being removed: %s", serverAddr.String())
	ctx.JSON(200, gin.H{"result": "success"})
}
