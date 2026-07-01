# Binary name
BINARY_NAME=dockenciler
# Main package path
MAIN_PACKAGE=./cmd/dockenciler

.PHONY: all build test clean fmt tidy docker-build docker-up security-scan

all: build test

## build: Build the binary
build:
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

## test: Run all tests
test:
	go test -v ./...

## clean: Remove the binary
clean:
	rm -f $(BINARY_NAME)

## fmt: Format the code
fmt:
	go fmt ./...

## tidy: Tidy up the go.mod file
tidy:
	go mod tidy

## docker-build: Build the Docker image
docker-build:
	docker build -t $(BINARY_NAME) .

## docker-up: Run with Docker Compose
docker-up:
	docker compose up -d

## security-scan: Run Trivy security scan locally
security-scan:
	@echo "Running Trivy filesystem scan..."
	trivy fs --severity CRITICAL,HIGH .
	@echo "Running Trivy config scan..."
	trivy config --severity CRITICAL,HIGH,MEDIUM .

## help: Show this help message
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "} {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
