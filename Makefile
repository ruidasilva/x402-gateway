.PHONY: build test lint run clean

build:
	go build -o bin/x402-server ./cmd/server
	go build -o bin/x402-client ./cmd/client

test:
	go test ./... -v -count=1

lint:
	go vet ./...

run:
	go run ./cmd/server

clean:
	rm -rf bin/
