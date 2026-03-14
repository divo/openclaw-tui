.PHONY: run build tidy

run:
	go run .

build:
	go build -o openclaw-tui .

tidy:
	go mod tidy
