package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo/inmemory"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// test it out
// curl -d '{"name":"special server", "ip":"1.2.3.5", "port":"45677"}' -H "Content-Type: application/json" -X POST localhost:8080/register

// Our in-memory storage for registered servers
// TODO: Move away from global references.
var servers = inmemory.NewServerRepository()

// Called by servers to let clients know they exist
// TODO: You really should be able to have multiple servers on one IP.
func register(c *gin.Context) {
	requestAddr, _ := srvrepo.ParseServerAddress(c.Request.RemoteAddr)
	var serverData srvrepo.Server

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(c.Request.Body)
	if err := json.Unmarshal(body, &serverData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	// Update the last-seen value to "now"
	serverData.Seen()

	// Debug printing
	//fmt.Println(string(body), serverData)

	fmt.Println("A server is registering.")

	if err := serverData.Validate(); err != nil {
		fmt.Printf("error during input validation: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
		return
	}

	if !serverData.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		fmt.Printf("error during request validation: %v\n", err)
		c.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	existed, err := servers.Register(serverData)
	if err != nil {
		fmt.Printf("error registering server: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"result": "internal server error"})
		return
	}

	if existed {
		fmt.Println("This server is already registered.")
		c.JSON(http.StatusOK, gin.H{"result": "updated"})
		return
	}

	fmt.Println("New server registered!")
	c.JSON(http.StatusCreated, gin.H{"result": "registered"})
}

// Called by clients to get a list of active servers
func list(c *gin.Context) {
	serverList := servers.List()

	// Send server list to client
	c.JSON(http.StatusOK, serverList)
}

// Gather the source IP from an incoming HTTP request.
func getip(c *gin.Context) {
	ip, port, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		fmt.Println(err.Error())
		c.JSON(500, gin.H{"result": "internal server error"})
	} else {
		fmt.Println("Incoming request /getip:", ip+":"+port)
		// Only return the IP, even though we have their source ephemeral port.
		c.JSON(200, gin.H{"ip": ip})
	}
}

// Allow servers to remove _themselves_ from the list when requested.
// They cannot remove entries for IP addresses other than their origin IP.
// Only jerks do that.
func remove(c *gin.Context) {
	requestAddr, _ := srvrepo.ParseServerAddress(c.Request.RemoteAddr)
	var serverAddr srvrepo.ServerAddress

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(c.Request.Body)
	if err := json.Unmarshal(body, &serverAddr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	fmt.Println("A server is being removed.")

	if err := serverAddr.Validate(); err != nil {
		fmt.Printf("error during input validation: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"result": err.Error()})
		return
	}

	if !serverAddr.IP.Equal(requestAddr.IP) {
		err := fmt.Errorf("request IP address does not match client IP address")

		fmt.Printf("error during request validation: %v\n", err)
		c.JSON(http.StatusForbidden, gin.H{"result": err.Error()})
		return
	}

	exists := servers.Remove(srvrepo.ServerID(serverAddr.String()))

	if !exists {
		fmt.Println("The server was not found.")
		c.JSON(http.StatusNotFound, gin.H{"result": "failure"})
		return
	}

	fmt.Println("This server is being removed.")
	c.JSON(200, gin.H{"result": "success"})
}

// pruneServers takes a threshold duration for server age to prune old servers,
// running via an infinite ticker that ticks at half the duration of the given
// threshold.
func pruneServers(threshold time.Duration) {
	// The interval is half the treshold
	interval := threshold / 2

	for range time.Tick(interval) {
		servers.Prune(threshold)
	}
}

func main() {
	// Allow users to provide arguments on the CLI
	var ipAddr string
	var portNum int
	var staleThreshold int

	flag.StringVar(&ipAddr, "a", "0.0.0.0", "IP address for repository  to listen on")
	flag.IntVar(&portNum, "p", 8080, "TCP port for repository to listen on")
	flag.IntVar(&staleThreshold, "s", 30, "Duration (in seconds) before a server is marked stale")
	flag.Parse()

	s := fmt.Sprintf("Server starting with arguments: %s:%d staleThreshold=%v", ipAddr, portNum, staleThreshold)
	fmt.Println(s)

	router := gin.Default()
	router.Use(gzip.Gzip(gzip.DefaultCompression))

	// Log to a file (overwrite) and stdout
	f, _ := os.Create("gin-server.log")
	gin.DefaultWriter = io.MultiWriter(f, os.Stdout)

	// Register endpoints
	router.POST("/register", register)
	router.GET("/list", list)
	router.GET("/getip", getip)
	router.DELETE("/remove", remove)

	// thread w/locking for the pruning operations
	go pruneServers(time.Duration(staleThreshold) * time.Second)

	// Start her up!
	p := fmt.Sprintf("%s:%d", ipAddr, portNum)
	router.Run(p)
}
