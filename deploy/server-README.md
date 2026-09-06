# fugitive-repo on hydra

Backend for the Fugitive 3D in-game server browser. Game servers register
themselves here over HTTP; the game client and the website's stats tile read the
list back.

## Layout

| Path | What |
| --- | --- |
| `~/bin/fugitive-repo` | the running binary |
| `~/deploy/repo` | git checkout, master |
| `~/deploy/redeploy.sh` | pull, rebuild, restart |
| `~/deploy/.deployed-sha` | commit the installed binary was built from |
| `/etc/systemd/system/fugitive-repo.service` | the service |
| `/etc/systemd/system/fugitive-repo-webhook.service` | deploy receiver, port 9190 |
| `/etc/fugitive-repo-webhook/` | webhook secret and hooks.yaml |
| `/etc/nginx/sites-available/repository.fugitivethegame.online` | vhost |

State is entirely in memory. There is no database and nothing to back up: a
restart drops every registration, and live game servers re-register on their
next heartbeat within about thirty seconds.

## Everyday commands

```bash
systemctl status fugitive-repo
journalctl -u fugitive-repo -f              # service log
journalctl -u fugitive-repo-webhook -f      # deploy log
sudo -u fugitive-repo ~fugitive-repo/deploy/redeploy.sh --force
```

Successful requests are not logged. Every registered server heartbeats
continuously, so logging them would bury everything else; errors and rejections
are always logged. `-v=2` in the unit turns the successful ones on, but do not
leave it there.

## Two things that will bite you

**This vhost must stay plain HTTP.** Shipped game clients hardcode
`http://repository.fugitivethegame.online` and cannot be updated. Running
`certbot --nginx` here would add an unconditional redirect to https and break
the server browser for every client already in players' hands.

**The proxy has to pass `X-Forwarded-For`.** Source IP is the only
authentication this service has, and it believes the forwarding header only from
loopback. If the header stops arriving, every registration is compared against
`127.0.0.1` and rejected with a 403 — while `GET /servers` keeps answering
normally. The failure looks like a healthy service with a permanently empty
browser.

To tell the difference, from off the box:

```bash
curl -sS http://repository.fugitivethegame.online/reflection/ip
```

That must echo your own public address. If it says `127.0.0.1`, registration is
broken.
