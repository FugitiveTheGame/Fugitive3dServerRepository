package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
)

// These tests cover the trust boundary the source-IP checks depend on. The
// service is meant to sit behind a reverse proxy on the same host, so a
// forwarding header from loopback carries the real caller's address, while the
// same header from anywhere else is an attempt to impersonate a game server
// and must be ignored.

const (
	// loopbackPeer is what the connection looks like when nginx on this host
	// forwards a request.
	loopbackPeer = "127.0.0.1:51234"

	// untrustedPeer is any other host talking to the service directly.
	untrustedPeer = "198.51.100.7:51234"

	// forwardedFor is the address a proxy reports the real client at.
	forwardedFor = "203.0.113.4"
)

func forwarded(ip string) map[string]string {
	return map[string]string{"X-Forwarded-For": ip}
}

// Behind the proxy, the forwarded address is the one that must be honoured,
// otherwise every registration is compared against 127.0.0.1 and rejected.
func TestUpdateBehindTrustedProxy(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = forwardedFor + ":45677"
	recorder := doRequestWithHeaders(router, http.MethodPut, "/servers/"+serverAddr,
		loopbackPeer, validBody, forwarded(forwardedFor))

	assertStatus(t, recorder, http.StatusCreated)
	assertResult(t, recorder, "registered")

	if !repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Errorf("server %q was not registered through the proxy", serverAddr)
	}
}

// The same header from a peer that is not a trusted proxy is an impersonation
// attempt and must not be believed.
func TestUpdateRejectsForwardedForFromUntrustedPeer(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = forwardedFor + ":45677"
	recorder := doRequestWithHeaders(router, http.MethodPut, "/servers/"+serverAddr,
		untrustedPeer, validBody, forwarded(forwardedFor))

	assertStatus(t, recorder, http.StatusForbidden)
	assertResult(t, recorder, "request IP address does not match client IP address")

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a spoofed X-Forwarded-For registered a server")
	}
}

// The hijack this protects against: taking over another game server's listing
// by claiming its address.
func TestUpdateCannotHijackAnotherServerByForwardedFor(t *testing.T) {
	router, repository := newTestRouter()

	const victim = forwardedFor + ":45677"

	// The victim registers legitimately through the proxy.
	doRequestWithHeaders(router, http.MethodPut, "/servers/"+victim,
		loopbackPeer, validBody, forwarded(forwardedFor))

	// An attacker elsewhere claims to be the victim.
	recorder := doRequestWithHeaders(router, http.MethodPut, "/servers/"+victim, untrustedPeer,
		`{"name":"hijacked server","game_version":1,"max_players":8}`, forwarded(forwardedFor))

	assertStatus(t, recorder, http.StatusForbidden)

	listing := repository.List()
	if len(listing) != 1 {
		t.Fatalf("repository holds %d servers, want 1", len(listing))
	}
	if listing[0].Name != "a server" {
		t.Errorf("name = %q, want the original registration to be intact", listing[0].Name)
	}
}

// Deregistration is equally sensitive: a spoofed header must not be able to
// take a server out of the browser.
func TestRemoveRejectsForwardedForFromUntrustedPeer(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = forwardedFor + ":45677"
	doRequestWithHeaders(router, http.MethodPut, "/servers/"+serverAddr,
		loopbackPeer, validBody, forwarded(forwardedFor))

	recorder := doRequestWithHeaders(router, http.MethodDelete, "/servers/"+serverAddr,
		untrustedPeer, "", forwarded(forwardedFor))

	assertStatus(t, recorder, http.StatusForbidden)

	if !repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a spoofed X-Forwarded-For deregistered a server")
	}
}

func TestRemoveBehindTrustedProxy(t *testing.T) {
	router, repository := newTestRouter()

	const serverAddr = forwardedFor + ":45677"
	doRequestWithHeaders(router, http.MethodPut, "/servers/"+serverAddr,
		loopbackPeer, validBody, forwarded(forwardedFor))

	recorder := doRequestWithHeaders(router, http.MethodDelete, "/servers/"+serverAddr,
		loopbackPeer, "", forwarded(forwardedFor))

	assertStatus(t, recorder, http.StatusOK)
	assertResult(t, recorder, "success")

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("server was not removed through the proxy")
	}
}

func TestRegisterRejectsForwardedForFromUntrustedPeer(t *testing.T) {
	router, repository := newTestRouter()

	// The responder listens on loopback, so an attacker would have to claim a
	// loopback address to register it.
	serverAddr := startUDPResponder(t, "pong")

	recorder := doRequestWithHeaders(router, http.MethodPost, "/servers/"+serverAddr,
		untrustedPeer, validBody, forwarded("127.0.0.1"))

	assertStatus(t, recorder, http.StatusForbidden)

	if repository.Has(srvrepo.ServerID(serverAddr)) {
		t.Error("a spoofed X-Forwarded-For registered a server")
	}
}

// Game servers call this to learn the address they should register under, so
// behind a proxy it has to report the forwarded address. Returning 127.0.0.1
// would leave every server unable to register.
func TestGetIPBehindTrustedProxy(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequestWithHeaders(router, http.MethodGet, "/reflection/ip",
		loopbackPeer, "", forwarded(forwardedFor))

	assertStatus(t, recorder, http.StatusOK)
	assertReflectedIP(t, recorder, forwardedFor)
}

func TestGetIPIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequestWithHeaders(router, http.MethodGet, "/reflection/ip",
		untrustedPeer, "", forwarded(forwardedFor))

	assertStatus(t, recorder, http.StatusOK)
	assertReflectedIP(t, recorder, "198.51.100.7")
}

// A request arriving directly, with no forwarding header, is reported as the
// connection's own address.
func TestGetIPDirectConnection(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequest(router, http.MethodGet, "/reflection/ip", untrustedPeer, "")

	assertStatus(t, recorder, http.StatusOK)
	assertReflectedIP(t, recorder, "198.51.100.7")
}

// With a chain of forwarding hops, the rightmost address that is not a trusted
// proxy is the one to use.
func TestGetIPWithForwardedChain(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequestWithHeaders(router, http.MethodGet, "/reflection/ip",
		loopbackPeer, "", forwarded("192.0.2.9, "+forwardedFor))

	assertStatus(t, recorder, http.StatusOK)
	assertReflectedIP(t, recorder, forwardedFor)
}

// assertReflectedIP checks the address /reflection/ip reported back.
func assertReflectedIP(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()

	var body struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not a JSON object: %v (body: %s)", err, recorder.Body.String())
	}

	if body.IP != want {
		t.Errorf("ip = %q, want %q", body.IP, want)
	}
}
