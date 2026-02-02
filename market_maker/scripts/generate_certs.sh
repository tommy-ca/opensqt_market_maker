#!/bin/bash
# Generate self-signed TLS certificates for gRPC communication
# This script creates certificates for development/testing purposes
# Resolves TODO #001 - Unencrypted gRPC Communications

set -e

CERT_DIR="$(dirname "$0")/../certs"
mkdir -p "$CERT_DIR"

echo "🔐 Generating TLS certificates for gRPC communication..."
echo "Certificates will be stored in: $CERT_DIR"
echo ""

# Certificate validity in days (1 year)
VALID_DAYS=365

# Generate private key with strong encryption
echo "Step 1/4: Generating server private key (4096-bit RSA)..."
openssl genrsa -out "$CERT_DIR/server-key.pem" 4096

# Generate certificate signing request
echo "Step 2/4: Generating certificate signing request..."
openssl req -new -key "$CERT_DIR/server-key.pem" \
    -out "$CERT_DIR/server.csr" \
    -subj "/C=US/ST=California/L=SanFrancisco/O=MarketMaker/OU=Trading/CN=localhost"

# Generate self-signed certificate valid for 365 days with proper extensions
echo "Step 3/4: Generating self-signed certificate (valid for ${VALID_DAYS} days)..."
openssl x509 -req -days ${VALID_DAYS} \
    -in "$CERT_DIR/server.csr" \
    -signkey "$CERT_DIR/server-key.pem" \
    -out "$CERT_DIR/server-cert.pem" \
    -extfile <(printf "subjectAltName=DNS:localhost,DNS:*.localhost,IP:127.0.0.1,IP:0.0.0.0\nextendedKeyUsage=serverAuth,clientAuth")

# Clean up CSR
rm "$CERT_DIR/server.csr"

# Set appropriate permissions (private key readable only by owner)
echo "Step 4/4: Setting secure file permissions..."
chmod 600 "$CERT_DIR/server-key.pem"
chmod 644 "$CERT_DIR/server-cert.pem"

echo ""
echo "✅ TLS certificates generated successfully!"
echo ""
echo "Generated files:"
echo "  📄 Certificate: $CERT_DIR/server-cert.pem"
echo "  🔑 Private key: $CERT_DIR/server-key.pem"
echo ""
echo "Certificate details:"
openssl x509 -in "$CERT_DIR/server-cert.pem" -noout -subject -dates -ext subjectAltName
echo ""
echo "⚠️  IMPORTANT: These are self-signed certificates for development/internal use."
echo "   For production, consider using Let's Encrypt or a trusted CA."
echo ""
