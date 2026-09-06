# Fugitive3dServerRepository

The server back end for Fugitive 3D's server browser.

Game servers register themselves here; game clients read the list to populate the
in-game server browser. Registrations are held in memory only and expire: a server
that stops sending heartbeats is pruned once it has not been seen for the stale
threshold (30 seconds by default, set with `-s`).

## Running

```
go run . -a 0.0.0.0 -p 8080 -s 30 -logtostderr
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-a` | `0.0.0.0` | IP address to listen on |
| `-p` | `8080` | TCP port to listen on |
| `-s` | `30` | Seconds before a server is considered stale and pruned |

Logging is glog, so `-logtostderr` and `-log_dir=<path>` are also accepted.

## Endpoints

A server is identified by its own address, so `:server_id` is a literal
`ip:port` string, for example `/servers/203.0.113.4:45677`. The IP must be IPv4
and the port must be in the range 1024-65535.

### `GET /reflection/ip`

Returns the caller's public IP, so a game server can learn the address it should
register under.

```
200 { "ip": "203.0.113.4" }
```

### `GET /servers`

Returns the full list of currently registered servers.

```
200 [
  {
    "ip": "203.0.113.4",
    "port": 45677,
    "name": "special server",
    "game_version": 1,
    "current_players": 2,
    "max_players": 8,
    "is_joinable": true,
    "last_seen": "2021-01-19T12:34:56Z"
  }
]
```

### `POST /servers/{ip:port}`

First-time registration. Before accepting the server, the repository dials UDP
back to the address being registered and waits up to 5 seconds for the game
server to answer a `ping` with `pong`. This confirms the port is actually
reachable from the outside.

The request body carries the server metadata:

```
{
  "name": "special server",
  "game_version": 1,
  "current_players": 0,
  "max_players": 8,
  "is_joinable": true
}
```

| Status | Body | Cause |
| --- | --- | --- |
| 200 | `{"result": "registration complete"}` | Registered |
| 400 | `{"result": "invalid request JSON"}` | Body did not parse |
| 400 | `{"result": "<validation error>"}` | Name length or address out of range |
| 403 | `{"result": "request IP address does not match client IP address"}` | Registering an address you are not calling from |
| 406 | `{"result": "invalid server ID"}` | `:server_id` is not a valid `ip:port` |
| 406 | `{"result": "Bad ping response"}` | Reply was not `pong` |
| 412 | `{"result": "Repository could not ping you."}` | Could not open a UDP socket to the address |
| 504 | `{"result": "no ping response received, is your port not properly forwarded?"}` | No reply within 5 seconds |
| 500 | `{"result": "internal server error"}` | Registration failed |

### `PUT /servers/{ip:port}`

Heartbeat. Refreshes the last-seen timestamp and metadata, and must be called
more often than the stale threshold to stay listed. There is no ping-back on
this path.

This is an upsert, not an update: the repository keeps its registrations in
memory, so a restart drops every server while game servers are still
heartbeating. A `PUT` for a server the repository does not know about therefore
recreates it rather than failing.

| Status | Body | Cause |
| --- | --- | --- |
| 202 | `{"result": "updated"}` | Existing registration refreshed |
| 201 | `{"result": "registered"}` | Server was not known, so it was recreated |
| 400 | `{"result": "invalid request JSON"}` | Body did not parse |
| 400 | `{"result": "invalid server ID"}` | `:server_id` is not a valid `ip:port` |
| 400 | `{"result": "<validation error>"}` | Name length or address out of range |
| 403 | `{"result": "request IP address does not match client IP address"}` | Updating an address you are not calling from |
| 500 | `{"result": "internal server error"}` | Registration failed |

### `DELETE /servers/{ip:port}`

Deregisters a server.

| Status | Body | Cause |
| --- | --- | --- |
| 200 | `{"result": "success"}` | Removed |
| 403 | `{"result": "request IP address does not match client IP address"}` | Removing an address you are not calling from |
| 404 | `{"result": "invalid server ID"}` | `:server_id` is not a valid `ip:port` |
| 404 | `{"result": "failure"}` | No such server |

## Validation

- `ip` must be IPv4, `port` must be within 1024-65535.
- `name` is trimmed of surrounding whitespace and must then be 3-32 characters.
- Every mutating call must originate from the IP it is registering, updating, or
  removing; this is the only authentication in the system.
