.PHONY: run swagger test test-integration

run:
	go run ./cmd/api

swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g cmd/api/main.go -o docs

test:
	go test ./...

# integration tests: postgres, migrations up/down/up, app and anvil in docker-compose.test.yml
TEST_ENV = WALLET_TEST_DATABASE_URL="postgres://test:test@localhost:15433/wallet_test?sslmode=disable" \
	WALLET_TEST_API_URL="http://localhost:18090" \
	WALLET_TEST_ETH_RPC="http://localhost:18545" \
	WALLET_TEST_KEK="000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

test-integration:
	docker compose -f docker-compose.test.yml up -d --build --wait
	$(TEST_ENV) go test -tags=integration -count=1 ./... ; status=$$?; \
	docker compose -f docker-compose.test.yml down -v; exit $$status
