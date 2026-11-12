.PHONY: init dev test build build-linux build-windows clean

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")

init:
	go mod tidy

dev:
	go run .

test:
	go test ./...

build: build-linux build-windows

build-linux:
	echo "Building aadb for linux/amd64"
	GOOS=linux GOARCH=amd64 CC=gcc go build -ldflags '-X main.Version=$(VERSION)' -o bin/aadb-$(VERSION) -v .
	echo "Building aadb for linux/amd64 done"

build-windows:
	echo "Building aadb for windows/amd64"
	GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -ldflags '-X main.Version=$(VERSION)' -o bin/aadb-$(VERSION).exe -v .
	echo "Building aadb for windows/amd64 done"

clean:
	rm -rf bin

.DEFAULT_GOAL := build
