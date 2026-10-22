#!/usr/bin/env bash
set -euo pipefail

# screenshot tool for bitmagnet webui
# Usage: ./scripts/screenshot.sh [URL] [OUTPUT_PATH] [WAIT_MS] [WIDTH] [HEIGHT]

URL="${1:-http://localhost:3333/webui}"
OUTPUT="${2:-screenshot.png}"
WAIT="${3:-1500}"
WIDTH="${4:-1280}"
HEIGHT="${5:-800}"

mkdir -p "$(dirname "$OUTPUT")"

echo "Capturing screenshot of $URL -> $OUTPUT (viewport ${WIDTH}x${HEIGHT}, wait ${WAIT}ms)..."
shot-scraper shot \
  --wait "$WAIT" \
  -w "$WIDTH" \
  -h "$HEIGHT" \
  "$URL" \
  -o "$OUTPUT"
echo "Screenshot saved to $OUTPUT"
