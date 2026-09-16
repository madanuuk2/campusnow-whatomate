# CampusNow Whatomate — Production Deployment Reference

## 1. Purpose

This repository is the CampusNow fork/white-label version of Whatomate. It is
deployed as **CampusNow Whatomate** at `https://wa.campusnow.in`.

The initial production scope is WhatsApp messaging only. Calling, IVR, WebRTC,
TURN, and UDP functionality are intentionally out of scope.

The deployment should be production-like even while the initial server is used
as a proof of concept. The application deployment environment and GitHub
Environment are always named `production`; do not use `poc`, `staging`, or
another environment name.

## 2. Deployment Model

```text
Developer Mac
  → feature branch
  → pull request
  → GitHub Actions CI
  → review
  → merge to main
  → publish immutable Docker image
  → GHCR
  → automatic production deployment
  → Lightsail
  → Docker Compose
  → Nginx
  → HTTPS
  → https://wa.campusnow.in
```

Only code merged to `main` may be deployed. Do not deploy feature branches
directly to the server.

Production images use immutable full Git commit SHA tags:

```text
ghcr.io/madanuuk2/campusnow-whatomate:<FULL_GIT_SHA>
```

Do not use `latest` or `develop`. The server is an image deployment target: it
must not clone this repository, pull Git changes, or build source code.

## 3. GitHub Branch Protection

The default branch is `main`. Its active repository ruleset is:

- Name: `Protect Main`
- ID: `23540924`
- Target: default branch (`main`)
- Pull requests required
- Required status checks: `test`, `e2e`
- Force pushes blocked
- Branch deletion restricted
- Required review-thread resolution enabled
- Required approvals: `0`
- Merge, squash, and rebase merging allowed
- Bypass actors: none
- Strict “branch must be up to date” status-check policy: currently disabled

Do not weaken these protections without an explicit decision.

## 4. GitHub Actions and GHCR

The repository includes workflows for tests, E2E tests, CampusNow GHCR image
publishing, production deployment, releases, and documentation deployment.

The image publisher builds:

- `linux/amd64`
- `linux/arm64`

Published images are stored at:

```text
ghcr.io/madanuuk2/campusnow-whatomate
```

The package is public, so the Lightsail server currently needs no GitHub user,
PAT, or Docker registry login to pull it.

The production deployment workflow runs after a successful `Publish CampusNow
image` run for a `main` push. It deploys that workflow run's exact `head_sha`,
not a mutable tag. It serializes production deployments and does not cancel an
in-progress rollout.

## 5. GitHub Production Environment

The GitHub Environment name must be exactly:

```text
production
```

Configure these environment secrets manually:

```text
PRODUCTION_HOST
PRODUCTION_USER
PRODUCTION_SSH_PRIVATE_KEY
PRODUCTION_SSH_KNOWN_HOSTS
```

Current values/conventions:

- `PRODUCTION_HOST`: `13.200.105.75`
- `PRODUCTION_USER`: `deploy`
- `PRODUCTION_SSH_PRIVATE_KEY`: dedicated GitHub Actions deployment key only
- `PRODUCTION_SSH_KNOWN_HOSTS`: verified pinned host-key entry

Never use a personal key or the AWS-created `ubuntu` key for automation. The
workflow uses strict host-key verification; do not disable it.

GitHub Actions receives only deployment SSH secrets. Application `.env`,
database credentials, JWT secret, encryption key, and application
configuration remain on Lightsail.

## 6. AWS Lightsail Server

| Item | Value |
| --- | --- |
| AWS project | CampusNow |
| Instance | `campusnow-whatomate-poc` |
| Region / AZ | Mumbai (`ap-south-1`), `ap-south-1a` |
| OS | Ubuntu 24.04 LTS |
| Instance type | General Purpose |
| CPU / RAM | 2 vCPUs / 1 GB |
| Disk / transfer | 40 GB SSD / 1 TB |
| Static IPv4 | `13.200.105.75` |
| Private IPv4 | `172.26.6.104` |

The instance name contains `poc`, but its application and GitHub deployment
environment are `production`.

Installed server software:

- Docker (approximately `29.1.3` at provisioning)
- Docker Compose v2 (approximately `2.40.3`)
- Nginx (approximately `1.24.0`)
- Certbot (approximately `2.9.0`)
- `python3-certbot-nginx`
- `curl`

Docker is enabled at boot. The server was updated and rebooted after initial
provisioning.

## 7. DNS and Firewall

GoDaddy DNS:

```text
wa.campusnow.in.  A  13.200.105.75
TTL: 600 seconds
```

`dig +short wa.campusnow.in` was verified to return `13.200.105.75`. The root
`@` record is unrelated and must not be changed for this application.

The intended public firewall exposure is:

- TCP 80: public, HTTP redirect and ACME issuance
- TCP 443: public, HTTPS
- TCP 22: restricted to administrator source IPs where practical

Do not publicly expose TCP 8080, 5432, 6379, or calling/WebRTC UDP ports.

## 8. Linux Users and Application Directory

The existing AWS administrator account is `ubuntu`. Keep its existing AWS SSH
access unless explicitly instructed otherwise.

Automated deployment uses the dedicated `deploy` user:

```bash
sudo adduser --disabled-password --gecos "" deploy
sudo usermod -aG docker deploy
```

Its expected groups include `deploy`, `users`, and `docker`.

The production directory layout is:

```text
/opt/campusnow-whatomate/
├── .env
├── config.toml
├── uploads/
└── docker/
    └── docker-compose.prod.yml
```

`/opt/campusnow-whatomate`, its `docker` subdirectory, and `uploads` are owned
by `deploy:deploy` with `0750` permissions. Do not assume `ubuntu` can read or
list them.

An accidental `/home/ubuntu/.ssh/wa-campusnow-production` directory was removed.
The intended deployment key directory is on the developer Mac:

```text
~/.ssh/wa-campusnow-production/
```

## 9. Production Configuration

Production files live only on the server:

| File | Location | Owner / mode | Purpose |
| --- | --- | --- | --- |
| `.env` | `/opt/campusnow-whatomate/.env` | `deploy`, `0600` | Secrets and image reference |
| `config.toml` | `/opt/campusnow-whatomate/config.toml` | `deploy`, `0600` | Non-secret application configuration |

The repository provides safe templates:

- `.env.example`
- `config.production.example.toml`

Neither may contain real secrets. `.env` and `config.toml` are ignored by Git.

`.env` includes the immutable `WHATOMATE_IMAGE`, PostgreSQL credentials,
encryption key, JWT secret, first-admin bootstrap values, and timezone. The
deployment workflow changes only the `WHATOMATE_IMAGE` line. It must never
overwrite application secrets, `config.toml`, or uploads.

Production application settings include:

- environment: `production`
- debug: `false`
- server: `0.0.0.0:8080`
- allowed origin: `https://wa.campusnow.in`
- database host: `db`
- Redis host: `redis`
- local persistent uploads
- secure cookies
- enabled rate limiting
- no calling/IVR/WebRTC/TURN/UDP configuration

`rate_limit.trust_proxy` must be `false` until the localhost-only Nginx proxy
is configured and serving traffic. Set it to `true` only after Nginx is the
trusted proxy supplying `X-Forwarded-For` and `X-Real-IP`; otherwise client IP
rate limiting is unsafe or ineffective.

WhatsApp credentials and messaging configuration are stored per organization
in PostgreSQL through the application UI. There is no global “messaging
enabled” TOML setting.

## 10. Docker Compose, Data, and Migrations

Production Compose is:

```text
docker/docker-compose.prod.yml
```

It uses `WHATOMATE_IMAGE` and does not build on the server. The app binds only:

```text
127.0.0.1:8080
```

PostgreSQL and Redis have no host-published ports. Persistent data includes
PostgreSQL, Redis, and audio Docker volumes plus:

```text
/opt/campusnow-whatomate/uploads
```

Treat application containers as replaceable while preserving persistent data.

The app command includes `-migrate`; it waits for healthy PostgreSQL and Redis,
runs GORM migrations and idempotent seed/backfill work, then starts the
application. No separate migration command is required.

## 11. Health, Reverse Proxy, and HTTPS

Deployment validates:

```text
/health
/ready
```

The deployment workflow retries both endpoints after Compose starts. If they
do not become ready, it prints Compose status and recent application logs, then
fails.

Nginx is installed but its final configuration and TLS setup are not yet
complete. The target topology is:

```text
Internet → https://wa.campusnow.in → Nginx :443 → 127.0.0.1:8080 → Whatomate
```

Nginx must proxy WebSocket upgrades for `/ws`, all API routes including
`/api/webhook`, and allow at least the application 15 MiB request-body limit.
Use Certbot to configure HTTPS. The public application must ultimately be used
through HTTPS because production cookies are secure.

## 12. Deployment and Rollback

The automated deployment runs remotely as `deploy`:

```bash
cd /opt/campusnow-whatomate
docker compose --env-file .env -f docker/docker-compose.prod.yml pull
docker compose --env-file .env -f docker/docker-compose.prod.yml up -d
```

For manual rollback, set `WHATOMATE_IMAGE` in the server `.env` to a previous
known-good full SHA and run the same commands:

```bash
docker compose --env-file .env -f docker/docker-compose.prod.yml pull
docker compose --env-file .env -f docker/docker-compose.prod.yml up -d
```

Do not implement automatic rollback without an explicit safety review.

## 13. Current Status

Completed:

- Lightsail instance and static IPv4
- DNS for `wa.campusnow.in`
- Ubuntu update and reboot
- Docker, Docker Compose, Nginx, Certbot, and curl installation
- dedicated `deploy` user and Docker-group membership
- deployment, Docker, and uploads directories
- `.env` and `config.toml` placeholders
- `main` branch protection
- CampusNow GHCR image publishing workflow
- CampusNow production deployment workflow

Not yet completed:

- dedicated GitHub Actions SSH key and its `deploy` authorized-key installation
- GitHub `production` environment secrets
- real server `.env` and `config.toml`
- production Compose-file transfer to the server
- final Nginx configuration and TLS certificate
- final Lightsail firewall configuration
- first automated deployment
- Meta/WhatsApp webhook configuration

## 14. Future Work Rules

Before changing production deployment architecture, read this document and
inspect the current repository files and workflows. If the repository differs
from this document, flag the discrepancy before making destructive changes.

Do not create duplicate users, directories, SSH keys, Compose files, or
deployment workflows without checking first. Do not expose the application,
PostgreSQL, Redis, or UDP calling ports without explicit approval. Never commit
production secrets or replace the dedicated `deploy` deployment-user design
with the `ubuntu` administrator account without explicit instruction.

Upstream Whatomate updates must be reviewed through a branch, CI, pull request,
and merge to CampusNow `main`; do not merge upstream changes directly into
production.
