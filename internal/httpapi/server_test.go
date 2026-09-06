package httpapi

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
)

const validBody = `{"name":"a server","game_version":3,"current_players":2,"max_players":8,"is_joinable":true}`

// startUDPResponder listens on an ephemeral loopback UDP port and answers every
// datagram with the given reply, standing in for a real game server answering
// the repository's reachability ping. It returns the "ip:port" address it is
// listening on, which doubles as the server ID.
func startUDPResponder(t *testing.T, reply string) string {
	t.Helper()

	connection, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("could not listen on a loopback UDP port: %v", err)
	}
	t.Cleanup(func() { connection.Close() })

	go func() {
		buffer := make([]byte, 64)
		for {
			n, peer, err := connection.ReadFromUDP(buffer)
			if err != nil {
				// The listener was closed at the end of the test.
				return
			}
			if string(buffer[:n]) == "ping" {
				connection.WriteToUDP([]byte(reply), peer)
			}
		}
	}()

	return connection.LocalAddr().String()
}

// freeLoopbackAddress returns a loopback address with nothing listening on it.
func freeLoopbackAddress(t *testing.T) string {
	t.Helper()

	connection, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("could not listen on a loopback UDP port: %v", err)
	}

	addr := connection.LocalAddr().String()
	connection.Close()

	return addr
}

// remoteAddrFor builds a client address that shares the server address's IP, so
// the handler's source-IP check passes.
func remoteAddrFor(t *testing.T, serverAddr string) string {
	t.Helper()

	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) returned unexpected error: %v", serverAddr, err)
	}

	return net.JoinHostPort(host, "51234")
}

func TestHandleListEmpty(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequest(router, http.MethodGet, "/servers", "203.0.113.4:51234", "")

	assertStatus(t, recorder, http.StatusOK)

	// An empty repository must serialize as [], not null, so the client can
	// iterate the result unconditionally.
	if got := recorder.Body.String(); got != "[]" {
		t.Errorf("body = %s, want []", got)
	}
}

func TestHandleList(t *testing.T) {
	router, _ := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), validBody)

	recorder := doRequest(router, http.MethodGet, "/servers", "198.51.100.7:51234", "")
	assertStatus(t, recorder, http.StatusOK)

	var servers []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &servers); err != nil {
		t.Fatalf("response body is not a JSON array: %v (body: %s)", err, recorder.Body.String())
	}

	if len(servers) != 1 {
		t.Fatalf("listed %d servers, want 1 (body: %s)", len(servers), recorder.Body.String())
	}

	for field, want := range map[string]any{
		"ip":              "203.0.113.4",
		"port":            float64(45677),
		"name":            "a server",
		"game_version":    float64(3),
		"current_players": float64(2),
		"max_players":     float64(8),
		"is_joinable":     true,
	} {
		if got := servers[0][field]; got != want {
			t.Errorf("field %q = %v, want %v", field, got, want)
		}
	}

	if _, ok := servers[0]["last_seen"]; !ok {
		t.Errorf("last_seen missing from listed server: %s", recorder.Body.String())
	}
}

// A PUT for a server the repository has never seen recreates it rather than
// failing, so registrations survive a repository restart.
func TestHandleUpdateRegistersUnknownServer(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	recorder := doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), validBody)

	assertStatus(t, recorder, http.StatusCreated)
	assertResult(t, recorder, "registered")

	if !repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Errorf("server %q was not registered", serverAddr)
	}
}

func TestHandleUpdateExistingServer(t *testing.T) {
	router, _ := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	remoteAddr := remoteAddrFor(t, serverAddr)

	doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddr, validBody)

	recorder := doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddr,
		`{"name":"a renamed server","game_version":3,"current_players":5,"max_players":8,"is_joinable":false}`)

	assertStatus(t, recorder, http.StatusAccepted)
	assertResult(t, recorder, "updated")

	listing := doRequest(router, http.MethodGet, "/servers", remoteAddr, "")

	var servers []map[string]any
	if err := json.Unmarshal(listing.Body.Bytes(), &servers); err != nil {
		t.Fatalf("response body is not a JSON array: %v (body: %s)", err, listing.Body.String())
	}

	if len(servers) != 1 {
		t.Fatalf("listed %d servers, want 1 (body: %s)", len(servers), listing.Body.String())
	}
	if servers[0]["name"] != "a renamed server" {
		t.Errorf("name = %v, want %q", servers[0]["name"], "a renamed server")
	}
	if servers[0]["current_players"] != float64(5) {
		t.Errorf("current_players = %v, want 5", servers[0]["current_players"])
	}
}

// Regression: the invalid-JSON branch used to write a 400 and then fall through
// to write a second response body.
func TestHandleUpdateInvalidJSONWritesOneResponse(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	recorder := doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), `{not json`)

	assertStatus(t, recorder, http.StatusBadRequest)
	assertSingleJSONBody(t, recorder)
	assertResult(t, recorder, "invalid request JSON")

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a server was registered from an unparseable request body")
	}
}

func TestHandleUpdateRejects(t *testing.T) {
	const serverAddr = "203.0.113.4:45677"

	cases := []struct {
		name       string
		target     string
		remoteAddr string
		body       string
		wantStatus int
		wantResult string
	}{
		{
			name:       "invalid server ID",
			target:     "/servers/notanaddress",
			remoteAddr: "203.0.113.4:51234",
			body:       validBody,
			wantStatus: http.StatusBadRequest,
			wantResult: "invalid server ID",
		},
		{
			name:       "name too short",
			target:     "/servers/" + serverAddr,
			remoteAddr: "203.0.113.4:51234",
			body:       `{"name":"ab"}`,
			wantStatus: http.StatusBadRequest,
			wantResult: "name length must be within range of 3-32",
		},
		{
			name:       "privileged port",
			target:     "/servers/203.0.113.4:80",
			remoteAddr: "203.0.113.4:51234",
			body:       validBody,
			wantStatus: http.StatusBadRequest,
			wantResult: "port is not within the valid port range of 1024-65535",
		},
		{
			name:       "source IP does not match",
			target:     "/servers/" + serverAddr,
			remoteAddr: "198.51.100.7:51234",
			body:       validBody,
			wantStatus: http.StatusForbidden,
			wantResult: "request IP address does not match client IP address",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, repository := newTestRouter()

			recorder := doRequest(router, http.MethodPut, tc.target, tc.remoteAddr, tc.body)

			assertStatus(t, recorder, tc.wantStatus)
			assertSingleJSONBody(t, recorder)
			assertResult(t, recorder, tc.wantResult)

			if len(repository.List()) != 0 {
				t.Error("a server was registered by a rejected request")
			}
		})
	}
}

func TestHandleRegister(t *testing.T) {
	router, repository := newTestRouter()

	serverAddr := startUDPResponder(t, "pong")

	recorder := doRequest(router, http.MethodPost, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), validBody)

	assertStatus(t, recorder, http.StatusOK)
	assertResult(t, recorder, "registration complete")

	if !repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Errorf("server %q was not registered", serverAddr)
	}
}

// Regression: the invalid-JSON branch inside the ping-succeeded path used to
// write a 400 and then fall through to write a second response body.
func TestHandleRegisterInvalidJSONWritesOneResponse(t *testing.T) {
	router, repository := newTestRouter()

	serverAddr := startUDPResponder(t, "pong")

	recorder := doRequest(router, http.MethodPost, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), `{not json`)

	assertStatus(t, recorder, http.StatusBadRequest)
	assertSingleJSONBody(t, recorder)
	assertResult(t, recorder, "invalid request JSON")

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a server was registered from an unparseable request body")
	}
}

func TestHandleRegisterBadPingResponse(t *testing.T) {
	router, repository := newTestRouter()

	serverAddr := startUDPResponder(t, "nope")

	recorder := doRequest(router, http.MethodPost, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), validBody)

	assertStatus(t, recorder, http.StatusNotAcceptable)
	assertResult(t, recorder, "Bad ping response")

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a server that failed the ping check was registered")
	}
}

func TestHandleRegisterInvalidServerID(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequest(router, http.MethodPost, "/servers/notanaddress", "203.0.113.4:51234", validBody)

	assertStatus(t, recorder, http.StatusNotAcceptable)
	assertResult(t, recorder, "invalid server ID")
}

func TestHandleRegisterSourceIPMismatch(t *testing.T) {
	router, repository := newTestRouter()

	serverAddr := startUDPResponder(t, "pong")

	recorder := doRequest(router, http.MethodPost, "/servers/"+serverAddr, "198.51.100.7:51234", validBody)

	assertStatus(t, recorder, http.StatusForbidden)
	assertResult(t, recorder, "request IP address does not match client IP address")

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a server registering from a foreign IP was registered")
	}
}

// An unreachable server must be reported to the caller, not take the process
// down; this path used to call glog.Fatal.
func TestHandleRegisterUnreachableServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: waits for the 5 second ping deadline")
	}

	router, repository := newTestRouter()

	serverAddr := freeLoopbackAddress(t)

	recorder := doRequest(router, http.MethodPost, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), validBody)

	// Either no reply arrives before the deadline, or the OS reports the port
	// as unreachable; both leave the server unregistered and the process up.
	if recorder.Code != http.StatusGatewayTimeout && recorder.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want %d or %d (body: %s)",
			recorder.Code, http.StatusGatewayTimeout, http.StatusPreconditionFailed, recorder.Body.String())
	}
	assertSingleJSONBody(t, recorder)

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("an unreachable server was registered")
	}

	// The repository still serves requests.
	listing := doRequest(router, http.MethodGet, "/servers", "203.0.113.4:51234", "")
	assertStatus(t, listing, http.StatusOK)
}

func TestHandleRemove(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	remoteAddr := remoteAddrFor(t, serverAddr)

	doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddr, validBody)

	recorder := doRequest(router, http.MethodDelete, "/servers/"+serverAddr, remoteAddr, "")

	assertStatus(t, recorder, http.StatusOK)
	assertResult(t, recorder, "success")

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Errorf("server %q still registered after removal", serverAddr)
	}
}

func TestHandleRemoveUnknownServer(t *testing.T) {
	router, _ := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	recorder := doRequest(router, http.MethodDelete, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), "")

	assertStatus(t, recorder, http.StatusNotFound)
	assertResult(t, recorder, "failure")
}

func TestHandleRemoveInvalidServerID(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequest(router, http.MethodDelete, "/servers/notanaddress", "203.0.113.4:51234", "")

	assertStatus(t, recorder, http.StatusNotFound)
	assertResult(t, recorder, "invalid server ID")
}

// A server may only be removed by the host it is registered from.
func TestHandleRemoveSourceIPMismatch(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddrFor(t, serverAddr), validBody)

	recorder := doRequest(router, http.MethodDelete, "/servers/"+serverAddr, "198.51.100.7:51234", "")

	assertStatus(t, recorder, http.StatusForbidden)
	assertResult(t, recorder, "request IP address does not match client IP address")

	if !repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a server was removed by a request from a foreign IP")
	}
}

// The repository must keep serving after a rejected request, and the rejection
// must not leave a partial registration behind.
func TestRejectedRequestsLeaveRepositoryUsable(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = "203.0.113.4:45677"
	remoteAddr := remoteAddrFor(t, serverAddr)

	for _, body := range []string{`{not json`, `{"name":"ab"}`, ""} {
		doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddr, body)
	}

	if got := len(repository.List()); got != 0 {
		t.Fatalf("repository holds %d servers after rejected requests, want 0", got)
	}

	recorder := doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddr, validBody)
	assertStatus(t, recorder, http.StatusCreated)

	if got := len(repository.List()); got != 1 {
		t.Errorf("repository holds %d servers, want 1", got)
	}
}

// Concurrent traffic from many game servers must not race; run with -race.
func TestConcurrentRegistrations(t *testing.T) {
	router, repository := newTestRouter()

	const servers = 16
	done := make(chan struct{}, servers)

	for i := range servers {
		go func(i int) {
			defer func() { done <- struct{}{} }()

			serverAddr := fmt.Sprintf("203.0.113.%d:45677", i+1)
			remoteAddr := fmt.Sprintf("203.0.113.%d:51234", i+1)

			doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddr, validBody)
			doRequest(router, http.MethodGet, "/servers", remoteAddr, "")
			doRequest(router, http.MethodPut, "/servers/"+serverAddr, remoteAddr, validBody)
		}(i)
	}

	for range servers {
		<-done
	}

	if got := len(repository.List()); got != servers {
		t.Errorf("repository holds %d servers, want %d", got, servers)
	}
}
