.PHONY: all
all: build check test

.PHONY: build
build:
	CGO_ENABLED=0 go build -o rspamd-iscan main.go

.PHONY: check
check:
	golangci-lint run ./...

.PHONY: test
test:
	go test -race -timeout=60s ./...
