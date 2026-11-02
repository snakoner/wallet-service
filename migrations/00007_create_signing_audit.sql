-- +goose Up
CREATE TABLE signing_audit (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP NOT NULL,
    service_client_id UUID NOT NULL, -- caller key, service via service_clients
    key_id UUID NOT NULL, -- as requested, no FK: may not exist
    network SMALLINT NOT NULL,
    raw_tx BYTEA NOT NULL, -- unsigned tx as received
    tx_hash VARCHAR(66) DEFAULT NULL, -- signed tx
    error_code VARCHAR(64) DEFAULT NULL, -- NULL - signed, else error code of response

    CONSTRAINT signing_audit_network_check CHECK (network IN (1, 2, 3))
);

CREATE INDEX idx_signing_audit_key_created ON signing_audit (key_id, created_at);
CREATE INDEX idx_signing_audit_failed ON signing_audit (created_at) WHERE error_code IS NOT NULL;

-- +goose Down
DROP TABLE signing_audit;
