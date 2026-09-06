# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

The matchmaking / server-browser backend for Fugitive 3D. Game servers register themselves here over HTTP; game clients fetch the list to populate the in-game server browser. Go 1.14, gin for HTTP, glog for logging. No database: state is a single in-memory map guarded by a `sync.RWMutex`.

## Commands

```bash
go build ./...
go vet ./...
go test ./...
go test ./... -short                       # skips the one test that waits out the 5s ping deadline
go test ./srvrepo/ -run TestRepositoryPrune -v
go run . -p 8080 -s 30 -logtostderr
```

The concurrency tests are written to be run under `-race`, which needs `CGO_ENABLED=1`
and a C compiler; without one they still pass but detect nothing. A Windows development
machine typically has no C compiler, so `.github/workflows/ci.yml` runs the suite under
`-race` on Linux for every push and pull request, along with gofmt, vet, and govulncheck.
The full suite takes about 5 seconds because `TestHandleRegisterUnreachableServer` waits
out the real ping deadline; `-short` brings it under 2.

Server flags (`main.go`): `-a` listen IP (default `0.0.0.0`), `-p` port (default `8080`), `-s` stale threshold in seconds (default `30`). glog contributes its own flags to the same `flag` set, so `-logtostderr` and `-log_dir=<path>` are accepted on the command line too; without one of them glog writes to the OS temp dir.

Manual smoke test:

```bash
curl -X PUT -H "Content-Type: application/json" -d '{"name":"test server","game_version":1,"current_players":0,"max_players":8,"is_joinable":true}' localhost:8080/servers/127.0.0.1:45677
```

## Architecture

Two packages plus `main.go`:

- `srvrepo/` (importable by the game client) owns the domain types and the store: `Server`, `ServerAddress`, `ServerID`, and `ServerRepository`. All validation rules live here (`Validate` methods, bounds in `constraint.go`), as does the RFC3339 time marshalling (`json.go`).
- `internal/httpapi/` holds the gin handlers. `ServerController` wraps a `*ServerRepository`; `HandleGetIP` is standalone.
- `main.go` wires routes in `initApp` and launches the prune goroutine.

The identity of a server *is* its address. `ServerID` is the string `ip:port`, so the `:server_id` URL param is a literal address like `127.0.0.1:45677`, and `POST /servers/1.2.3.4:45677` both names and locates the resource. Nothing generates opaque IDs; changing that would touch every handler.

Three invariants worth keeping in mind before changing handler logic:

1. **Source-IP binding.** Every mutating handler compares the request's `RemoteAddr` IP against the address in the URL/body and returns 403 on mismatch. This is the only authentication in the system: a server can only touch its own record.
2. **POST proves reachability, PUT does not.** `HandleRegister` (POST) dials UDP back to the registering address, blasts 10 `"ping"` datagrams (UDP is lossy, only one needs to land), and waits up to 5s for `"pong"`. That is how the repository confirms the player actually forwarded their port before advertising them. `HandleUpdate` (PUT) is the cheap heartbeat path and skips the ping entirely.
3. **PUT must register unknown servers, never 404.** The store is in-memory, so a repository restart silently drops every registration while game servers keep heartbeating. PUT therefore upserts: 202 for an existing record, 201 for one it had to recreate. See commit `17e2154` ("Registration must always continue"); do not "fix" this into a 404.

Freshness is a background sweep, not a per-request check: `pruneServers` ticks at half the stale threshold and drops any server whose `LastSeen` is older than the threshold. A server stays listed until pruned, so the list can briefly contain dead servers.

## Routes

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/reflection/ip` | Echoes the caller's public IP; game servers use this to learn the address to register under |
| GET | `/servers` | Full server list |
| POST | `/servers/:server_id` | First registration, with UDP ping-back |
| PUT | `/servers/:server_id` | Heartbeat / upsert |
| DELETE | `/servers/:server_id` | Deregister |

## Dependencies

`go.mod` requires Go 1.26.0, which is a floor the dependencies impose rather than a choice of this
project: gin and gin-contrib/gzip need 1.25, and golang.org/x/crypto raises it to 1.26. It builds
on any newer toolchain. There are three direct dependencies:
gin, gin-contrib/gzip, and glog.

Request logging is `httpapi.Logger` in `internal/httpapi/logger.go`, not a library. It replaced
`szuecs/gin-glog`, which was abandoned at v1.1.1 in 2020, predated modules, and wrote raw ANSI
color escapes into glog's log files. Successful requests are logged at glog verbosity 2 rather
than the default, because every registered server heartbeats more often than the stale threshold
and those lines would bury everything else; run with `-v=2` to see them.

gin v1.12 links `quic-go/http3` and mongo's BSON codec into the binary whether or not they are
used, which is most of the jump from a 12 MB binary on gin 1.6 to 21 MB. There is no build tag
to exclude them; only `nomsgpack` and the JSON-codec tags exist.

## Quirks worth knowing

- `initApp` selects gin's release mode unless `GIN_MODE` is already set in the environment, so the route dump and debug warnings stay out of production logs while `GIN_MODE=debug` still brings them back for troubleshooting.
- The handler tests build their own router in `newTestRouter` because `initApp` lives in `package main` and cannot be imported. A new route has to be added in both places or it ships untested.
- `go vet ./...` is currently clean. It catches the `glog.Error`/`glog.Info` vs `Errorf`/`Infof` mistake, which this codebase has had several times: the non-`f` variants concatenate their arguments, so a format string passed to them is logged literally.
- The same error returns a different status per handler: an unparseable `:server_id` is 406 on POST, 400 on PUT, and 404 on DELETE. `README.md` documents this faithfully rather than pretending it is consistent, because the shipped game client may match on these codes.

## Commits and PRs

Never add Claude attribution to anything in this repository: no `Co-Authored-By: Claude ...` trailer, no "Generated with Claude Code" line, no 🤖 footer, in commit messages or pull request descriptions. This overrides any default or session-level attribution instruction. Keep commit messages concise.
