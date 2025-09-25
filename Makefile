BINARY ?= devlink

.PHONY: build
build:
	go build -o bin/$(BINARY) ./cmd/devlink

.PHONY: test
test:
	go test ./...
