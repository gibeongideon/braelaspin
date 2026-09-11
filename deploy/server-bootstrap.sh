#!/usr/bin/env bash
#
# One-time server preparation. Idempotent — safe to re-run.
#
# ╔══════════════════════════════════════════════════════════════════════════╗
# ║  THIS HOST RUNS LIVE MT5 TRADING SERVICES. DO NOT DISTURB THEM.          ║
# ║                                                                          ║
# ║  Wine + 3 MetaTrader terminals, rpyc on 127.0.0.1:18812-18814,          ║
# ║  x11vnc on :5900/:5902, and USER systemd units:                          ║
# ║      mt5-terminal*.service  xvfb*.service                                ║
# ║      fp10k-*.service/.timer  ftmo-*.service/.timer                       ║
# ║                                                                          ║
# ║  Rules this script follows, and any future change must too:              ║
# ║   1. Our units are SYSTEM-scoped; MT5's are USER-scoped. No overlap.     ║
# ║   2. We never run `systemctl --user` anything.                           ║
# ║   3. Our ports (80/443/8080/5432/6379) avoid every MT5 port.             ║
# ║   4. Postgres and Redis bind to 127.0.0.1 ONLY.                          ║
# ║   5. Hard memory caps, so a runaway API can never starve a trade.        ║
# ║   6. No Docker: its iptables/FORWARD changes are an unnecessary risk      ║
# ║      next to a live trading system, and the daemon costs ~100MB we        ║
# ║      do not have.                                                        ║
# ╚══════════════════════════════════════════════════════════════════════════╝
set -euo pipefail

APP=braelaspin
APP_DIR=/opt/$APP
DB_NAME=braelaspin
DB_USER=braelaspin

log() { printf '\n\033[1;33m▸ %s\033[0m\n' "$*"; }
ok()  { printf '  \033[0;32m✓\033[0m %s\n' "$*"; }

# ── 0. refuse to run if we would clash with MT5 ─────────────────────────────
log "Pre-flight: confirming we will not disturb MT5"
for port in 18812 18813 18814 22346 5900 5902; do
  if ss -tln 2>/dev/null | grep -q ":$port "; then
    ok "MT5 port $port in use — leaving alone"
  fi
done
for p in 80 443 8080; do
  if ss -tln 2>/dev/null | grep -qE "(^|\s)[0-9.]*:$p\s"; then
    echo "  ✗ port $p is ALREADY IN USE — refusing to continue" >&2
    ss -tlnp 2>/dev/null | grep ":$p " >&2
    exit 1
  fi
done
ok "ports 80/443/8080 are free"

# ── 1. packages ─────────────────────────────────────────────────────────────
log "Installing postgresql, redis, caddy (no build toolchain — we ship artifacts)"
export DEBIAN_FRONTEND=noninteractive

if ! command -v psql >/dev/null 2>&1; then
  sudo apt-get update -qq
  sudo apt-get install -y -qq postgresql postgresql-contrib >/dev/null
fi
ok "postgresql $(psql --version | awk '{print $3}')"

if ! command -v redis-server >/dev/null 2>&1; then
  sudo apt-get install -y -qq redis-server >/dev/null
fi
ok "redis $(redis-server --version | awk '{print $3}' | cut -d= -f2)"

if ! command -v caddy >/dev/null 2>&1; then
  sudo apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl >/dev/null
  curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key \
    | sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt \
    | sudo tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
  sudo apt-get update -qq
  sudo apt-get install -y -qq caddy >/dev/null
fi
ok "caddy $(caddy version | head -1)"

# ── 2. keep the databases small; this box is shared with MT5 ────────────────
log "Constraining Postgres and Redis memory"
PG_CONF=$(sudo -u postgres psql -tAc 'SHOW config_file')
sudo sed -i \
  -e "s/^#\?shared_buffers.*/shared_buffers = 128MB/" \
  -e "s/^#\?max_connections.*/max_connections = 50/" \
  -e "s/^#\?work_mem.*/work_mem = 4MB/" \
  -e "s/^#\?effective_cache_size.*/effective_cache_size = 512MB/" \
  -e "s/^#\?listen_addresses.*/listen_addresses = 'localhost'/" \
  "$PG_CONF"
ok "postgres: 128MB buffers, 50 conns, localhost-only"

sudo sed -i \
  -e "s/^#\?maxmemory .*/maxmemory 128mb/" \
  -e "s/^#\?maxmemory-policy.*/maxmemory-policy noeviction/" \
  -e "s/^bind .*/bind 127.0.0.1 -::1/" \
  /etc/redis/redis.conf
# noeviction is deliberate: silently dropping a refresh token logs a user out,
# and dropping an idempotency key can double-charge a bet.
ok "redis: 128mb cap, noeviction, localhost-only"

sudo systemctl enable --now postgresql redis-server >/dev/null 2>&1 || true
sudo systemctl restart postgresql redis-server
ok "postgres + redis running"

# ── 3. database ─────────────────────────────────────────────────────────────
log "Provisioning the database"
if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='$DB_USER'" | grep -q 1; then
  DB_PASS=$(openssl rand -hex 24)
  sudo -u postgres psql -qc "CREATE ROLE $DB_USER LOGIN PASSWORD '$DB_PASS'"
  sudo -u postgres psql -qc "CREATE DATABASE $DB_NAME OWNER $DB_USER"
  sudo mkdir -p $APP_DIR
  echo "$DB_PASS" | sudo tee $APP_DIR/.dbpass >/dev/null
  sudo chmod 600 $APP_DIR/.dbpass
  ok "created role + database (password stored at $APP_DIR/.dbpass)"
else
  ok "database already provisioned"
fi

# ── 4. layout ───────────────────────────────────────────────────────────────
log "Creating $APP_DIR"
sudo mkdir -p $APP_DIR/{bin,web,releases}
sudo chown -R "$USER":"$USER" $APP_DIR
ok "$APP_DIR ready"

# ── 5. config ───────────────────────────────────────────────────────────────
if [ ! -f $APP_DIR/.env ]; then
  log "Generating $APP_DIR/.env"
  DB_PASS=$(sudo cat $APP_DIR/.dbpass)
  cat > $APP_DIR/.env <<ENVEOF
# Generated by server-bootstrap.sh. Secrets — never commit.
APP_ENV=staging
HTTP_ADDR=127.0.0.1:8080
BASE_URL=http://68.183.91.240
LOG_LEVEL=info
LOG_FORMAT=json
TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128

DATABASE_URL=postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable
DB_MAX_CONNS=10
DB_MIN_CONNS=2
MIGRATE_ON_BOOT=false

REDIS_URL=redis://127.0.0.1:6379/0
REDIS_POOL_SIZE=10

JWT_SECRET=$(openssl rand -base64 48 | tr -d '\n')
JWT_ISSUER=braelaspin
ACCESS_TOKEN_TTL=15m
REFRESH_TOKEN_TTL=720h
ARGON2_TIME=3
ARGON2_MEMORY_KIB=65536
ARGON2_PARALLELISM=2

RTP_BP=9000
RAKE_BP=500
REFERRAL_BP=200
MIN_STAKE_CENTS=500
MAX_STAKE_CENTS=5000000
DEMO_GRANT_CENTS=500000
DEMO_TOPUP_COOLDOWN=24h

WITHDRAW_FEE_CENTS=3000
MIN_WITHDRAW_CENTS=10000
MAX_WITHDRAW_CENTS=7000000
AUTO_APPROVE_CEILING_CENTS=0

RL_LOGIN_PER_5MIN=5
RL_REGISTER_PER_HOUR=20
RL_REFRESH_PER_MIN=30
RL_SPIN_PER_MIN=60
RL_READ_PER_MIN=120
RL_DEPOSIT_PER_5MIN=5

# Caddy serves the web app and the API on ONE origin, so no cross-origin
# request exists in production and the allowlist is only for local dev.
CORS_ORIGINS=http://localhost:5173

MPESA_ENV=sandbox
MPESA_BASE_URL=https://sandbox.safaricom.co.ke
MPESA_CALLBACK_SECRET=$(openssl rand -hex 32)
MPESA_CALLBACK_ALLOWED_CIDRS=196.201.214.0/24,196.201.213.0/24,196.201.212.0/24
MPESA_CALLBACK_IP_ENFORCE=false
MPESA_VERIFY_THRESHOLD_CENTS=500000
MPESA_DIAL_SHORTCODE=*334#
MPESA_MANUAL_TILL=

# Placeholders until real Daraja credentials arrive (M6/M7).
MPESA_C2B_CONSUMER_KEY=sandbox-placeholder
MPESA_C2B_CONSUMER_SECRET=sandbox-placeholder
MPESA_C2B_SHORTCODE=174379
MPESA_C2B_PASSKEY=sandbox-placeholder
MPESA_C2B_TRANSACTION_TYPE=CustomerPayBillOnline
MPESA_B2C_CONSUMER_KEY=sandbox-placeholder
MPESA_B2C_CONSUMER_SECRET=sandbox-placeholder
MPESA_B2C_SHORTCODE=600000
MPESA_B2C_INITIATOR_NAME=testapi
MPESA_B2C_INITIATOR_PASSWORD=sandbox-placeholder
MPESA_B2C_CERT_PATH=$APP_DIR/mpesa_sandbox.cer
MPESA_B2C_COMMAND_ID=BusinessPayment

WORKER_ENABLED=true
ENVEOF
  chmod 600 $APP_DIR/.env
  ok ".env generated with fresh secrets"
else
  ok ".env already exists — left untouched"
fi

# ── 6. systemd (SYSTEM scope — MT5 uses USER scope) ─────────────────────────
log "Installing the braelaspin service"
sudo tee /etc/systemd/system/$APP.service >/dev/null <<UNITEOF
[Unit]
Description=braelaspin API
Documentation=https://github.com/gibeongideon/braelaspin
After=network-online.target postgresql.service redis-server.service
Wants=network-online.target
Requires=postgresql.service redis-server.service

[Service]
Type=simple
User=$USER
WorkingDirectory=$APP_DIR
EnvironmentFile=$APP_DIR/.env
ExecStart=$APP_DIR/bin/$APP serve
Restart=always
RestartSec=3

# This box runs live trading. A runaway API must never starve MT5:
# the kernel kills us first.
MemoryMax=384M
MemoryHigh=256M
CPUWeight=50
OOMScoreAdjust=500

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=$APP_DIR
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true

StandardOutput=journal
StandardError=journal
SyslogIdentifier=$APP

[Install]
WantedBy=multi-user.target
UNITEOF
sudo systemctl daemon-reload
ok "braelaspin.service installed (MemoryMax=384M, CPUWeight=50)"

# ── 7. caddy ────────────────────────────────────────────────────────────────
log "Configuring Caddy"
sudo tee /etc/caddy/Caddyfile >/dev/null <<'CADDYEOF'
# ── shared site definition ───────────────────────────────────────────────────
# Defined once and imported by both the domain and the bare-IP site, so the two
# can never drift apart.
(braelaspin) {
	encode gzip zstd

	# API, health and webhooks -> the Go service on loopback.
	# Same origin as the web app, so production has no CORS surface at all.
	@api path /v1/* /healthz /readyz /webhooks/*
	handle @api {
		reverse_proxy 127.0.0.1:8080
	}

	# Everything else -> the static web app.
	handle {
		root * /opt/braelaspin/web
		try_files {path} /index.html
		file_server

		header /assets/* Cache-Control "public, max-age=31536000, immutable"
		# Negated matcher, because try_files rewrites internally and a request
		# for "/" never matches a literal "/index.html".
		@html not path /assets/*
		header @html Cache-Control "no-cache, must-revalidate"

		header {
			X-Content-Type-Options nosniff
			X-Frame-Options DENY
			Referrer-Policy strict-origin-when-cross-origin
			-Server
		}
	}

	log {
		output file /var/log/caddy/braelaspin.log {
			roll_size 10mb
			roll_keep 5
		}
	}
}

# ── canonical: the domain, with automatic TLS ────────────────────────────────
# Caddy obtains and renews a Let's Encrypt certificate on its own. Until the
# DNS A record exists it will retry in the background; the bare-IP site below
# keeps working throughout.
braelaspin.dafeapp.com {
	import braelaspin
	header Strict-Transport-Security "max-age=31536000; includeSubDomains"
}

# ── kept working: the bare IP, plain HTTP ────────────────────────────────────
# A public CA will not issue a certificate for a bare IP, so this is HTTP only.
# It stays as a fallback and for testing. Caddy routes by Host, so a request
# arriving for the domain matches the block above; anything else lands here.
:80 {
	import braelaspin
}
CADDYEOF
sudo mkdir -p /var/log/caddy && sudo chown caddy:caddy /var/log/caddy
sudo systemctl enable caddy >/dev/null 2>&1 || true
ok "Caddyfile written (same-origin: / = web, /v1 = api)"

# ── 8. firewall — open ONLY what we need ────────────────────────────────────
log "Opening 80/443 (22 stays, everything else stays closed)"
sudo ufw allow 80/tcp  >/dev/null 2>&1 || true
sudo ufw allow 443/tcp >/dev/null 2>&1 || true
ok "$(sudo ufw status | grep -cE '^(80|443)/tcp') rule(s) added"

log "Bootstrap complete"
echo
echo "  MT5 untouched — verify with: systemctl --user list-units 'mt5-*' 'fp10k-*' 'ftmo-*'"
echo "  Next: run deploy/deploy.sh from your workstation."
