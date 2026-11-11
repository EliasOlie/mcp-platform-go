.PHONY: all build run test clean

all: build

build:
	@echo "==> Compilando Janus Core..."
	go build -o bin/janus ./cmd/server

run:
	@echo "==> Iniciando Janus Core em modo de desenvolvimento..."
	go run ./cmd/server

test:
	@echo "==> Executando testes..."
	go test -v ./...

clean:
	rm -rf bin
