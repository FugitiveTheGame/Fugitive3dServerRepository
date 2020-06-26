package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/internal/httpapi"
	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/szuecs/gin-glog"
)

// test it out
// curl -d '{"name":"special server", "ip":"1.2.3.5", "port":"45677"}' -H "Content-Type: application/json" -X POST localhost:8080/register

// pruneServers takes a threshold duration for server age to prune old servers,
// running via an infinite ticker that ticks at half the duration of the given
// threshold.
func pruneServers(repository *srvrepo.ServerRepository, threshold time.Duration) {
	// The interval is half the treshold
	interval := threshold / 2

	for range time.Tick(interval) {
		repository.Prune(threshold)
	}
}

func main() {
	// Allow users to provide arguments on the CLI
	var ipAddr string
	var portNum int
	var staleThreshold int
	var logPath string

	flag.StringVar(&ipAddr, "a", "0.0.0.0", "IP address for repository  to listen on")
	flag.IntVar(&portNum, "p", 8080, "TCP port for repository to listen on")
	flag.IntVar(&staleThreshold, "s", 30, "Duration (in seconds) before a server is marked stale")
	flag.StringVar(&logPath, "l", "gin-server.log", "Path to write log file to")
	flag.Parse()

	serveAddr := net.JoinHostPort(ipAddr, strconv.Itoa(portNum))

	s := fmt.Sprintf("Server starting with arguments: %s staleThreshold=%v", serveAddr, staleThreshold)
	fmt.Println(s)

	router := initApp(staleThreshold, logPath)

	http.ListenAndServe(serveAddr, router)
}

func initApp(staleThreshold int, logPath string) http.Handler {
	// Log to a file (overwrite) and stdout
	f, _ := os.Create(logPath)

	// TODO: This is overriding globally. We should likely use a better scope.
	gin.DefaultWriter = io.MultiWriter(f, os.Stdout)

	router := gin.Default()
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(ginglog.Logger(3 * time.Second))
	router.Use(gin.Recovery())

	repository := srvrepo.NewServerRepository()
	srvController := httpapi.NewServerController(repository)

	// Register endpoint handlers
	router.GET("/reflection/ip", httpapi.HandleGetIP)
	router.GET("/servers", srvController.HandleList)
	router.POST("/servers/:server_id", srvController.HandleRegister)
	router.PUT("/servers/:server_id", srvController.HandleUpdate)
	router.DELETE("/servers/:server_id", srvController.HandleRemove)

	// thread w/locking for the pruning operations
	go pruneServers(repository, time.Duration(staleThreshold)*time.Second)

	return router
}
