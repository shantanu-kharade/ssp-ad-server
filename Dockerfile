# Stage 1: Builder
FROM golang:1.25-alpine AS builder

# Install build dependencies
# RUN apk add --no-cache git

WORKDIR /app

# Copy go.mod and go.sum for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application as a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o ssp-ad-server ./cmd/server/main.go

# Stage 2: Final Image
FROM alpine:3.19

# Install CA certificates for secure connections (HTTPS DSPs)
# RUN apk add --no-cache ca-certificates

# Create a non-root user for security
RUN adduser -D -g '' sspuser

WORKDIR /home/sspuser

# Copy the binary from the builder stage
COPY --from=builder /app/ssp-ad-server .
# Copy migrations for database setup
COPY --from=builder /app/internal/db/migrations ./internal/db/migrations

# Change ownership to the non-root user
RUN chown sspuser:sspuser ssp-ad-server

# Use the non-root user
USER sspuser

# Expose the default server port
EXPOSE 8080

# Run the server
ENTRYPOINT ["./ssp-ad-server"]
