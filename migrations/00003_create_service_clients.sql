-- +goose Up
CREATE TABLE service_clients (
    id UUID PRIMARY KEY,
    service VARCHAR(64) NOT NULL, 
    key_hash BYTEA NOT NULL, -- sha256 of X-Service-Key
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP DEFAULT NULL,
    expires_at TIMESTAMP DEFAULT NULL, -- old key after rotation is accepted until this time
    replaced_by UUID DEFAULT NULL REFERENCES service_clients(id), -- new key after rotation

    CONSTRAINT service_clients_key_hash_unique UNIQUE (key_hash)
);

CREATE INDEX idx_service_clients_service ON service_clients (service);

CREATE UNIQUE INDEX service_clients_one_primary
    ON service_clients (service)
    WHERE is_active AND expires_at IS NULL;

-- +goose Down
DROP TABLE service_clients;
