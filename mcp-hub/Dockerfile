FROM golang:alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/mcp-hub ./cmd/server

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /app/mcp-hub /app/mcp-hub

EXPOSE 8082
CMD ["/app/mcp-hub"]
