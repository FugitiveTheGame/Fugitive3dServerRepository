package main

import (
	"encoding/json"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"net/http"
	"time"
)

// Our in-memory storage for registered servers
var servers = make(map[string]map[string]string)

const STALE_THRESHOLD = time.Duration(60 * time.Second)

// Json keys expected from the client
const SERV_NAME = "name"
const SERV_IP = "ip"
const SERV_PORT = "port"
const SERV_LAST_SEEN = "last_seen"


// Called by servers to let clients know they exist
func register(c *gin.Context) {
	// $TODO This needs error checking
	body, _ := ioutil.ReadAll(c.Request.Body)
	severInfoMap := make(map[string]string)
	_ = json.Unmarshal(body, &severInfoMap)

	// Debug printing
	//print(string(body))

	// Pull data out of request
	name := severInfoMap[SERV_NAME]
	ip := severInfoMap[SERV_IP]
	port := severInfoMap[SERV_PORT]

	// Create our local representation
	serverInfo := map[string]string {
		SERV_NAME:      name,
		SERV_IP:        ip,
		SERV_PORT:      port,
		SERV_LAST_SEEN: time.Now().Format(time.RFC3339),
	}

	// Store, and replace any old representation
	servers[ip] = serverInfo

	// Report back to the user
	c.JSON(http.StatusOK, gin.H{
		"result": "registered",
	})
}

// Called by clients to get a list of active servers
func list(c *gin.Context) {
	// Remove old servers
	pruneServers()

	// Marshall the servers into a list for JSON
	var serverList []map[string]string
	for _, value := range servers {
		serverList = append(serverList, value)
	}

	// Send server list to client
	c.JSON(http.StatusOK, serverList)
}

// Remove servers that we haven't seen in a while
func pruneServers() {
	now := time.Now()

	// Check each server
	for ip, server := range servers {

		// Parse the last seen time, then check if it's too old
		lastSeen, _ := time.Parse( time.RFC3339, server[SERV_LAST_SEEN] )
		if lastSeen.Add(STALE_THRESHOLD).Before(now) {
			// Too old, remove server
			delete(servers, ip)
		}
	}
}

func main() {
	router := gin.Default()
	router.Use(gzip.Gzip(gzip.DefaultCompression))

	// Register endpoints
	router.POST("/register", register)
	router.GET("/list", list)

	// Start her up!
	router.Run(":8080")
}
