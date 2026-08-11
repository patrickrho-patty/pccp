#!/bin/sh
set -e

COMPONENT="${1:-server}"
shift || true

case "$COMPONENT" in
    server)
        exec /app/pccp-server "$@"
        ;;
    relay)
        exec /app/pccp-relay "$@"
        ;;
    pia)
        exec /app/pccp-pia "$@"
        ;;
    *)
        echo "Unknown component: $COMPONENT"
        echo "Usage: docker run pccp [server|relay|pia] [args...]"
        exit 1
        ;;
esac
