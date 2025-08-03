FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o build/jotter ./cmd/jotter

FROM alpine:latest

# Install ca-certificates for HTTPS requests and openssl for cert generation
RUN apk --no-cache add ca-certificates openssl

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/build/jotter .

# Create directories for jots and certificates
RUN mkdir -p /app/jots /app/certs

# Expose ports (8000 for HTTP, 8443 for HTTPS)
EXPOSE 8000 8443

# Set environment variables
ENV JOT_DIR=/app/jots
ENV JOT_HOST=0.0.0.0
ENV JOT_PORT=8000

# Optional TLS environment variables (set these when running the container)
# ENV JOT_CERT_FILE=/app/certs/server.crt
# ENV JOT_KEY_FILE=/app/certs/server.key

# Run the application
CMD ["./jotter"]
