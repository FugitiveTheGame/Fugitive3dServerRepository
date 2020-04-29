package inmemory

import (
	"fmt"
	"sync"
	"time"

	"github.com/FugitiveTheGame/Fugitive3dServerRepository/srvrepo"
)

// ServerRepository defines the structure for an in-memory server repository.
type ServerRepository struct {
	servers map[srvrepo.ServerID]srvrepo.Server

	mu sync.RWMutex
}

// NewServerRepository returns a pointer to a new initialized ServerRepository.
func NewServerRepository() *ServerRepository {
	return &ServerRepository{
		servers: make(map[srvrepo.ServerID]srvrepo.Server),
	}
}

// List returns a slice representation of the servers in the repository.
func (r *ServerRepository) List() []srvrepo.Server {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serverList := make([]srvrepo.Server, 0, len(r.servers))

	for _, srv := range r.servers {
		serverList = append(serverList, srv)
	}

	return serverList
}

// Register takes a srvrepo.Server and registers it with the repository,
// returning a bool that represents whether the server already existed or not
// (true for already exists, false otherwise), and a potential error if the
// registration failed.
func (r *ServerRepository) Register(srv srvrepo.Server) (bool, error) {
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

// Remove takes a srvrepo.ServerID and removes the corresponding server from the
// repository, returning a bool that represents whether the server existed or
// not (true for exists, false otherwise).
func (r *ServerRepository) Remove(id srvrepo.ServerID) bool {
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
			// TODO: Log with an abstraction
			fmt.Printf("Pruning server: %s\n", id)

			delete(r.servers, id)
		}
	}
}
