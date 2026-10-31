.PHONY: test vet build sim sim-mock run run-mock replay schema

test:
	go test -race ./...

vet:
	go vet ./...

build:
	go build -o bin/pokerd ./cmd/pokerd

sim:
	go run ./cmd/pokerd sim --hands 3

sim-mock:
	go run ./cmd/pokerd sim --mock --fast --hands 20

run:
	go run ./cmd/pokerd serve

run-mock:
	go run ./cmd/pokerd serve --mock

replay:
	go run ./cmd/pokerd replay --speed 2 --loop recordings/sample.jsonl

schema:
	go run ./tools/gen-schema
