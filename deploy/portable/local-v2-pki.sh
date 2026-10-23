#!/bin/sh
# Generate the local v2 topology PKI.
#
# The Cloud owner processes authenticate their gRPC peers with mutual TLS. Every
# owner process presents a service certificate whose URI SAN is
# spiffe://opl.cloud/service/<service>, and the CA that signed it is the only
# trust root any owner accepts. packages/contracts/go/owneridentity/transport.go
# refuses a certificate without exactly one such URI SAN, and it refuses a
# caller whose certificate identity differs from the peer identity it claims,
# so this script issues one certificate per process of the local topology.
#
# The same-origin proxy is not an owner: it is the installation's route
# provider, so its certificate carries the host names the browser and the
# containers use instead of a service URI.
#
# Usage: local-v2-pki.sh <output-directory>
#
# The output directory holds ca.crt, ca.key, one <service>.crt/<service>.key per
# process and the proxy certificate. The private keys stay 0600 and the
# directory stays 0700; the local v2 Compose file mounts the directory into the
# owner containers as root, which is the only identity that may read them.
set -eu

output="${1:-}"
if [ -z "$output" ]; then
  echo "usage: local-v2-pki.sh <output-directory>" >&2
  exit 2
fi

# The owner processes of the local v2 topology. "tenant" is the deployment unit
# that carries the CloudIdentity and Gateway owners, so it presents the tenant
# service identity. "console_bff" and "control_plane" are transport principals
# rather than data owners, but they are peers on the same boundary, so they get
# certificates too.
services="tenant capability build runtime_control workspace serve resource_catalog ledger fabric console_bff control_plane"

openssl="${OPENSSL:-openssl}"
if ! command -v "$openssl" >/dev/null 2>&1; then
  echo "openssl is required to generate the local v2 PKI" >&2
  exit 2
fi

mkdir -p "$output"
cd "$output"
chmod 700 "$output"

cat >ca.cnf <<'CA'
[req]
distinguished_name = dn
prompt = no
x509_extensions = v3_ca
[dn]
CN = opl-cloud local v2 CA
O = opl-cloud local v2
[v3_ca]
basicConstraints = critical, CA:TRUE, pathlen:0
keyUsage = critical, keyCertSign, cRLSign
subjectKeyIdentifier = hash
CA

"$openssl" req -x509 -newkey rsa:2048 -nodes -sha256 -days 825 \
  -keyout ca.key -out ca.crt -config ca.cnf >/dev/null 2>&1
chmod 600 ca.key

issue() {
  name="$1"
  subject="$2"
  extensions="$3"

  cat >"$name.ext.cnf" <<EXT
[v3_req]
$extensions
EXT
  cat >"$name.req.cnf" <<REQ
[req]
distinguished_name = dn
prompt = no
req_extensions = v3_req
[dn]
CN = $subject
O = opl-cloud local v2
[v3_req]
$extensions
REQ

  "$openssl" req -new -newkey rsa:2048 -nodes -sha256 \
    -keyout "$name.key" -out "$name.csr" -config "$name.req.cnf" >/dev/null 2>&1
  "$openssl" x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out "$name.crt" -days 825 -sha256 -extfile "$name.ext.cnf" -extensions v3_req >/dev/null 2>&1
  rm -f "$name.csr" "$name.req.cnf" "$name.ext.cnf"
  chmod 600 "$name.key"
  chmod 644 "$name.crt"
}

for service in $services; do
  issue "$service" "$service" "subjectAltName = URI:spiffe://opl.cloud/service/$service
extendedKeyUsage = serverAuth, clientAuth
keyUsage = critical, digitalSignature, keyEncipherment
basicConstraints = critical, CA:FALSE"
done

# The proxy answers the browser and the containers on one origin. Its
# certificate names the host the browser uses and the service name the
# containers resolve, so the same certificate serves both callers.
issue proxy "cloud-local-v2" "subjectAltName = DNS:localhost, DNS:cloud-local-v2, IP:127.0.0.1
extendedKeyUsage = serverAuth
keyUsage = critical, digitalSignature, keyEncipherment
basicConstraints = critical, CA:FALSE"

rm -f ca.cnf
echo "local v2 PKI written to $output"
"$openssl" x509 -in ca.crt -noout -subject
