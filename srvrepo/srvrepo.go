// Package srvrepo defines the public (externally importable) interfaces, types,
// and functions for interacting with the server repository.
package srvrepo

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// ServerRepository defines the high-level interface for interacting with the
// server repository service.
type ServerRepository interface {
	// List returns a slice representation of the servers in the repository.
	List() []Server

	// Register takes a Server and registers it with the repository, returning a
	// bool that represents whether the server already existed or not (true for
	// already exists, false otherwise), and a potential error if the
	// registration failed.
	Register(srv Server) (bool, error)

	// Remove takes a ServerID and removes the corresponding server from the
	// repository, returning a bool that represents whether the server existed
	// or not (true for exists, false otherwise).
	Remove(id ServerID) bool

	// Prune takes a time.Duration representing the threshold of when a server's
	// last-seen "age" should be considered too old, and removes those servers
	// from the repository.
	Prune(threshold time.Duration)
}

// Server defines a structure for our server data.
type Server struct {
	ServerAddress // embedded to flatten the structure

	Name        string `json:"name"`
	GameVersion int    `json:"game_version"`

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
