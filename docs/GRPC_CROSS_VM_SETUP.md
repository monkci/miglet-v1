# gRPC Cross-VM Communication Setup

This guide explains how to configure MIGlet and the MIG Controller to communicate securely via gRPC when they're on different VMs.

## Overview

```
┌─────────────────────┐         gRPC (TLS)         ┌──────────────────────┐
│      MIGlet VM      │ ───────────────────────►   │   Controller VM      │
│                     │         :50051              │                      │
│  - Connects to      │                            │  - Exposes gRPC      │
│    controller       │                            │    on port 50051     │
│  - Sends heartbeats │                            │  - Accepts streams   │
│  - Receives commands│                            │  - Sends commands    │
└─────────────────────┘                            └──────────────────────┘
```

## 1. Network Configuration (GCP)

### Create Firewall Rule

```bash
# Allow gRPC traffic from MIGlet VMs to Controller
gcloud compute firewall-rules create allow-grpc-controller \
    --direction=INGRESS \
    --priority=1000 \
    --network=default \
    --action=ALLOW \
    --rules=tcp:50051 \
    --source-ranges=0.0.0.0/0 \  # Or restrict to specific IP ranges
    --target-tags=controller-vm

# Tag your controller VM
gcloud compute instances add-tags YOUR_CONTROLLER_VM \
    --tags=controller-vm \
    --zone=YOUR_ZONE
```

### Using Internal IPs (Recommended for Same VPC)

If both VMs are in the same VPC, use internal IPs:

```bash
# Get controller's internal IP
gcloud compute instances describe YOUR_CONTROLLER_VM \
    --zone=YOUR_ZONE \
    --format='get(networkInterfaces[0].networkIP)'
```

## 2. TLS Certificate Setup

### Option A: Self-Signed Certificates (Development/Testing)

Generate certificates on the Controller VM:

```bash
# Create a directory for certificates
mkdir -p /opt/controller/certs
cd /opt/controller/certs

# Generate CA private key
openssl genrsa -out ca.key 4096

# Generate CA certificate
openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 \
    -out ca.crt \
    -subj "/C=US/ST=State/L=City/O=MonkCI/CN=MonkCI-CA"

# Generate server private key
openssl genrsa -out server.key 2048

# Create server certificate signing request (CSR)
# IMPORTANT: Include the controller's IP/hostname in SAN
cat > server.csr.conf << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
C = US
ST = State
L = City
O = MonkCI
CN = controller.monkci.io

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = controller.monkci.io
DNS.2 = localhost
IP.1 = CONTROLLER_INTERNAL_IP
IP.2 = CONTROLLER_EXTERNAL_IP
IP.3 = 127.0.0.1
EOF

# Replace placeholders with actual IPs
sed -i "s/CONTROLLER_INTERNAL_IP/$(hostname -I | awk '{print $1}')/g" server.csr.conf
# Add external IP if needed

# Generate CSR
openssl req -new -key server.key -out server.csr -config server.csr.conf

# Sign server certificate with CA
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out server.crt -days 365 -extensions v3_req -extfile server.csr.conf

# Set permissions
chmod 600 *.key
chmod 644 *.crt

echo "Certificates generated in $(pwd)"
```

### Option B: Let's Encrypt (Production)

Use certbot with your domain:

```bash
sudo certbot certonly --standalone -d controller.monkci.io

# Certificates will be in:
# /etc/letsencrypt/live/controller.monkci.io/fullchain.pem
# /etc/letsencrypt/live/controller.monkci.io/privkey.pem
```

## 3. Controller Configuration

### Start Controller with TLS

```bash
# Set environment variables for TLS
export GRPC_TLS_CERT="/opt/controller/certs/server.crt"
export GRPC_TLS_KEY="/opt/controller/certs/server.key"

# Optional: For mTLS (client certificate verification)
# export GRPC_TLS_CA="/opt/controller/certs/ca.crt"

# Start the controller
./controller
```

Expected output:
```
TLS configuration found - enabling secure gRPC
gRPC server starting on port 50051 (TLS enabled)
```

## 4. MIGlet Configuration

### Copy CA Certificate to MIGlet VM

```bash
# From Controller VM, copy ca.crt to MIGlet VM
scp /opt/controller/certs/ca.crt user@miglet-vm:/opt/miglet/certs/
```

### Configure MIGlet

**Option A: Environment Variables**

```bash
export MIGLET_POOL_ID="my-pool"
export MIGLET_VM_ID="miglet-001"
export MIGLET_CONTROLLER_GRPC_ENDPOINT="CONTROLLER_IP:50051"
export MIGLET_CONTROLLER_TLS_ENABLED="true"
export MIGLET_CONTROLLER_TLS_CA_CERT_PATH="/opt/miglet/certs/ca.crt"

# Optional: For mTLS
# export MIGLET_CONTROLLER_TLS_CLIENT_CERT_PATH="/opt/miglet/certs/client.crt"
# export MIGLET_CONTROLLER_TLS_CLIENT_KEY_PATH="/opt/miglet/certs/client.key"

# If using self-signed and having hostname issues:
# export MIGLET_CONTROLLER_TLS_SERVER_NAME="controller.monkci.io"

./bin/miglet
```

**Option B: Config File (configs/miglet.yaml)**

```yaml
pool_id: "my-pool"
vm_id: "miglet-001"

controller:
  grpc_endpoint: "CONTROLLER_IP:50051"
  tls:
    enabled: true
    ca_cert_path: "/opt/miglet/certs/ca.crt"
    # For mTLS:
    # client_cert_path: "/opt/miglet/certs/client.crt"
    # client_key_path: "/opt/miglet/certs/client.key"
    # Override server name for certificate verification:
    # server_name: "controller.monkci.io"
```

## 5. Quick Test (Insecure - Development Only)

If you just want to test cross-VM communication without TLS:

### Controller VM
```bash
./controller
# Output: gRPC server starting on port 50051 (INSECURE - no TLS)
```

### MIGlet VM
```bash
export MIGLET_POOL_ID="test-pool"
export MIGLET_VM_ID="test-vm"
export MIGLET_CONTROLLER_GRPC_ENDPOINT="CONTROLLER_IP:50051"
# TLS disabled by default

./bin/miglet
```

> ⚠️ **Warning**: Never use insecure mode in production!

## 6. Troubleshooting

### Connection Refused

```bash
# Check if controller is listening
netstat -tlnp | grep 50051

# Check firewall rules
gcloud compute firewall-rules list --filter="name=allow-grpc"

# Test connectivity from MIGlet VM
nc -zv CONTROLLER_IP 50051
```

### TLS Handshake Errors

```bash
# Verify certificate
openssl s_client -connect CONTROLLER_IP:50051 -CAfile /opt/miglet/certs/ca.crt

# Check certificate details
openssl x509 -in /opt/controller/certs/server.crt -text -noout
```

### Certificate Verification Failed

Common causes:
1. **Wrong CA**: Make sure MIGlet uses the CA that signed the server cert
2. **Wrong hostname**: Server cert must include the IP/hostname you're connecting to
3. **Expired certificate**: Check cert validity dates

```bash
# Check if IP is in certificate's SAN
openssl x509 -in server.crt -text -noout | grep -A1 "Subject Alternative Name"
```

### Use InsecureSkipVerify (Emergency Only!)

If you're stuck and need to test quickly:

```bash
export MIGLET_CONTROLLER_TLS_ENABLED="true"
export MIGLET_CONTROLLER_TLS_INSECURE_SKIP_VERIFY="true"  # NOT FOR PRODUCTION!
```

## 7. Production Recommendations

1. **Use mTLS**: Require client certificates for mutual authentication
2. **Rotate certificates**: Set up automatic certificate renewal
3. **Use internal IPs**: Keep gRPC traffic within your VPC
4. **Monitor connections**: Log and alert on connection failures
5. **Use a service mesh**: Consider Istio or similar for automatic mTLS

## 8. Environment Variable Reference

| Variable | Description | Example |
|----------|-------------|---------|
| `MIGLET_CONTROLLER_GRPC_ENDPOINT` | Controller gRPC address | `10.128.0.5:50051` |
| `MIGLET_CONTROLLER_TLS_ENABLED` | Enable TLS | `true` |
| `MIGLET_CONTROLLER_TLS_CA_CERT_PATH` | Path to CA certificate | `/opt/miglet/certs/ca.crt` |
| `MIGLET_CONTROLLER_TLS_CLIENT_CERT_PATH` | Client cert (mTLS) | `/opt/miglet/certs/client.crt` |
| `MIGLET_CONTROLLER_TLS_CLIENT_KEY_PATH` | Client key (mTLS) | `/opt/miglet/certs/client.key` |
| `MIGLET_CONTROLLER_TLS_SERVER_NAME` | Override server name | `controller.monkci.io` |
| `MIGLET_CONTROLLER_TLS_INSECURE_SKIP_VERIFY` | Skip verification | `false` |

### Controller Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `GRPC_TLS_CERT` | Server certificate path | `/opt/controller/certs/server.crt` |
| `GRPC_TLS_KEY` | Server private key path | `/opt/controller/certs/server.key` |
| `GRPC_TLS_CA` | CA cert for mTLS | `/opt/controller/certs/ca.crt` |

