package httpapi

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// Server is an HTTP API Server.
type Server struct {
	repository srvrepo.ServerRepository

	mux *gin.Engine
}

// NewServer creates a new Server and returns its pointer.
func NewServer(repository srvrepo.ServerRepository) *Server {
	server := &Server{
		repository: repository,
	}

	server.initMux()

	return server
}

// ListenAndServe listens on the given address and starts serving requests.
func (s *Server) ListenAndServe(addr string) {
	s.mux.Run(addr)
}

func (s *Server) initMux() {
	s.mux = gin.Default()
	s.mux.Use(gzip.Gzip(gzip.DefaultCompression))

	// Register handlers
	s.mux.GET("/list", s.handleList)
	s.mux.POST("/register", s.handleRegister)
	s.mux.DELETE("/remove", s.handleRemove)
	s.mux.GET("/getip", s.handleGetIP)
}

func (s *Server) handleList(ctx *gin.Context) {
	serverList := s.repository.List()

	// Send server list to client
	ctx.JSON(http.StatusOK, serverList)
}

func (s *Server) handleRegister(ctx *gin.Context) {
	requestAddr, _ := srvrepo.ParseServerAddress(ctx.Request.RemoteAddr)
	var serverData srvrepo.Server

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(ctx.Request.Body)
	if err := json.Unmarshal(body, &serverData); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	// Update the last-seen value to "now"
	serverData.Seen()

	// Debug printing
	//fmt.Println(string(body), serverData)

	fmt.Println("A server is registering.")

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

	existed, err := s.repository.Register(serverData)
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

func (s *Server) handleRemove(ctx *gin.Context) {
	requestAddr, _ := srvrepo.ParseServerAddress(ctx.Request.RemoteAddr)
	var serverAddr srvrepo.ServerAddress

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(ctx.Request.Body)
	if err := json.Unmarshal(body, &serverAddr); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	fmt.Println("A server is being removed.")

	if err := serverAddr.Validate(); err != nil {
		fmt.Printf("error during input validation: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
		return
	}

	if !serverAddr.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		fmt.Printf("error during request validation: %v\n", err)
		ctx.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	exists := s.repository.Remove(srvrepo.ServerID(serverAddr.String()))

	if !exists {
		fmt.Println("The server was not found.")
		ctx.JSON(http.StatusNotFound, gin.H{"result": "failure"})
		return
	}

	fmt.Println("This server is being removed.")
	ctx.JSON(200, gin.H{"result": "success"})
}

func (s *Server) handleGetIP(ctx *gin.Context) {
	ip, port, err := net.SplitHostPort(ctx.Request.RemoteAddr)
	if err != nil {
		fmt.Println(err.Error())
		ctx.JSON(500, gin.H{"result": "internal server error"})
		return
	}

	fmt.Println("Incoming request /getip:", ip+":"+port)
	// Only return the IP, even though we have their source ephemeral port.
	ctx.JSON(200, gin.H{"ip": ip})
}
