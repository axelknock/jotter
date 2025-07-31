#!/bin/bash

# Generate self-signed TLS certificates for Jotter
# Usage: ./scripts/generate-certs.sh [domain]

set -e

DOMAIN=${1:-localhost}
CERT_DIR="certs"
CERT_FILE="$CERT_DIR/server.crt"
KEY_FILE="$CERT_DIR/server.key"

# Create certs directory if it doesn't exist
mkdir -p "$CERT_DIR"

echo "Generating self-signed TLS certificate for domain: $DOMAIN"

# Generate private key
openssl genrsa -out "$KEY_FILE" 2048

# Generate certificate signing request and self-signed certificate
openssl req -new -x509 -key "$KEY_FILE" -out "$CERT_FILE" -days 365 \
    -subj "/C=US/ST=State/L=City/O=Organization/CN=$DOMAIN" \
    -addext "subjectAltName=DNS:$DOMAIN,DNS:localhost,IP:127.0.0.1"

echo "Certificate generated successfully!"
echo "Certificate: $CERT_FILE"
echo "Private Key: $KEY_FILE"
echo ""
echo "To run Jotter with TLS, set these environment variables:"
echo "export JOT_CERT_FILE=$(pwd)/$CERT_FILE"
echo "export JOT_KEY_FILE=$(pwd)/$KEY_FILE"
echo ""
echo "Then run:"
echo "go run cmd/jotter/main.go"
echo ""
echo "Or with Docker:"
echo "docker run -d -p 8000:8000 \\"
echo "  -v $(pwd)/$CERT_DIR:/app/certs \\"
echo "  -e JOT_CERT_FILE=/app/certs/server.crt \\"
echo "  -e JOT_KEY_FILE=/app/certs/server.key \\"
echo "  -v /path/to/jots:/app/jots \\"
echo "  jotter"
