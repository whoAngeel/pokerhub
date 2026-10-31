.PHONY: test vet build sim sim-mock run replay schema

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

replay:
	go run ./cmd/pokerd replay

schema:
	@echo "schema: not implemented yet (PLAN F3)"
