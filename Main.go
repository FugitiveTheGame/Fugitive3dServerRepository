package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// test it out
// curl -d '{"name":"special server", "ip":"1.2.3.5", "port":"45677"}' -H "Content-Type: application/json" -X POST localhost:8080/register

// Our in-memory storage for registered servers
var servers = make(map[string]map[string]string)

var mux = &sync.RWMutex{}

const STALE_THRESHOLD = time.Duration(5 * time.Second)

// Json keys expected from the client
const SERV_NAME = "name"
const SERV_IP = "ip"
const SERV_PORT = "port"
const SERV_LAST_SEEN = "last_seen"

func verifyIp(ip string) bool {
	// verify the IP address provided is valid.
	// This just ensures it's _any_ IPv4 address.
	addr := net.ParseIP(ip)
	if addr.To4() == nil {
		fmt.Fprintln(os.Stdout, ip, "is not a valid IPv4 address\n")
		return false
	}
	return true
}

func verifyPort(port string) bool {
	// Ensure the port is between 1024 and 65535 (applies in TCP and UDP)
	if n, err := strconv.Atoi(port); err == nil {
		if 1024 > n || n > 65535 {
			// TODO: be a little more specific so they know what to do
			fmt.Fprintln(os.Stdout, port, "is not a valid port number.\n")
			return false
		}
		return true
	}
	return false
}

func cleanName(servername string) string {
	// Trim whitespace and newlines off ends of name
	s := strings.TrimSpace(servername)
	return s
}

func validateEntry(name string, ip string, port string) bool {
	// Run simple input validation

	// clean the name string and measure length here.
	a := cleanName(name)
	if 3 > len(a) || len(a) > 32 {
		fmt.Fprintln(os.Stdout, name, "must be between 3 and 32 characters.")
		return false
	}
	b := verifyIp(ip)
	c := verifyPort(port)
	if !b || !c {
		return false
	}
	return true
}

// Called by servers to let clients know they exist
func register(c *gin.Context) {
	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(c.Request.Body)
	severInfoMap := make(map[string]string)
	_ = json.Unmarshal(body, &severInfoMap)

	// Debug printing
	//print(string(body))

	// Pull data out of request
	name := severInfoMap[SERV_NAME]
	ip := severInfoMap[SERV_IP]
	port := severInfoMap[SERV_PORT]

	fmt.Println("A server is registering.")

	// Create our local representation
	serverInfo := map[string]string{
		SERV_NAME:      name,
		SERV_IP:        ip,
		SERV_PORT:      port,
		SERV_LAST_SEEN: time.Now().Format(time.RFC3339),
	}

	// run simple validation before we register it.
	if validateEntry(name, ip, port) {
		// Input was valid. Are they new or updating?
		// only thing you change is the message you send back to the user
		// check for membership, don't iterate
		for i, _ := range servers {
			if i == ip {
				fmt.Println("This server is already registered.")
				c.JSON(200, gin.H{"result": "updated"})
				mux.Lock()
				servers[ip] = serverInfo
				mux.Unlock()
				return
			}
		}

		// Report back to the user: new server was created
		fmt.Println("New server registered!")
		c.JSON(201, gin.H{
			"result": "registered",
		})
	} else {
		// They failed payload validation.
		c.JSON(http.StatusBadRequest, gin.H{"result": "name, IP, or port was invalid!"})
	}

	// Store, and replace any old representation
	mux.Lock()
	servers[ip] = serverInfo
	mux.Unlock()
}

// Called by clients to get a list of active servers
func list(c *gin.Context) {
	// Remove old servers
	//pruneServers()

	// Marshall the servers into a list for JSON
	var serverList = make([]map[string]string, 0)

	mux.RLock()
	for _, value := range servers {
		serverList = append(serverList, value)
	}
	mux.RUnlock()

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

// Remove servers that we haven't seen in a while
// TODO: needs to move to a channel/async
func pruneServers() {
	for {
		now := time.Now()

		// Check each server
		for ip, server := range servers {

			// Parse the last seen time, then check if it's too old
			lastSeen, _ := time.Parse(time.RFC3339, server[SERV_LAST_SEEN])
			if lastSeen.Add(STALE_THRESHOLD).Before(now) {
				// Too old, remove server
				s := fmt.Sprintf("Pruning IP: %s", ip)
				fmt.Println(s)
				mux.Lock()
				delete(servers, ip)
				mux.Unlock()
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func main() {
	// Allow users to provide arguments on the CLI
	var ipAddr string
	var portNum string

	flag.StringVar(&ipAddr, "a", "0.0.0.0", "IP address for repository  to listen on")
	flag.StringVar(&portNum, "p", "8080", "TCP port for repository to listen on")
	flag.Parse()

	router := gin.Default()
	router.Use(gzip.Gzip(gzip.DefaultCompression))

	// Log to a file (overwrite) and stdout
	f, _ := os.Create("gin-server.log")
	gin.DefaultWriter = io.MultiWriter(f, os.Stdout)

	// Register endpoints
	router.POST("/register", register)
	router.GET("/list", list)
	router.GET("/getip", getip)

	go pruneServers()

	// Start her up!
	p := fmt.Sprintf("%s:%s", ipAddr, portNum)
	router.Run(p)
}
