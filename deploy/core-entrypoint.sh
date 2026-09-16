#!/bin/sh
set -eu

if [ "$(id -u)" = 0 ]; then
  # Only adjust the mount roots; existing files retain their ownership and mode.
  chown 10001:10001 /app/auths /app/data /run/wb2a
  chmod 700 /app/auths /app/data /run/wb2a
  exec su-exec 10001:10001 "$@"
fi

exec "$@"
