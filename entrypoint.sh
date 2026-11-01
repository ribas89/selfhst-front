#!/bin/sh
set -eu

# The original Go API hard-codes /app/icons; the image links it to /icons.
# Run the API ONLY on the internal loopback port used by the gallery proxy.
export ICONS_UPSTREAM=http://127.0.0.1:4050

PORT=4050 /usr/local/bin/icons-api &
api_pid=$!

/usr/local/bin/icons-front &
front_pid=$!

shutdown() {
  trap - TERM INT
  kill -TERM "$front_pid" "$api_pid" 2>/dev/null || true
  wait "$front_pid" 2>/dev/null || true
  wait "$api_pid" 2>/dev/null || true
}
trap 'shutdown; exit 143' TERM INT

# If either server exits, stop the other and let Docker restart the container.
set +e
wait -n "$front_pid" "$api_pid"
status=$?
set -e
shutdown

# Even a clean unexpected exit means the long-running service should restart.
if [ "$status" -eq 0 ]; then
  exit 1
fi
exit "$status"
