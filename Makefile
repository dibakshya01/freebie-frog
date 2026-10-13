BINARY  := freebie
PKG     := ./...
VERSION ?= dev
LDFLAGS := -X github.com/dibakshya01/freebie-frog/internal/version.Version=$(VERSION)

.PHONY: build test vet fmt fmt-check check run clean

## build: compile the freebie binary into ./bin
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/freebie

## test: run the test suite with the race detector
test:
	go test -race $(PKG)

## vet: run go vet
vet:
	go vet $(PKG)

## fmt: format all Go files in place
fmt:
	gofmt -w .

## fmt-check: fail if any Go file is not gofmt-ed
fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "not gofmt-ed:"; echo "$$out"; exit 1; fi

## check: run all CI checks locally
check: fmt-check vet test build

## run: build and start the server
run: build
	./bin/$(BINARY) serve

## clean: remove build artifacts
clean:
	rm -rf bin
