.PHONY: run swagger

run:
	go run ./cmd/api

swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g cmd/api/main.go -o docs
