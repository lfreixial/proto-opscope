.PHONY: all build install gen-options test lint

BINARY = protoc-gen-fieldops
CMD    = ./cmd/protoc-gen-fieldops

all: build

build:
	go build -o bin/$(BINARY) $(CMD)

install:
	go install $(CMD)

gen-options:
	buf generate

test:
	go test ./...

lint:
	go vet ./...
