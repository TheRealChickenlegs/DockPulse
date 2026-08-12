#!/usr/bin/env bash
#
# Generate a local self-signed CA + controller cert + agent cert for
# development. Run from the repo root:
#
#     ./scripts/gen-dev-certs.sh
#
# Output goes into ./certs/. NEVER commit that directory.

set -euo pipefail

OUT="${OUT:-certs}"
DAYS_CA="${DAYS_CA:-3650}"
DAYS_LEAF="${DAYS_LEAF:-825}"

mkdir -p "$OUT"

echo "==> Generating CA"
openssl genrsa -out "$OUT/ca.key" 4096
openssl req -x509 -new -nodes -key "$OUT/ca.key" -sha256 -days "$DAYS_CA" \
    -subj "/CN=DockPulse Dev CA" -out "$OUT/ca.crt"

echo "==> Generating controller cert"
openssl genrsa -out "$OUT/controller.key" 2048
openssl req -new -key "$OUT/controller.key" -subj "/CN=dockpulse-controller" -out "$OUT/controller.csr"
cat > "$OUT/controller.ext" <<'EOF'
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
extendedKeyUsage=serverAuth
subjectAltName=@alt
[alt]
DNS.1=localhost
DNS.2=dockpulse.local
IP.1=127.0.0.1
EOF
openssl x509 -req -in "$OUT/controller.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" -CAcreateserial \
    -out "$OUT/controller.crt" -days "$DAYS_LEAF" -sha256 -extfile "$OUT/controller.ext"

echo "==> Generating agent cert (placeholder, Phase 1 issues per-agent certs)"
openssl genrsa -out "$OUT/agent.key" 2048
openssl req -new -key "$OUT/agent.key" -subj "/CN=dockpulse-agent-local" -out "$OUT/agent.csr"
cat > "$OUT/agent.ext" <<'EOF'
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
extendedKeyUsage=clientAuth
EOF
openssl x509 -req -in "$OUT/agent.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" -CAcreateserial \
    -out "$OUT/agent.crt" -days "$DAYS_LEAF" -sha256 -extfile "$OUT/agent.ext"

echo
echo "Done. Certs in $OUT:"
ls -1 "$OUT"
echo
echo "Phase 0 does not use these yet; the agent/agent API in Phase 1 will."