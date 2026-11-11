.PHONY: all build build-auth build-janus build-hub run-auth run-janus run-hub up down clean test

all: build

build: build-auth build-janus build-hub

build-auth:
	@echo "==> Compilando Auth Service..."
	go build -o bin/auth-server ./auth-service/cmd/server

build-janus:
	@echo "==> Compilando Janus Core..."
	go build -o bin/janus-server ./janus/cmd/server

build-hub:
	@echo "==> Compilando MCP-Hub..."
	go build -o bin/mcp-hub ./mcp-hub/cmd/server

run-auth: build-auth
	./bin/auth-server

run-janus: build-janus
	./bin/janus-server

run-hub: build-hub
	./bin/mcp-hub

up:
	docker compose up -d --build

down:
	docker compose down

clean:
	rm -rf bin/
