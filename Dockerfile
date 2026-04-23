# Stage 1: Builder
FROM golang:1.25-alpine AS builder
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

# Create a non-root user for security
RUN adduser -D sspuser

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/ssp-ad-server .
COPY --from=builder /app/internal/db/migrations ./internal/db/migrations

# Use the non-root user
USER sspuser

# Expose the default server port
EXPOSE 8080

# Run the server
ENTRYPOINT ["./ssp-ad-server"]
