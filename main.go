package main

import (
	"flag"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/internal/httpapi"
	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
)

// logFlushInterval is how often glog's buffers are flushed to disk. glog's own
// daemon only flushes every 30 seconds, which is a long time to wait for a log
// line while watching a live problem.
const logFlushInterval = 3 * time.Second

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

// flushLogs flushes glog's buffers on an interval, running via an infinite
// ticker.
func flushLogs(interval time.Duration) {
	for range time.Tick(interval) {
		glog.Flush()
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

	serveAddr := net.JoinHostPort(ipAddr, strconv.Itoa(portNum))

	router := initApp(staleThreshold)

	glog.Infof("Server starting with arguments: %s staleThreshold=%v", serveAddr, staleThreshold)

	http.ListenAndServe(serveAddr, router)
}

func initApp(staleThreshold int) http.Handler {
	// Release mode unless the environment asks for something else, so the
	// route dump and debug warnings stay out of production logs.
	if _, ok := os.LookupEnv(gin.EnvGinMode); !ok {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(httpapi.Logger())
	router.Use(gin.Recovery())
	router.Use(gzip.Gzip(gzip.DefaultCompression))

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
	go flushLogs(logFlushInterval)

	return router
}
