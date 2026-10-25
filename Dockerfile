FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /wallet-service ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /wallet-admin ./cmd/admin
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gas-station-send ./cmd/gas-station-send

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /wallet-service /wallet-service
COPY --from=builder /wallet-admin /wallet-admin
COPY --from=builder /gas-station-send /gas-station-send

EXPOSE 8080

ENTRYPOINT ["/wallet-service"]
