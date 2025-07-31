FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o jotter .

FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/jotter .

# Create jots directory
RUN mkdir -p /app/jots

# Expose port
EXPOSE 8000

# Set environment variables
ENV JOT_DIR=/app/jots
ENV JOT_HOST=0.0.0.0
ENV JOT_PORT=8000

# Run the application
CMD ["./jotter"]
