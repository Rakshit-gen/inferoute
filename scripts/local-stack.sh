#!/usr/bin/env bash
# Brings up a full local inferoute: two mock inference backends and the
# gateway in front of them, with no GPUs or real models needed. Point the
# dashboard (web/) at http://localhost:8091 to see it work end to end.
#
#   ./scripts/local-stack.sh
#
# Ctrl-C stops everything.
set -euo pipefail
cd "$(dirname "$0")/.."

TMP="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null; rm -rf "$TMP"' EXIT INT TERM

echo "building..."
go build -o "$TMP/inferouted" ./cmd/inferouted
go build -o "$TMP/mock" ./scripts/mock-openai-backend

cat > "$TMP/config.json" <<'EOF'
{
  "listen_addr": ":8091",
  "health_check_path": "/healthz",
  "health_check_interval": "3s",
  "backends": [
    { "name": "alpha", "url": "http://localhost:11434", "models": ["llama3"] },
    { "name": "bravo", "url": "http://localhost:11435", "models": ["llama3"] }
  ],
  "rate_limit": { "enabled": true, "requests_per_second": 50, "burst": 20 },
  "cache": { "enabled": false },
  "model_aliases": { "gpt-4o": "llama3" },
  "cors_origins": ["*"],
  "api_keys": []
}
EOF

"$TMP/mock" -addr :11434 -model llama3 -name alpha &
"$TMP/mock" -addr :11435 -model llama3 -name bravo &
sleep 1
"$TMP/inferouted" -config "$TMP/config.json" &

echo
echo "gateway up on http://localhost:8091"
echo "  add it as a connection in the dashboard, or:"
echo "  curl localhost:8091/v1/chat/completions -d '{\"model\":\"llama3\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'"
echo
wait
