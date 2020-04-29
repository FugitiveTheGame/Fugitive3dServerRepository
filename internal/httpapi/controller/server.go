package controller

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/internal/httpapi"
	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-gonic/gin"
)

// serverController is an HTTP API controller for server resources.
type serverController struct {
	repository srvrepo.ServerRepository
}

// NewServerController constructs a new httpapi.Controller for controlling
// server resources.
func NewServerController(repository srvrepo.ServerRepository) httpapi.Controller {
	return &serverController{
		repository: repository,
	}
}

// Routes satisfies the httpapi.Controller interface and returns the routes that
// this controller controls.
func (c *serverController) Routes() []httpapi.Route {
	return []httpapi.Route{
		httpapi.Route{http.MethodGet, "/list", http.HandlerFunc(c.handleList)},
		httpapi.Route{http.MethodPost, "/register", http.HandlerFunc(c.handleRegister)},
		httpapi.Route{http.MethodDelete, "/remove", http.HandlerFunc(c.handleRemove)},
	}
}

func (c *serverController) handleList(w http.ResponseWriter, r *http.Request) {
	meta, _ := httpapi.MetaFromContext(r.Context())

	serverList := c.repository.List()

	// Send server list to client
	meta.JSON(http.StatusOK, serverList)
}

func (c *serverController) handleRegister(w http.ResponseWriter, r *http.Request) {
	meta, _ := httpapi.MetaFromContext(r.Context())

	requestAddr, _ := srvrepo.ParseServerAddress(r.RemoteAddr)
	var serverData srvrepo.Server

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(r.Body)
	if err := json.Unmarshal(body, &serverData); err != nil {
		meta.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	// Update the last-seen value to "now"
	serverData.Seen()

	// Debug printing
	//fmt.Println(string(body), serverData)

	fmt.Println("A server is registering.")

	if err := serverData.Validate(); err != nil {
		fmt.Printf("error during input validation: %v\n", err)
		meta.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
		return
	}

	if !serverData.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		fmt.Printf("error during request validation: %v\n", err)
		meta.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	existed, err := c.repository.Register(serverData)
	if err != nil {
		fmt.Printf("error registering server: %v\n", err)
		meta.JSON(http.StatusInternalServerError, gin.H{"result": "internal server error"})
		return
	}

	if existed {
		fmt.Println("This server is already registered.")
		meta.JSON(http.StatusOK, gin.H{"result": "updated"})
		return
	}

	fmt.Println("New server registered!")
	meta.JSON(http.StatusCreated, gin.H{"result": "registered"})
}

func (c *serverController) handleRemove(w http.ResponseWriter, r *http.Request) {
	meta, _ := httpapi.MetaFromContext(r.Context())

	requestAddr, _ := srvrepo.ParseServerAddress(r.RemoteAddr)
	var serverAddr srvrepo.ServerAddress

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(r.Body)
	if err := json.Unmarshal(body, &serverAddr); err != nil {
		meta.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	fmt.Println("A server is being removed.")

	if err := serverAddr.Validate(); err != nil {
		fmt.Printf("error during input validation: %v\n", err)
		meta.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
		return
	}

	if !serverAddr.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		fmt.Printf("error during request validation: %v\n", err)
		meta.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	exists := c.repository.Remove(srvrepo.ServerID(serverAddr.String()))

	if !exists {
		fmt.Println("The server was not found.")
		meta.JSON(http.StatusNotFound, gin.H{"result": "failure"})
		return
	}

	fmt.Println("This server is being removed.")
	meta.JSON(200, gin.H{"result": "success"})
}
