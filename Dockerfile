FROM golang:latest AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN GOTOOLCHAIN=auto go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOTOOLCHAIN=auto go build -ldflags="-s -w" -o /dolibarr-mcp ./cmd/dolibarr-mcp

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /dolibarr-mcp /usr/local/bin/dolibarr-mcp

EXPOSE 8080

ENTRYPOINT ["dolibarr-mcp"]
