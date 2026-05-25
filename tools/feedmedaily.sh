#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ACTION="${1:-}"
NODE_BIN="${NODE_BIN:-node}"
GO_BIN="${GO_BIN:-go}"

usage() {
  cat <<'EOF'
Usage: tools/feedmedaily.sh <serve|open|sync|paths>

Commands:
  serve   Start or reuse the local FeedMeDaily daemon.
  open    Start or reuse the daemon, then open the local Web UI.
  sync    Start or reuse the daemon, then trigger one sync job and stream its status.
  paths   Print the key source-mode paths and the recommended cron sync command.
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

load_config_json() {
  "$NODE_BIN" - "$ROOT_DIR" <<'NODE'
const fs = require("fs");
const path = require("path");

const root = path.resolve(process.argv[2]);
const envPath = path.join(root, ".env");

function trimEnvValue(value) {
  const clean = String(value || "").trim();
  if (clean.length >= 2) {
    const first = clean[0];
    const last = clean[clean.length - 1];
    if ((first === "'" && last === "'") || (first === '"' && last === '"')) {
      return clean.slice(1, -1).replace(/\\'/g, "'").replace(/\\"/g, '"');
    }
  }
  return clean;
}

function parseDotEnv(filePath) {
  try {
    const text = fs.readFileSync(filePath, "utf8");
    const values = {};
    for (const rawLine of text.split(/\r?\n/)) {
      const line = rawLine.trim();
      if (!line || line.startsWith("#")) {
        continue;
      }
      const index = line.indexOf("=");
      if (index < 0) {
        continue;
      }
      const key = line.slice(0, index).trim();
      const value = line.slice(index + 1);
      values[key] = trimEnvValue(value);
    }
    return values;
  } catch (_error) {
    return {};
  }
}

function readValue(values, key, fallback) {
  const fromEnv = process.env[key];
  if (typeof fromEnv === "string" && fromEnv.trim() !== "") {
    return fromEnv.trim();
  }
  const fromFile = values[key];
  if (typeof fromFile === "string" && fromFile.trim() !== "") {
    return fromFile.trim();
  }
  return fallback;
}

const dotenv = parseDotEnv(envPath);
const host = readValue(dotenv, "SCIRSS_SERVER_HOST", "127.0.0.1");
const parsedPort = Number.parseInt(readValue(dotenv, "SCIRSS_SERVER_PORT", "8000"), 10);
const port = Number.isInteger(parsedPort) && parsedPort > 0 ? parsedPort : 8000;

process.stdout.write(JSON.stringify({
  root_dir: root,
  host,
  port,
  server_url: `http://${host}:${port}`,
  config_dir: root,
  data_dir: path.join(root, "data"),
  logs_dir: path.join(root, "logs"),
  runtime_state_path: path.join(root, "runtime.json"),
  script_path: path.join(root, "tools", "feedmedaily.sh"),
}));
NODE
}

json_field() {
  "$NODE_BIN" -e 'const data = JSON.parse(process.argv[1]); const key = process.argv[2]; const value = data[key]; if (value === undefined || value === null) process.exit(0); process.stdout.write(String(value));' "$CONFIG_JSON" "$1"
}

runtime_field() {
  if [[ ! -f "$RUNTIME_STATE_PATH" ]]; then
    return 1
  fi
  "$NODE_BIN" -e 'const fs = require("fs"); const filePath = process.argv[1]; const key = process.argv[2]; try { const data = JSON.parse(fs.readFileSync(filePath, "utf8")); const value = data[key]; if (value === undefined || value === null) process.exit(1); process.stdout.write(String(value)); } catch (_error) { process.exit(1); }' "$RUNTIME_STATE_PATH" "$1"
}

healthcheck() {
  curl -fsS --max-time 2 "$1/api/app/health" >/dev/null 2>&1
}

discover_base_url() {
  local runtime_port
  if runtime_port="$(runtime_field port 2>/dev/null)"; then
    local runtime_url="http://${HOST}:${runtime_port}"
    if healthcheck "$runtime_url"; then
      printf '%s\n' "$runtime_url"
      return 0
    fi
  fi
  if healthcheck "$SERVER_URL"; then
    printf '%s\n' "$SERVER_URL"
    return 0
  fi
  return 1
}

start_daemon() {
  mkdir -p "$LOGS_DIR"
  echo "Starting FeedMeDaily daemon at $SERVER_URL" >&2
  nohup "$GO_BIN" run ./cmd/feedmedailyd --root "$ROOT_DIR" --host "$HOST" --port "$PORT" >>"$DAEMON_LOG_PATH" 2>&1 &
  local daemon_launcher_pid=$!
  for _ in $(seq 1 60); do
    if healthcheck "$SERVER_URL"; then
      printf '%s\n' "$SERVER_URL"
      return 0
    fi
    sleep 0.5
  done
  echo "FeedMeDaily daemon did not become healthy in time. Check $DAEMON_LOG_PATH" >&2
  if kill -0 "$daemon_launcher_pid" >/dev/null 2>&1; then
    wait "$daemon_launcher_pid" || true
  fi
  return 1
}

ensure_service() {
  local existing_url
  if existing_url="$(discover_base_url)"; then
    printf '%s\n' "$existing_url"
    return 0
  fi
  start_daemon
}

launch_sync_job() {
  local base_url="$1"
  local response
  response="$(curl -fsS -X POST "$base_url/api/admin/run")"
  "$NODE_BIN" -e 'const payload = JSON.parse(process.argv[1]); const job = payload && payload.job; if (!job || !job.id) { process.exit(1); } process.stdout.write(String(job.id));' "$response"
}

job_snapshot() {
  "$NODE_BIN" -e 'const job = JSON.parse(process.argv[1]); const fields = [job.status || "", job.message || "", job.error || "", String(job.warning_count ?? ""), String(job.verification_required ?? false), job.verification_feed_url || ""]; process.stdout.write(fields.map((value) => String(value).replace(/[\t\r\n]+/g, " ")).join("\t"));' "$1"
}

poll_sync_job() {
  local base_url="$1"
  local job_id="$2"
  local last_signature=""
  while true; do
    local response
    response="$(curl -fsS "$base_url/api/admin/jobs/$job_id")"
    local status message error_text warning_count verification_required verification_feed_url
    IFS=$'\t' read -r status message error_text warning_count verification_required verification_feed_url <<<"$(job_snapshot "$response")"
    local signature="${status}|${message}|${error_text}|${warning_count}|${verification_required}|${verification_feed_url}"
    if [[ "$signature" != "$last_signature" ]]; then
      case "$status" in
        queued|running)
          echo "[$status] ${message:-Sync job is running.}"
          ;;
        waiting_for_user)
          echo "[$status] ${message:-A protected feed requires manual verification.}"
          if [[ -n "$verification_feed_url" ]]; then
            echo "           feed: $verification_feed_url"
          fi
          ;;
        completed)
          echo "[$status] ${message:-Sync completed.}"
          ;;
        failed)
          echo "[$status] ${error_text:-Sync failed.}" >&2
          ;;
        *)
          echo "[$status] ${message:-Job state updated.}"
          ;;
      esac
      last_signature="$signature"
    fi
    case "$status" in
      completed)
        return 0
        ;;
      failed)
        return 1
        ;;
    esac
    sleep 2
  done
}

print_paths() {
  cat <<EOF
Root: $ROOT_DIR
Config: $CONFIG_DIR
Data: $DATA_DIR
Logs: $LOGS_DIR
Runtime state: $RUNTIME_STATE_PATH
Preferred server URL: $SERVER_URL
Recommended cron sync command: bash $SCRIPT_PATH sync
EOF
}

require_command "$NODE_BIN"
require_command curl
require_command "$GO_BIN"

CONFIG_JSON="$(load_config_json)"
HOST="$(json_field host)"
PORT="$(json_field port)"
SERVER_URL="$(json_field server_url)"
CONFIG_DIR="$(json_field config_dir)"
DATA_DIR="$(json_field data_dir)"
LOGS_DIR="$(json_field logs_dir)"
RUNTIME_STATE_PATH="$(json_field runtime_state_path)"
SCRIPT_PATH="$(json_field script_path)"
DAEMON_LOG_PATH="$LOGS_DIR/feedmedailyd-source.log"

case "$ACTION" in
  serve)
    base_url="$(ensure_service)"
    echo "FeedMeDaily daemon is ready at $base_url"
    echo "Log file: $DAEMON_LOG_PATH"
    ;;
  open)
    require_command xdg-open
    base_url="$(ensure_service)"
    xdg-open "$base_url" >/dev/null 2>&1 &
    echo "Opened FeedMeDaily at $base_url"
    ;;
  sync)
    base_url="$(ensure_service)"
    job_id="$(launch_sync_job "$base_url")"
    echo "Started sync job $job_id via $base_url"
    poll_sync_job "$base_url" "$job_id"
    ;;
  paths)
    print_paths
    ;;
  ""|-h|--help|help)
    usage
    ;;
  *)
    echo "Unknown action: $ACTION" >&2
    usage >&2
    exit 1
    ;;
esac
