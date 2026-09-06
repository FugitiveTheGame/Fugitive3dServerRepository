package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLevelForStatus(t *testing.T) {
	cases := []struct {
		status int
		want   logLevel
	}{
		{status: http.StatusOK, want: levelInfo},
		{status: http.StatusCreated, want: levelInfo},
		{status: http.StatusAccepted, want: levelInfo},
		{status: http.StatusMovedPermanently, want: levelInfo},
		{status: http.StatusBadRequest, want: levelWarning},
		{status: http.StatusForbidden, want: levelWarning},
		{status: http.StatusNotFound, want: levelWarning},
		{status: http.StatusNotAcceptable, want: levelWarning},
		{status: http.StatusInternalServerError, want: levelError},
		{status: http.StatusGatewayTimeout, want: levelError},
	}

	for _, tc := range cases {
		if got := levelForStatus(tc.status); got != tc.want {
			t.Errorf("levelForStatus(%d) = %d, want %d", tc.status, got, tc.want)
		}
	}
}

func TestFormatRequest(t *testing.T) {
	line := formatRequest(http.StatusAccepted, 1500*time.Microsecond, "203.0.113.4", http.MethodPut, "/servers/203.0.113.4:45677", "")

	for _, want := range []string{"202", "1.5ms", "203.0.113.4", "PUT", "/servers/203.0.113.4:45677"} {
		if !strings.Contains(line, want) {
			t.Errorf("formatRequest() = %q, want it to contain %q", line, want)
		}
	}
}

// The middleware this replaced wrote raw ANSI color escapes into glog's log
// files, which are read far more often than they are watched on a terminal.
func TestFormatRequestHasNoANSIEscapes(t *testing.T) {
	statuses := []int{http.StatusOK, http.StatusBadRequest, http.StatusInternalServerError}

	for _, status := range statuses {
		line := formatRequest(status, time.Millisecond, "203.0.113.4", http.MethodGet, "/servers", "")

		if strings.ContainsRune(line, 0x1b) {
			t.Errorf("formatRequest(%d) contains an ANSI escape: %q", status, line)
		}
	}
}

func TestFormatRequestIncludesErrors(t *testing.T) {
	line := formatRequest(http.StatusInternalServerError, time.Millisecond, "203.0.113.4", http.MethodGet, "/servers", "something broke\n")

	if !strings.Contains(line, "something broke") {
		t.Errorf("formatRequest() = %q, want it to contain the error text", line)
	}
	if strings.HasSuffix(line, "\n") {
		t.Errorf("formatRequest() = %q, want no trailing newline", line)
	}
}

// A request with no errors must not get an empty trailing separator.
func TestFormatRequestOmitsEmptyErrors(t *testing.T) {
	line := formatRequest(http.StatusOK, time.Millisecond, "203.0.113.4", http.MethodGet, "/servers", "")

	if strings.HasSuffix(strings.TrimSpace(line), "|") {
		t.Errorf("formatRequest() = %q, want no trailing separator", line)
	}
}

// The middleware must not change what the client receives.
func TestLoggerIsTransparent(t *testing.T) {
	router := gin.New()
	router.Use(Logger())
	router.GET("/teapot", func(ctx *gin.Context) {
		ctx.JSON(http.StatusTeapot, gin.H{"result": "short and stout"})
	})

	recorder := doRequest(router, http.MethodGet, "/teapot", "203.0.113.4:51234", "")

	assertStatus(t, recorder, http.StatusTeapot)
	assertResult(t, recorder, "short and stout")
}

// A handler that panics must still be recovered and logged as a 500 rather
// than taking the process down.
func TestLoggerWithRecovery(t *testing.T) {
	router := gin.New()
	router.Use(Logger())
	router.Use(gin.Recovery())
	router.GET("/boom", func(ctx *gin.Context) {
		panic("boom")
	})

	recorder := doRequest(router, http.MethodGet, "/boom", "203.0.113.4:51234", "")

	assertStatus(t, recorder, http.StatusInternalServerError)
}
