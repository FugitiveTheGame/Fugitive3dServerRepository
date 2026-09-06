package httpapi

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)

	// Keep glog's output out of the test log and out of the system temp root.
	// os.Exit skips deferred calls, so the cleanup is explicit.
	logDir, err := os.MkdirTemp("", "httpapi-test-logs")
	if err == nil {
		flag.Set("log_dir", logDir)
	}

	code := m.Run()

	glog.Flush()
	if logDir != "" {
		os.RemoveAll(logDir)
	}

	os.Exit(code)
}

// newTestRouter mirrors the route table wired up in main.initApp, which is not
// importable from here.
func newTestRouter() (*gin.Engine, *srvrepo.ServerRepository) {
	repository := srvrepo.NewServerRepository()
	controller := NewServerController(repository)

	router := gin.New()
	router.GET("/reflection/ip", HandleGetIP)
	router.GET("/servers", controller.HandleList)
	router.POST("/servers/:server_id", controller.HandleRegister)
	router.PUT("/servers/:server_id", controller.HandleUpdate)
	router.DELETE("/servers/:server_id", controller.HandleRemove)

	return router, repository
}

func doRequest(router http.Handler, method, target, remoteAddr, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}

func assertStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int) {
	t.Helper()

	if recorder.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", recorder.Code, want, recorder.Body.String())
	}
}

func assertResult(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()

	var body struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not a JSON object: %v (body: %s)", err, recorder.Body.String())
	}

	if body.Result != want {
		t.Errorf("result = %q, want %q", body.Result, want)
	}
}

// assertSingleJSONBody guards the regression where a handler wrote an error
// response and then fell through to write a second one, leaving two JSON
// objects concatenated in the body.
func assertSingleJSONBody(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))

	var first any
	if err := decoder.Decode(&first); err != nil {
		t.Fatalf("response body is not valid JSON: %v (body: %s)", err, recorder.Body.String())
	}

	if decoder.More() {
		t.Errorf("response body contains more than one JSON value: %s", recorder.Body.String())
	}
}
