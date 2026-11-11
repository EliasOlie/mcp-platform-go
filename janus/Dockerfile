# Stage 1: Build estático em Go
FROM golang:alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/janus-server ./cmd/server

# Stage 2: Imagem final ultra-leve
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app

COPY --from=builder /app/janus-server /app/janus-server
COPY --from=builder /app/migrations /app/migrations

ENV PORT=3030

EXPOSE 3030

CMD ["/app/janus-server"]
