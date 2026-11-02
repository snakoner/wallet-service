-- +goose Up
CREATE TABLE wallet_keys (
    id UUID PRIMARY KEY,
    role_id UUID NOT NULL REFERENCES wallet_roles(id),
    network SMALLINT NOT NULL,
    alias VARCHAR(128) DEFAULT NULL, -- optional human-readable name
    address VARCHAR(64) NOT NULL,
    external_ref VARCHAR(128) DEFAULT NULL, -- deposit role: order id
    status SMALLINT NOT NULL, 
    key_cipher BYTEA NOT NULL,
    dek_cipher BYTEA NOT NULL,
    kek_version INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP DEFAULT NULL,

    CONSTRAINT wallet_keys_network_check CHECK (network IN (1, 2, 3)),
    CONSTRAINT wallet_keys_status_check CHECK (status IN (1, 4, 5)),
    CONSTRAINT wallet_keys_network_address_unique UNIQUE (network, address),
    CONSTRAINT wallet_keys_role_external_ref_unique UNIQUE (role_id, external_ref)
);

CREATE INDEX idx_wallet_keys_role_status ON wallet_keys (role_id, status);

CREATE INDEX idx_wallet_keys_kek_version ON wallet_keys (kek_version);

-- +goose Down
DROP TABLE wallet_keys;
