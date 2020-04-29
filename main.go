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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// test it out
// curl -d '{"name":"special server", "ip":"1.2.3.5", "port":"45677"}' -H "Content-Type: application/json" -X POST localhost:8080/register

// timeFormatJSON defines the format that we use for time formatting in JSON.
const timeFormatJSON = time.RFC3339

// port range min and max values for valid server address ports.
const (
	portRangeMin = 1024
	portRangeMax = 65535
)

// name length min and max values for valid server names.
const (
	nameLengthMin = 3
	nameLengthMax = 32
)

// jsonTime defines a time.Time with custom marshalling (embedded for method
// access, rather than aliasing)
type jsonTime struct {
	time.Time
	// put nothing else here...
}

// MarshalJSON satisfies the encoding/json.Marshaler interface and customizes
// the JSON formatting of the jsonTime structure.
func (t jsonTime) MarshalJSON() ([]byte, error) {
	formatted := t.Format(timeFormatJSON)

	return json.Marshal(&formatted)
}

// serverAddress defines the structure of a server address.
type serverAddress struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`
}

// parseServerAddress parses a string address into a serverAddress, returning
// the parsed value and any errors that occurred during parsing.
func parseServerAddress(s string) (serverAddress, error) {
	var addr serverAddress

	rawIP, rawPort, err := net.SplitHostPort(s)
	if err != nil {
		return addr, err
	}

	ip := net.ParseIP(rawIP)
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return addr, fmt.Errorf("invalid port number with err: %w", err)
	}

	addr = serverAddress{
		IP:   ip,
		Port: port,
	}

	return addr, nil
}

// String satisfies the fmt.Stringer interface and returns a string form of the
// serverAddress structure.
func (a *serverAddress) String() string {
	return net.JoinHostPort(
		a.IP.String(),
		strconv.Itoa(a.Port),
	)
}

// Validate runs validations on the value and returns an error if the value is
// invalid for any reason.
func (a *serverAddress) Validate() error {
	if a.IP.To4() == nil {
		return fmt.Errorf("IP is not a valid IPv4 address")
	}

	if a.Port < portRangeMin || a.Port > portRangeMax {
		return fmt.Errorf("port is not within the valid port range of %d-%d", portRangeMin, portRangeMax)
	}

	return nil
}

// serverID defines the identifier of a particular server.
type serverID string

// server defines a structure for our server data.
type server struct {
	serverAddress // embedded to flatten the structure

	Name        string `json:"name"`
	GameVersion int    `json:"game_version"`

	LastSeen jsonTime `json:"last_seen"`
}

// ID returns the serverID for a server, generated based on its internal data.
func (s *server) ID() serverID {
	return serverID(s.serverAddress.String())
}

// Validate runs validations on the value and returns an error if the value is
// invalid for any reason.
func (s *server) Validate() error {
	if err := s.serverAddress.Validate(); err != nil {
		return err
	}

	// TODO: We likely should be cleaning/normalizing inputs when unmarshalling,
	// rather than during validation.
	s.Name = strings.TrimSpace(s.Name)
	if nameLength := len(s.Name); nameLength < nameLengthMin || nameLength > nameLengthMax {
		return fmt.Errorf("name length must be within range of %d-%d", nameLengthMin, nameLengthMax)
	}

	return nil
}

// serverRepository defines the structure for an in-memory server repository.
type serverRepository struct {
	servers map[serverID]server

	mu sync.RWMutex
}

// newServerRepository returns a pointer to a new initialized serverRepository.
func newServerRepository() *serverRepository {
	return &serverRepository{
		servers: make(map[serverID]server),
	}
}

// List returns a slice representation of the servers in the repository.
func (r *serverRepository) List() []server {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serverList := make([]server, 0, len(r.servers))

	for _, srv := range r.servers {
		serverList = append(serverList, srv)
	}

	return serverList
}

// Register takes a server and registers it with the repository, returning a
// bool that represents whether the server already existed or not (true for
// already exists, false otherwise), and a potential error if the registration
// failed.
func (r *serverRepository) Register(srv server) (bool, error) {
	alreadyExists := false
	var err error

	// TODO: Normalize? Validate?
	id := srv.ID()

	r.mu.Lock()
	defer r.mu.Unlock()

	_, alreadyExists = r.servers[id]
	r.servers[id] = srv

	return alreadyExists, err
}

// Remove takes a serverID and removes the corresponding server from the
// repository, returning a bool that represents whether the server existed or
// not (true for exists, false otherwise).
func (r *serverRepository) Remove(id serverID) bool {
	exists := false

	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists = r.servers[id]
	delete(r.servers, id)

	return exists
}

// Prune takes a time.Duration representing the threshold of when a server's
// last-seen "age" should be considered too old, and removes those servers from
// the repository.
func (r *serverRepository) Prune(threshold time.Duration) {
	cutoff := time.Now().Add(-threshold)

	r.mu.Lock()
	defer r.mu.Unlock()

	for id, srv := range r.servers {
		if srv.LastSeen.Before(cutoff) {
			// TODO: Log with an abstraction
			fmt.Printf("Pruning server: %s\n", id)

			delete(r.servers, id)
		}
	}
}

// Our in-memory storage for registered servers
// TODO: Move away from global references.
var servers = newServerRepository()

// Called by servers to let clients know they exist
// TODO: You really should be able to have multiple servers on one IP.
func register(c *gin.Context) {
	requestAddr, _ := parseServerAddress(c.Request.RemoteAddr)
	var serverData server

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(c.Request.Body)
	if err := json.Unmarshal(body, &serverData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	// Update the last-seen value to "now"
	serverData.LastSeen = jsonTime{time.Now()}

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
	requestAddr, _ := parseServerAddress(c.Request.RemoteAddr)
	var serverAddr serverAddress

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

	exists := servers.Remove(serverID(serverAddr.String()))

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
