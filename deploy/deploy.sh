#!/usr/bin/env bash
#
# Build locally, ship artifacts, restart. ~20 seconds end to end.
#
# The server has no Go and no Node on purpose: it runs live MT5 trading and
# does not need a build toolchain, a compiler cache, or a Docker daemon
# competing for its 4GB of RAM. We cross-compile a static binary and a static
# bundle here, and rsync the results.
#
# Usage:  ./deploy/deploy.sh [--skip-tests] [--api-only|--web-only]
set -euo pipefail

HOST=${DEPLOY_HOST:-trader@68.183.91.240}
APP_DIR=/opt/braelaspin
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export PATH=/home/rock/.local/go/bin:$PATH
export GOPATH=${GOPATH:-/home/rock/.local/gopath}

SKIP_TESTS=0; DO_API=1; DO_WEB=1
for arg in "$@"; do
  case $arg in
    --skip-tests) SKIP_TESTS=1 ;;
    --api-only)   DO_WEB=0 ;;
    --web-only)   DO_API=0 ;;
    *) echo "unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log()  { printf '\n\033[1;33m▸ %s\033[0m\n' "$*"; }
ok()   { printf '  \033[0;32m✓\033[0m %s\n' "$*"; }
fail() { printf '  \033[0;31m✗ %s\033[0m\n' "$*"; exit 1; }

cd "$ROOT"
VERSION=$(git rev-parse --short HEAD 2>/dev/null || date +%s)

# ── tests ───────────────────────────────────────────────────────────────────
# Deploying money code that has not been tested is not a shortcut worth having.
if [ $SKIP_TESTS -eq 0 ]; then
  log "Running tests"
  go vet ./... || fail "go vet failed"
  if docker compose -f deploy/docker-compose.yml ps 2>/dev/null | grep -q healthy; then
    TEST_DATABASE_URL="postgres://braelaspin:devpassword@localhost:5433/braelaspin_test?sslmode=disable" \
      go test ./... -count=1 >/dev/null || fail "go tests failed"
    ok "go tests pass (incl. integration)"
  else
    go test ./... -count=1 >/dev/null || fail "go tests failed"
    ok "go tests pass (unit only — local db not running)"
  fi
  (cd web && npm run typecheck >/dev/null && node scripts/check-core-purity.mjs >/dev/null \
     && npx vitest run >/dev/null 2>&1) || fail "web checks failed"
  ok "web typecheck, core purity and tests pass"
fi

# ── build ───────────────────────────────────────────────────────────────────
BUILT=()
if [ $DO_API -eq 1 ]; then
  log "Building the API (static, linux/amd64)"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -ldflags="-s -w -X main.version=$VERSION" \
    -o build/braelaspin ./cmd/api
  ok "build/braelaspin  $(du -h build/braelaspin | cut -f1)  version=$VERSION"
  BUILT+=(api)
fi

if [ $DO_WEB -eq 1 ]; then
  log "Building the web app"
  # Empty base = same origin. Caddy serves the app and proxies /v1 to the API,
  # so the browser never makes a cross-origin request in production.
  (cd web && VITE_API_BASE="" npx vite build >/dev/null) || fail "vite build failed"
  ok "web/dist  $(du -sh web/dist | cut -f1)"
  BUILT+=(web)
fi

# ── ship ────────────────────────────────────────────────────────────────────
log "Shipping to $HOST"

if [ $DO_API -eq 1 ]; then
  # Upload beside the running binary, then swap — a partially-uploaded binary
  # must never be what systemd tries to start.
  rsync -az --info=none build/braelaspin "$HOST:$APP_DIR/bin/braelaspin.new"
  ok "binary uploaded"
fi

if [ $DO_WEB -eq 1 ]; then
  # --delete so removed assets do not linger and get served by a stale
  # index.html reference.
  rsync -az --delete --info=none web/dist/ "$HOST:$APP_DIR/web/"
  ok "web bundle uploaded"
fi

# ── activate ────────────────────────────────────────────────────────────────
log "Activating"
ssh "$HOST" APP_DIR=$APP_DIR VERSION="$VERSION" DID_API=$DO_API 'bash -s' <<'REMOTE'
set -euo pipefail
cd "$APP_DIR"

if [ "$DID_API" -eq 1 ]; then
  # Keep the outgoing binary so a rollback is one command.
  if [ -f bin/braelaspin ]; then
    cp bin/braelaspin "releases/braelaspin.$(date +%Y%m%d-%H%M%S)"
    ls -1t releases/ | tail -n +6 | xargs -r -I{} rm -f "releases/{}"
  fi
  chmod +x bin/braelaspin.new
  mv -f bin/braelaspin.new bin/braelaspin

  set -a; . ./.env; set +a
  ./bin/braelaspin migrate up
  sudo systemctl restart braelaspin
fi

sudo systemctl reload caddy 2>/dev/null || sudo systemctl restart caddy

# Wait for readiness rather than assuming it.
for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    echo "  api healthy: $(curl -s http://127.0.0.1:8080/healthz)"
    break
  fi
  [ "$i" -eq 30 ] && { echo "  API DID NOT COME UP"; sudo journalctl -u braelaspin -n 30 --no-pager; exit 1; }
  sleep 1
done
REMOTE
ok "services restarted"

# ── verify from outside ─────────────────────────────────────────────────────
log "Verifying from the public internet"
# Canonical domain first, then the bare IP, which stays supported.
for BASE in "https://braelaspin.dafeapp.com" "http://${HOST#*@}"; do
  for path in /healthz /; do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 "$BASE$path" || echo 000)
    [ "$code" = "200" ] || fail "GET $BASE$path returned $code"
  done
  ok "$BASE  healthz+app 200"
done

# This host runs live trading: assert we did not disturb it.
TERMS=$(ssh "$HOST" 'pgrep -fc terminal64.exe || echo 0')
[ "$TERMS" -ge 1 ] || fail "MT5 terminals are not running after deploy"
ok "MT5 intact ($TERMS terminals running)"

printf '\n\033[0;32m  Deployed %s → https://braelaspin.dafeapp.com\033[0m\n' "$VERSION"
printf '  logs:     ssh %s "sudo journalctl -u braelaspin -f"\n' "$HOST"
printf '  rollback: ssh %s "ls %s/releases"\n\n' "$HOST" "$APP_DIR"
