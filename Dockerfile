FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /wallet-service ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /wallet-admin ./cmd/admin
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /ethereum-gas-station-send ./cmd/ethereum-gas-station-send
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /tron-gas-station-send ./cmd/tron-gas-station-send

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /wallet-service /wallet-service
COPY --from=builder /wallet-admin /wallet-admin
COPY --from=builder /ethereum-gas-station-send /ethereum-gas-station-send
COPY --from=builder /tron-gas-station-send /tron-gas-station-send

EXPOSE 8080

ENTRYPOINT ["/wallet-service"]
