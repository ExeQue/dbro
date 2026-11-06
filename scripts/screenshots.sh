#!/usr/bin/env bash
# Regenerates assets/screenshots/*.png: builds dbro, serves it headless with a demo database
# and a throwaway home and config, and clicks through it in Chromium.
# Needs Go, Node, Python 3 and Chromium or Chrome (set CHROME to pick one).
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
pid=""
trap '[ -n "$pid" ] && kill "$pid" 2>/dev/null; rm -rf "$tmp"' EXIT
port=${PORT:-7899}

(cd "$root/frontend" && npm ci --silent && npm run build >/dev/null)
go build -C "$root" -tags webkit2_41,production -o "$tmp/dbro" .

mkdir -p "$tmp/home/projects/shop" "$tmp/config" "$tmp/state"
python3 "$root/scripts/screenshots/demo.py" "$tmp/home/projects/shop/database.sqlite"
cat > "$tmp/config/connections.toml" <<TOML
[[connection]]
name = "shop"
driver = "sqlite"
file = "$tmp/home/projects/shop/database.sqlite"
color = "green"
TOML
cat > "$tmp/config/config.toml" <<'TOML'
theme = "dbro-dark"
omarchy_asked = true
first_run_done = true
lerd = false
lerd_auto_connect = false
TOML

HOME="$tmp/home" DBRO_CONFIG_DIR="$tmp/config" DBRO_STATE_DIR="$tmp/state" "$tmp/dbro" serve --addr "127.0.0.1:$port" 2>/dev/null &
pid=$!
until curl -fs "http://127.0.0.1:$port/" >/dev/null; do sleep 0.2; done

mkdir -p "$root/assets/screenshots"
cd "$root/scripts/screenshots"
npm ci --silent
node shoot.mjs "http://127.0.0.1:$port" "$root/assets/screenshots"
