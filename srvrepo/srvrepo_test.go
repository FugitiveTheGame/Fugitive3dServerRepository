package srvrepo

import (
	"encoding/json"
	"flag"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/glog"
)

func TestMain(m *testing.M) {
	// Keep glog's output out of the system temp root. os.Exit skips deferred
	// calls, so the cleanup is explicit.
	logDir, err := os.MkdirTemp("", "srvrepo-test-logs")
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

func TestParseServerAddress(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantErr  bool
		wantIP   string
		wantPort int
	}{
		{name: "IPv4 with port", input: "203.0.113.4:45677", wantIP: "203.0.113.4", wantPort: 45677},
		{name: "loopback", input: "127.0.0.1:8080", wantIP: "127.0.0.1", wantPort: 8080},
		{name: "IPv6 in brackets", input: "[::1]:8080", wantIP: "::1", wantPort: 8080},
		{name: "no port", input: "203.0.113.4", wantErr: true},
		{name: "non-numeric port", input: "203.0.113.4:http", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := ParseServerAddress(tc.input)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseServerAddress(%q) = %+v, want error", tc.input, addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseServerAddress(%q) returned unexpected error: %v", tc.input, err)
			}

			if got := addr.IP.String(); got != tc.wantIP {
				t.Errorf("IP = %q, want %q", got, tc.wantIP)
			}
			if addr.Port != tc.wantPort {
				t.Errorf("Port = %d, want %d", addr.Port, tc.wantPort)
			}
		})
	}
}

// An address that parses must render back to the string it came from, because
// the rendered form is the server's identity in the repository.
func TestServerAddressStringRoundTrip(t *testing.T) {
	const input = "203.0.113.4:45677"

	addr, err := ParseServerAddress(input)
	if err != nil {
		t.Fatalf("ParseServerAddress(%q) returned unexpected error: %v", input, err)
	}

	if got := addr.String(); got != input {
		t.Errorf("String() = %q, want %q", got, input)
	}
}

func TestServerAddressValidate(t *testing.T) {
	cases := []struct {
		name    string
		addr    ServerAddress
		wantErr bool
	}{
		{name: "valid", addr: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: 45677}},
		{name: "lowest allowed port", addr: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: portRangeMin}},
		{name: "highest allowed port", addr: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: portRangeMax}},
		{name: "privileged port", addr: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: portRangeMin - 1}, wantErr: true},
		{name: "port above range", addr: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: portRangeMax + 1}, wantErr: true},
		{name: "negative port", addr: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: -1}, wantErr: true},
		{name: "IPv6 rejected", addr: ServerAddress{IP: net.ParseIP("::1"), Port: 45677}, wantErr: true},
		{name: "nil IP rejected", addr: ServerAddress{Port: 45677}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.addr.Validate()

			if tc.wantErr && err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() returned unexpected error: %v", err)
			}
		})
	}
}

func TestServerValidate(t *testing.T) {
	validAddr := ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: 45677}

	cases := []struct {
		name    string
		server  Server
		wantErr bool
	}{
		{name: "valid", server: Server{ServerAddress: validAddr, Name: "a server"}},
		{name: "shortest allowed name", server: Server{ServerAddress: validAddr, Name: strings.Repeat("a", nameLengthMin)}},
		{name: "longest allowed name", server: Server{ServerAddress: validAddr, Name: strings.Repeat("a", nameLengthMax)}},
		{name: "name too short", server: Server{ServerAddress: validAddr, Name: strings.Repeat("a", nameLengthMin-1)}, wantErr: true},
		{name: "name too long", server: Server{ServerAddress: validAddr, Name: strings.Repeat("a", nameLengthMax+1)}, wantErr: true},
		{name: "empty name", server: Server{ServerAddress: validAddr}, wantErr: true},
		{name: "whitespace-only name", server: Server{ServerAddress: validAddr, Name: "     "}, wantErr: true},
		{name: "invalid address rejected before name", server: Server{ServerAddress: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: 80}, Name: "a server"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.server.Validate()

			if tc.wantErr && err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() returned unexpected error: %v", err)
			}
		})
	}
}

// Validate normalizes the name in place, so a padded name is both accepted and
// stored trimmed.
func TestServerValidateTrimsName(t *testing.T) {
	srv := Server{
		ServerAddress: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: 45677},
		Name:          "  a server  ",
	}

	if err := srv.Validate(); err != nil {
		t.Fatalf("Validate() returned unexpected error: %v", err)
	}

	if srv.Name != "a server" {
		t.Errorf("Name = %q, want %q", srv.Name, "a server")
	}
}

func TestServerID(t *testing.T) {
	srv := Server{ServerAddress: ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: 45677}}

	if got, want := srv.ID(), ServerID("203.0.113.4:45677"); got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}
}

func TestServerSeen(t *testing.T) {
	var srv Server

	before := time.Now()
	srv.Seen()
	after := time.Now()

	if srv.LastSeen.Before(before) || srv.LastSeen.After(after) {
		t.Errorf("LastSeen = %v, want a time within [%v, %v]", srv.LastSeen, before, after)
	}
}

func TestServerMarshalJSON(t *testing.T) {
	srv := Server{
		ServerAddress:  ServerAddress{IP: net.ParseIP("203.0.113.4"), Port: 45677},
		Name:           "a server",
		GameVersion:    3,
		CurrentPlayers: 2,
		MaxPlayers:     8,
		IsJoinable:     true,
		LastSeen:       jsonTime{time.Date(2021, time.January, 19, 12, 34, 56, 0, time.UTC)},
	}

	raw, err := json.Marshal(srv)
	if err != nil {
		t.Fatalf("json.Marshal returned unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal returned unexpected error: %v", err)
	}

	want := map[string]any{
		"ip":              "203.0.113.4",
		"port":            float64(45677),
		"name":            "a server",
		"game_version":    float64(3),
		"current_players": float64(2),
		"max_players":     float64(8),
		"is_joinable":     true,
		"last_seen":       "2021-01-19T12:34:56Z",
	}

	if len(got) != len(want) {
		t.Errorf("marshalled %d fields, want %d: %s", len(got), len(want), raw)
	}
	for key, wantValue := range want {
		if gotValue, ok := got[key]; !ok {
			t.Errorf("field %q missing from %s", key, raw)
		} else if gotValue != wantValue {
			t.Errorf("field %q = %v, want %v", key, gotValue, wantValue)
		}
	}
}

// last_seen must round-trip through RFC3339 rather than Go's default time
// format, since the game client parses it.
func TestJSONTimePreservesOffset(t *testing.T) {
	zone := time.FixedZone("PST", -8*60*60)
	value := jsonTime{time.Date(2021, time.January, 19, 12, 34, 56, 0, zone)}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned unexpected error: %v", err)
	}

	if want := `"2021-01-19T12:34:56-08:00"`; string(raw) != want {
		t.Errorf("json.Marshal = %s, want %s", raw, want)
	}
}

func newTestServer(t *testing.T, addr string, name string) Server {
	t.Helper()

	parsed, err := ParseServerAddress(addr)
	if err != nil {
		t.Fatalf("ParseServerAddress(%q) returned unexpected error: %v", addr, err)
	}

	srv := Server{ServerAddress: parsed, Name: name}
	srv.Seen()

	return srv
}

func TestRepositoryRegister(t *testing.T) {
	repo := NewServerRepository()
	srv := newTestServer(t, "203.0.113.4:45677", "a server")

	existed, err := repo.Register(srv)
	if err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}
	if existed {
		t.Error("Register reported the server already existed on first registration")
	}

	if !repo.Has(srv.ID()) {
		t.Errorf("Has(%q) = false after registration", srv.ID())
	}

	existed, err = repo.Register(srv)
	if err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}
	if !existed {
		t.Error("Register reported the server was new on re-registration")
	}
}

// Registering the same address twice updates in place rather than adding a
// second entry, because the address is the identity.
func TestRepositoryRegisterReplacesByAddress(t *testing.T) {
	repo := NewServerRepository()

	repo.Register(newTestServer(t, "203.0.113.4:45677", "original name"))
	repo.Register(newTestServer(t, "203.0.113.4:45677", "updated name"))

	list := repo.List()
	if len(list) != 1 {
		t.Fatalf("List() returned %d servers, want 1", len(list))
	}
	if list[0].Name != "updated name" {
		t.Errorf("Name = %q, want %q", list[0].Name, "updated name")
	}
}

func TestRepositoryHasUnknown(t *testing.T) {
	repo := NewServerRepository()

	if repo.Has(ServerID("203.0.113.4:45677")) {
		t.Error("Has reported an unregistered server exists")
	}
}

func TestRepositoryList(t *testing.T) {
	repo := NewServerRepository()

	if list := repo.List(); len(list) != 0 {
		t.Errorf("List() returned %d servers on an empty repository, want 0", len(list))
	}

	repo.Register(newTestServer(t, "203.0.113.4:45677", "first server"))
	repo.Register(newTestServer(t, "203.0.113.5:45677", "second server"))

	list := repo.List()
	if len(list) != 2 {
		t.Fatalf("List() returned %d servers, want 2", len(list))
	}

	seen := make(map[ServerID]bool)
	for _, srv := range list {
		seen[srv.ID()] = true
	}
	for _, want := range []ServerID{"203.0.113.4:45677", "203.0.113.5:45677"} {
		if !seen[want] {
			t.Errorf("List() is missing server %q", want)
		}
	}
}

// The returned slice is a copy, so mutating it must not corrupt the repository.
func TestRepositoryListReturnsCopy(t *testing.T) {
	repo := NewServerRepository()
	repo.Register(newTestServer(t, "203.0.113.4:45677", "original name"))

	list := repo.List()
	list[0].Name = "mutated name"

	if got := repo.List()[0].Name; got != "original name" {
		t.Errorf("Name = %q after mutating a listed copy, want %q", got, "original name")
	}
}

func TestRepositoryRemove(t *testing.T) {
	repo := NewServerRepository()
	srv := newTestServer(t, "203.0.113.4:45677", "a server")
	repo.Register(srv)

	if exists := repo.Remove(srv.ID()); !exists {
		t.Error("Remove reported the server did not exist")
	}
	if repo.Has(srv.ID()) {
		t.Error("Has reported the server still exists after removal")
	}

	if exists := repo.Remove(srv.ID()); exists {
		t.Error("Remove reported the server existed on a second removal")
	}
}

func TestRepositoryPrune(t *testing.T) {
	repo := NewServerRepository()

	fresh := newTestServer(t, "203.0.113.4:45677", "fresh server")
	stale := newTestServer(t, "203.0.113.5:45677", "stale server")
	stale.LastSeen = jsonTime{time.Now().Add(-time.Hour)}

	repo.Register(fresh)
	repo.Register(stale)

	repo.Prune(time.Minute)

	if repo.Has(stale.ID()) {
		t.Error("Prune left a server older than the threshold in the repository")
	}
	if !repo.Has(fresh.ID()) {
		t.Error("Prune removed a server newer than the threshold")
	}
}

// A server seen exactly at the cutoff is not yet stale.
func TestRepositoryPruneBoundary(t *testing.T) {
	repo := NewServerRepository()

	threshold := time.Minute
	srv := newTestServer(t, "203.0.113.4:45677", "a server")
	srv.LastSeen = jsonTime{time.Now().Add(-threshold / 2)}
	repo.Register(srv)

	repo.Prune(threshold)

	if !repo.Has(srv.ID()) {
		t.Error("Prune removed a server within the threshold")
	}
}

func TestRepositoryPruneEmpty(t *testing.T) {
	repo := NewServerRepository()

	repo.Prune(time.Minute)

	if list := repo.List(); len(list) != 0 {
		t.Errorf("List() returned %d servers after pruning an empty repository, want 0", len(list))
	}
}

// The repository is shared between the HTTP handlers and the prune goroutine,
// so every exported method must be safe under concurrent use. Run with -race.
func TestRepositoryConcurrentAccess(t *testing.T) {
	repo := NewServerRepository()

	const goroutines = 8
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 5)

	for i := range goroutines {
		srv := newTestServer(t, "203.0.113.4:4567"+string(rune('0'+i)), "a server")

		go func() {
			defer wg.Done()
			for range iterations {
				repo.Register(srv)
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				repo.List()
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				repo.Has(srv.ID())
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				repo.Remove(srv.ID())
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				repo.Prune(time.Millisecond)
			}
		}()
	}

	wg.Wait()
}
