.PHONY: run test vet check build clean cache-up cache-down cache-logs

run:
	go run ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

check: test vet

build:
	go build -o bin/domscan ./cmd/server

clean:
	go clean

cache-up:
	docker compose up -d redis

cache-down:
	docker compose stop redis

cache-logs:
	docker compose logs -f redis
