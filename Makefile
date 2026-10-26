BIN := bin/expenses_bot
ifeq ($(OS),Windows_NT)
BIN := $(BIN).exe
endif

.PHONY: build run db-init test vet up down logs clean

build:
	go build -o $(BIN) ./cmd

db-init:
	go run ./cmd init-db

run: db-init
	go run ./cmd

test:
	go test ./...

vet:
	go vet ./...

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f app

clean:
	go clean
	rm -rf bin
