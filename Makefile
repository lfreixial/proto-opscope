.PHONY: all build install generate test lint run

BINARY = protoc-gen-fieldops
CMD    = ./cmd/protoc-gen-fieldops

all: build

build:
	go build -o bin/$(BINARY) $(CMD)

install:
	go install $(CMD)

generate: build
	buf generate

test:
	go test ./...

lint:
	go vet ./...

run:
	go run ./example/server/main.go