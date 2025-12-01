.PHONY: build test lint

# Build the application binary
build:
	go build -o infinitetalk-api .

# Run tests with race detection
test:
	go test -race ./...

# Run linter
lint:
	golangci-lint run ./...
