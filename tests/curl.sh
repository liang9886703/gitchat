#!/bin/sh
# Reuse the owner cookie across requests. Override GITCHAT_URL for a remote instance.
set -eu
base=${GITCHAT_URL:-http://127.0.0.1:8002}
cookie=$(mktemp)
trap 'rm -f "$cookie"' EXIT
curl --fail-with-body -N -c "$cookie" -b "$cookie" "$base/g/default" \
  -H 'Content-Type: application/json' \
  -d '{"ifFirst":true,"stream":true,"message":{"content":[{"type":"text","text":["我想规划一次日本旅行"]}]}}'
