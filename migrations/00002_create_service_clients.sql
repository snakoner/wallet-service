-- +goose Up
CREATE TABLE service_clients (
    id UUID PRIMARY KEY,
    service VARCHAR(64) NOT NULL, -- exchange, banknote, cedex-retail
    key_hash BYTEA NOT NULL, -- sha256 of X-Service-Key
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP DEFAULT NULL,

    CONSTRAINT service_clients_key_hash_unique UNIQUE (key_hash)
);

CREATE INDEX idx_service_clients_service ON service_clients (service);

-- +goose Down
DROP TABLE service_clients;
