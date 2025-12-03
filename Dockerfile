# Build stage
FROM golang:1.25 AS builder

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/infinitetalk-api ./cmd/server

# Final stage
FROM debian:bookworm-slim

# Install ffmpeg
RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/infinitetalk-api .

# Run the application
ENTRYPOINT ["/app/infinitetalk-api"]
