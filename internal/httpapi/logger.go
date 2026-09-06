package httpapi

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
)

// successVerbosity is the glog verbosity at which successful requests are
// logged. Every registered game server heartbeats more often than the stale
// threshold, so logging those at the default verbosity would bury everything
// else. Run with -v=2 to see them.
const successVerbosity glog.Level = 2

// logLevel is the glog severity a request line is written at.
type logLevel int

const (
	levelInfo logLevel = iota
	levelWarning
	levelError
)

// levelForStatus maps a response status onto the severity its log line is
// written at.
func levelForStatus(status int) logLevel {
	switch {
	case status >= 500:
		return levelError
	case status >= 400:
		return levelWarning
	default:
		return levelInfo
	}
}

// formatRequest renders the log line for a completed request.
func formatRequest(status int, latency time.Duration, clientIP, method, path, errs string) string {
	line := fmt.Sprintf("[GIN] %3d | %12s | %-15s | %-6s %s",
		status, latency, clientIP, method, path)

	if errs = strings.TrimSpace(errs); errs != "" {
		line += " | " + errs
	}

	return line
}

// Logger returns a gin middleware that writes one glog line per request,
// routing it to a severity based on the response status.
func Logger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()

		ctx.Next()

		status := ctx.Writer.Status()
		line := formatRequest(
			status,
			time.Since(start),
			ctx.ClientIP(),
			ctx.Request.Method,
			ctx.Request.URL.Path,
			ctx.Errors.String(),
		)

		switch levelForStatus(status) {
		case levelError:
			glog.Error(line)
		case levelWarning:
			glog.Warning(line)
		default:
			glog.V(successVerbosity).Info(line)
		}
	}
}
