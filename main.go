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

// serverID defines the identifier of a particular server.
type serverID string

// server defines a structure for our server data.
type server struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`

	Name        string `json:"name"`
	GameVersion int    `json:"game_version"`

	LastSeen jsonTime `json:"last_seen"`
}

// ID returns the serverID for a server, generated based on its internal data.
func (s *server) ID() serverID {
	strID := fmt.Sprintf("%s:%d", s.IP, s.Port)

	return serverID(strID)
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

	// TODO: Validate?
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
var servers = make(map[string]server)

var mux = &sync.RWMutex{}

func verifyIP(ip string) bool {
	// verify the IP address provided is valid.
	// This just ensures it's _any_ IPv4 address.
	addr := net.ParseIP(ip)
	if addr.To4() == nil {
		fmt.Fprintln(os.Stdout, ip, "is not a valid IPv4 address")
		return false
	}
	return true
}

func verifyPort(port string) bool {
	// Ensure the port is between 1024 and 65535 (applies in TCP and UDP)
	if n, err := strconv.Atoi(port); err == nil {
		if 1024 > n || n > 65535 {
			// TODO: be a little more specific so they know what to do
			fmt.Fprintln(os.Stdout, port, "is not a valid port number")
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

func validateEntry(ctx *gin.Context, jname string, jip string, jport string) bool {
	// Run simple input validation
	// ctx == the gin context, for nabbing their source IP
	// info = the serverInfo map of their JSON request data

	// Your name should be in our required range.
	// Your IP needs to be a real IPv4 address.
	// Your jport needs to be in the ephemeral range.
	// Your incoming source IP must match the IP in your payload.

	// ip == their source client IP
	// port == source port, only useful for logging/debugging
	ip, _, _ := net.SplitHostPort(ctx.Request.RemoteAddr)

	fmt.Fprintln(os.Stdout, "Source IP: "+ip+" Payload IP: "+jip)
	if ip != jip {
		return false // their source IP != JSON IP value, spoofing/typo case
	}

	// clean the name string and measure length here.
	a := cleanName(jname)
	if 3 > len(a) || len(a) > 32 {
		fmt.Fprintln(os.Stdout, jname, "must be between 3 and 32 characters.")
		return false
	}
	b := verifyIP(ip)      // their detected source IP
	c := verifyPort(jport) // their port provided in the payload
	if !b || !c {
		return false
	}
	return true
}

// Each server entry needs to be keyed on 'detected source ip:advertised port' (string)
// This will allow the same IP to have servers running on multiple ports, if desired.
func createKey(ip string, port string) string {
	key := ip + ":" + port
	return key
}

// Called by servers to let clients know they exist
// TODO: You really should be able to have multiple servers on one IP.
func register(c *gin.Context) {
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

	// run simple validation before we register it.
	// TODO: Change validation interface
	if validateEntry(c, serverData.Name, serverData.IP.String(), strconv.Itoa(serverData.Port)) {
		// key is your incoming IP:advertised port (string)
		key := createKey(serverData.IP.String(), strconv.Itoa(serverData.Port))

		// Input was valid. Are they new or updating?
		// only thing you change is the message you send back to the user
		mux.RLock()

		if _, ok := servers[key]; ok {
			//updating
			fmt.Println("This server is already registered.")
			c.JSON(200, gin.H{"result": "updated"})
		} else {
			// Report back to the uer: new server was created
			fmt.Println("New server registered!")
			c.JSON(201, gin.H{"result": "registered"})
		}
		mux.RUnlock()

		mux.Lock()
		servers[key] = serverData
		mux.Unlock()
	} else {
		// They failed payload validation.
		c.JSON(http.StatusBadRequest, gin.H{"result": "name, IP, or port was invalid!"})
	}
}

// Called by clients to get a list of active servers
func list(c *gin.Context) {
	// Remove old servers
	//pruneServers()

	// Marshall the servers into a list for JSON
	var serverList = make([]server, 0)

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

// prune interval is always half the stale threshold
func pruneInterval(stale int) time.Duration {
	var d = time.Duration(stale / 2)
	return time.Duration(d * time.Second)
}

// Allow servers to remove _themselves_ from the list when requested.
// They cannot remove entries for IP addresses other than their origin IP.
// Only jerks do that.
func remove(c *gin.Context) {
	var serverData server

	// New servers are tracked for 60 seconds unless updated.
	body, _ := ioutil.ReadAll(c.Request.Body)
	if err := json.Unmarshal(body, &serverData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"result": "invalid request JSON"})
	}

	// Override the name due to our validation mechanism
	// TODO: Remove this override in favor of a different validation mechanism
	serverData.Name = "not important"

	fmt.Println("A server is registering.")

	if validateEntry(c, serverData.Name, serverData.IP.String(), strconv.Itoa(serverData.Port)) {
		// key is your incoming IP:advertised port (string)
		key := createKey(serverData.IP.String(), strconv.Itoa(serverData.Port))

		mux.RLock()
		if _, ok := servers[key]; ok {
			// It was found, remove it.
			fmt.Println("This server is being removed.")
			c.JSON(200, gin.H{"result": "success"})
		} else {
			// Report back to user: it wasn't removed because it wasn't found.
			fmt.Println("The server was not found.")
			c.JSON(404, gin.H{"result": "failure"})
		}
		mux.RUnlock()

		mux.Lock()
		delete(servers, key)
		mux.Unlock()
	} else {
		// They failed payload validation.
		c.JSON(http.StatusBadRequest, gin.H{"result": "name, IP, or port was invalid!"})
	}
}

// Remove servers that we haven't seen in a while
// TODO: needs to move to a channel/async
func pruneServers(staleThreshold int) {
	var pruneInterval = pruneInterval(staleThreshold)
	for {
		now := time.Now()

		// Check each server
		for key, server := range servers {

			// Parse the last seen time, then check if it's too old
			if server.LastSeen.Add(pruneInterval * 2).Before(now) {
				// Too old, remove server
				s := fmt.Sprintf("Pruning IP: %s", key)
				fmt.Println(s)
				mux.Lock()
				delete(servers, key)
				mux.Unlock()
			}
		}
		time.Sleep(pruneInterval)
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
	go pruneServers(staleThreshold)

	// Start her up!
	p := fmt.Sprintf("%s:%d", ipAddr, portNum)
	router.Run(p)
}
