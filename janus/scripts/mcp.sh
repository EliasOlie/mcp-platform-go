#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1

if [ ! -f ./bin/janus ]; then
    go build -o bin/janus ./cmd/server
fi

exec ./bin/janus --stdio
