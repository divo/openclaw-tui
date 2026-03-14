.PHONY: run build test tidy

run:
	go run ./cmd/openclaw-tui

build:
	go build -o openclaw-tui ./cmd/openclaw-tui

test:
	go test ./...

tidy:
	go mod tidy
