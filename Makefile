.PHONY: all
all: tidy build

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: build
build:
	go build -o bin/consul-registrator ./cmd/consul-registrator