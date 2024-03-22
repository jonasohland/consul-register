.PHONY: all
all: tidy build-dev

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: build-dev
build-dev:
	CGO_ENABLED=0 go build -ldflags="-X main.Version=$(shell git describe --tags)" -o bin/consul-register ./cmd/consul-register

.PHONY: build
build:
	@sh -c 'if [ -z "$(GIT_TAG)" ]; then echo "GIT_TAG not set" ; exit 1; fi'
	CGO_ENABLED=0 go build -ldflags="-X main.Version=$(GIT_TAG) -s -w -buildid=" -trimpath -o bin/consul-register ./cmd/consul-register