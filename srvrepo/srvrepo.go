// Package srvrepo defines the public (externally importable) interfaces, types,
// and functions for interacting with the server repository.
package srvrepo

import (
	"fmt"
	"github.com/golang/glog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server defines a structure for our server data.
type Server struct {
	ServerAddress // embedded to flatten the structure

	Name        string `json:"name"`
	GameVersion int    `json:"game_version"`
	IsJoinable bool		`json:"is_joinable"`

	LastSeen jsonTime `json:"last_seen"`
}

// ID returns the ServerID for a server, generated based on its internal data.
func (s *Server) ID() ServerID {
	return ServerID(s.ServerAddress.String())
}

// Seen marks the server as "seen", updating the `LastSeen` property value.
func (s *Server) Seen() {
	s.LastSeen = jsonTime{time.Now()}
}

// Validate runs validations on the value and returns an error if the value is
// invalid for any reason.
func (s *Server) Validate() error {
	if err := s.ServerAddress.Validate(); err != nil {
		return err
	}

	// TODO: We likely should be cleaning/normalizing inputs when unmarshalling,
	// rather than during validation.
	s.Name = strings.TrimSpace(s.Name)
	if nameLength := len(s.Name); nameLength < nameLengthMin || nameLength > nameLengthMax {
		return fmt.Errorf("name length must be within range of %d-%d", nameLengthMin, nameLengthMax)
	}

	return nil
}

// ServerAddress defines the structure of a server address.
type ServerAddress struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`
}

// ParseServerAddress parses a string address into a ServerAddress, returning
// the parsed value and any errors that occurred during parsing.
func ParseServerAddress(s string) (ServerAddress, error) {
	var addr ServerAddress

	rawIP, rawPort, err := net.SplitHostPort(s)
	if err != nil {
		return addr, err
	}

	ip := net.ParseIP(rawIP)
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return addr, fmt.Errorf("invalid port number with err: %w", err)
	}

	addr = ServerAddress{
		IP:   ip,
		Port: port,
	}

	return addr, nil
}

// String satisfies the fmt.Stringer interface and returns a string form of the
// ServerAddress structure.
func (a *ServerAddress) String() string {
	return net.JoinHostPort(
		a.IP.String(),
		strconv.Itoa(a.Port),
	)
}

// Validate runs validations on the value and returns an error if the value is
// invalid for any reason.
func (a *ServerAddress) Validate() error {
	if a.IP.To4() == nil {
		return fmt.Errorf("IP is not a valid IPv4 address")
	}

	if a.Port < portRangeMin || a.Port > portRangeMax {
		return fmt.Errorf("port is not within the valid port range of %d-%d", portRangeMin, portRangeMax)
	}

	return nil
}

// ServerID defines the identifier of a particular server.
type ServerID string

// ServerRepository defines the structure for an in-memory server repository.
type ServerRepository struct {
	servers map[ServerID]Server

	mu sync.RWMutex
}

// NewServerRepository returns a pointer to a new initialized ServerRepository.
func NewServerRepository() *ServerRepository {
	return &ServerRepository{
		servers: make(map[ServerID]Server),
	}
}

// Check if an given ServerID already exists in the repository
func (r *ServerRepository) Has(id ServerID) bool {
	alreadyExists := false

	r.mu.RLock()
	defer r.mu.RUnlock()

	_, alreadyExists = r.servers[id]

	return alreadyExists
}

// List returns a slice representation of the servers in the repository.
func (r *ServerRepository) List() []Server {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serverList := make([]Server, 0, len(r.servers))

	for _, srv := range r.servers {
		serverList = append(serverList, srv)
	}

	return serverList
}

// Register takes a Server and registers it with the repository, returning a
// bool that represents whether the server already existed or not (true for
// already exists, false otherwise), and a potential error if the registration
// failed.
func (r *ServerRepository) Register(srv Server) (bool, error) {
	alreadyExists := false
	var err error

	// TODO: Normalize? Validate?
	id := srv.ID()

	r.mu.Lock()
	defer r.mu.Unlock()

	_, alreadyExists = r.servers[id]
	r.servers[id] = srv

	return alreadyExists, err
}

// Remove takes a ServerID and removes the corresponding server from the
// repository, returning a bool that represents whether the server existed or
// not (true for exists, false otherwise).
func (r *ServerRepository) Remove(id ServerID) bool {
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
func (r *ServerRepository) Prune(threshold time.Duration) {
	cutoff := time.Now().Add(-threshold)

	r.mu.Lock()
	defer r.mu.Unlock()

	for id, srv := range r.servers {
		if srv.LastSeen.Before(cutoff) {
			glog.Infof("Pruning server: %s\n", id)

			delete(r.servers, id)
		}
	}
}
