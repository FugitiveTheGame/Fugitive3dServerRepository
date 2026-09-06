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

	router, err := initApp(staleThreshold)
	if err != nil {
		glog.Exitf("could not start: %v", err)
	}

	glog.Infof("Server starting with arguments: %s staleThreshold=%v", serveAddr, staleThreshold)

	// ListenAndServe only returns on failure. Reporting it matters on a shared
	// host, where a port conflict would otherwise look like a clean exit.
	if err := http.ListenAndServe(serveAddr, router); err != nil {
		glog.Exitf("server stopped: %v", err)
	}
}

func initApp(staleThreshold int) (http.Handler, error) {
	// Release mode unless the environment asks for something else, so the
	// route dump and debug warnings stay out of production logs.
	if _, ok := os.LookupEnv(gin.EnvGinMode); !ok {
		gin.SetMode(gin.ReleaseMode)
	}

	repository := srvrepo.NewServerRepository()

	router, err := httpapi.NewRouter(repository)
	if err != nil {
		return nil, err
	}

	// thread w/locking for the pruning operations
	go pruneServers(repository, time.Duration(staleThreshold)*time.Second)
	go flushLogs(logFlushInterval)

	return router, nil
}
