package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net"
	"net/http"
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
		ctx.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	serverAddr, err := srvrepo.ParseServerAddress(ctx.Param("server_id"))
	if err != nil {
		// 404, since the ID is a URL param
		ctx.JSON(http.StatusBadRequest, gin.H{"result": "invalid server ID"})
		return
	}

	// If we have never seen this server, require that they POST first
	existed := c.repository.Has(srvrepo.ServerID(serverAddr.String()))
	if !existed {
		ctx.JSON(http.StatusPreconditionRequired, gin.H{"result": "must POST first"})
		return
	}

	// Make sure that the provided address is what's set in the data, so that
	// the server data and ID match.
	serverData.ServerAddress = serverAddr

	// Update the last-seen value to "now"
	serverData.Seen()

	if err := serverData.Validate(); err != nil {
		fmt.Printf("error during input validation: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
		return
	}

	if !serverData.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		fmt.Printf("error during request validation: %v\n", err)
		ctx.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	fmt.Println("A server is registering.")

	existed, err = c.repository.Register(serverData)
	if err != nil {
		fmt.Printf("error registering server: %v\n", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"result": "internal server error"})
		return
	}

	if existed {
		fmt.Println("This server is already registered.")
		ctx.JSON(http.StatusOK, gin.H{"result": "updated"})
		return
	}

	fmt.Println("New server registered!")
	ctx.JSON(http.StatusCreated, gin.H{"result": "registered"})
}

// HandleRegister is a gin HTTP handler that allows servers to register
// themselves in the repository. This call will also dial back to the port
// being registered and confirm that the port is accessible.
func (c *ServerController) HandleRegister(ctx *gin.Context) {
	serverAddr, err := srvrepo.ParseServerAddress(ctx.Param("server_id"))
	if err != nil {
		// 404, since the ID is a URL param
		ctx.JSON(http.StatusNotFound, gin.H{"result": "invalid server ID"})
		return
	}

	// If we have already seen this server, just update it
	existed := c.repository.Has(srvrepo.ServerID(serverAddr.String()))
	if existed {
		c.HandleUpdate(ctx)
		return
	}

	destinationAddress, _ := net.ResolveUDPAddr("udp", serverAddr.String())
	connection, err := net.DialUDP("udp", nil, destinationAddress)
	defer connection.Close()
	if err != nil {
		log.Fatal(err)
		ctx.JSON(http.StatusPreconditionFailed, gin.H{"result": "Repository could not ping you."})
	}

	// We're sending 10 of these because of UDP
	// Only one actually needs to be received
	var buffer bytes.Buffer
	buffer.WriteString("ping")
	for ii := 0; ii < 10; ii++ {
		connection.Write(buffer.Bytes())
	}

	// Wait and read out the response from the game server
	readBuff :=  make([]byte, 8)
	_, err = bufio.NewReader(connection).Read(readBuff)
	response := string(readBuff[0:4])

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
			fmt.Printf("error during input validation: %v\n", err)
			ctx.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
			return
		}

		requestAddr, _ := srvrepo.ParseServerAddress(ctx.Request.RemoteAddr)
		if !serverData.IP.Equal(requestAddr.IP) {
			err := fmt.Errorf("request IP address does not match client IP address")

			fmt.Printf("error during request validation: %v\n", err)
			ctx.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
			return
		}

		existed, err = c.repository.Register(serverData)
		if err != nil {
			fmt.Printf("error registering server: %v\n", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"result": "internal server error"})
			return
		}

	} else {
		fmt.Printf("error registering server, bad ping response: %s\n", response)
		ctx.JSON(http.StatusNotAcceptable, gin.H{"result": "Bad ping response"})
	}
}

// HandleRemove is a gin HTTP handler that allows servers to remove themselves
// from the repository.
func (c *ServerController) HandleRemove(ctx *gin.Context) {
	requestAddr, _ := srvrepo.ParseServerAddress(ctx.Request.RemoteAddr)

	serverAddr, err := srvrepo.ParseServerAddress(ctx.Param("server_id"))
	if err != nil {
		// 404, since the ID is a URL param
		ctx.JSON(http.StatusNotFound, gin.H{"result": "invalid server ID"})
		return
	}

	if !serverAddr.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		fmt.Printf("error during request validation: %v\n", err)
		ctx.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	fmt.Println("A server is being removed.")

	exists := c.repository.Remove(srvrepo.ServerID(serverAddr.String()))

	if !exists {
		fmt.Println("The server was not found.")
		ctx.JSON(http.StatusNotFound, gin.H{"result": "failure"})
		return
	}

	fmt.Println("This server is being removed.")
	ctx.JSON(200, gin.H{"result": "success"})
}
