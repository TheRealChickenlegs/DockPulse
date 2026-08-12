#!/usr/bin/env bash
#
# Generate a one-time enrollment token for a DockPulse agent.
# In Phase 1 this writes the token into a SQLite table; in Phase 0 it
# only prints a token and instructions so the UX can be designed
# without committing to a storage backend.

set -euo pipefail

if [ "$#" -lt 1 ]; then
    echo "Usage: $0 <ttl-hours>" >&2
    echo "Example: $0 24" >&2
    exit 64
fi

TTL_HOURS="${1}"

# In Phase 1: write to the SQLite database with a hashed-at-cached value
# and a single-use guarantee. For Phase 0 we just generate an opaque token.
TOKEN="$(openssl rand -hex 24)"
EXPIRES_AT="$(date -u -d "+${TTL_HOURS} hours" +%Y-%m-%dT%H:%M:%SZ)"

cat <<EOF
ENROLLMENT TOKEN (Phase 0 placeholder)
  token:      $TOKEN
  expires_at: $EXPIRES_AT

Place this token in a file on the agent host:

  mkdir -p ./agent-data
  printf '%s' "$TOKEN" > ./agent-data/enroll.token
  chmod 600 ./agent-data/enroll.token

In Phase 1 this script persists the token (hashed) in the controller's
SQLite database and consumes it on first agent contact.
EOF