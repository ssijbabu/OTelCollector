.PHONY: test lint build tidy

test:
	go test ./...

lint:
	golangci-lint run ./...

build:
	go build ./...

tidy:
	go mod tidy
