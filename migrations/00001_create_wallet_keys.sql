-- +goose Up
CREATE TABLE wallet_roles (
    id UUID PRIMARY KEY,
    service VARCHAR(64) NOT NULL, -- owner service: exchange, banknote, cedex-retail
    purpose VARCHAR(32) NOT NULL, -- free form: executor, gas_station, owner, deposit
    label VARCHAR(64) NOT NULL DEFAULT '', -- purpose refinement: atoken, usdt_high_risk; '' - single role of purpose
    network SMALLINT NOT NULL, -- 1 - ethereum, 2 - tron, 3 - arbitrum
    kind SMALLINT NOT NULL, -- 1 - single (one active key), 2 - pool (several active keys), 3 - deposit (key per external_ref)
    policy JSONB NOT NULL DEFAULT '{}', -- signing rules
    created_at TIMESTAMP NOT NULL,

    CONSTRAINT wallet_roles_network_check CHECK (network IN (1, 2, 3)),
    CONSTRAINT wallet_roles_kind_check CHECK (kind IN (1, 2, 3)),
    CONSTRAINT wallet_roles_unique UNIQUE (service, purpose, label, network)
);

CREATE TABLE wallet_keys (
    id UUID PRIMARY KEY,
    role_id UUID NOT NULL REFERENCES wallet_roles(id),
    network SMALLINT NOT NULL, -- copy of role network
    alias VARCHAR(128) DEFAULT NULL, -- optional human-readable name
    address VARCHAR(64) NOT NULL,
    external_ref VARCHAR(128) DEFAULT NULL, -- deposit role: order id
    status SMALLINT NOT NULL, -- 0 - active, 1 - rotating (has unsent txs), 2 - pending (new key after rotation, no gas), 3 - disabled (can become active again), 4 - revoked
    key_cipher BYTEA NOT NULL,
    dek_cipher BYTEA NOT NULL,
    kek_version INTEGER NOT NULL,
    replaced_by UUID DEFAULT NULL REFERENCES wallet_keys(id), -- new key after rotation
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    rotated_at TIMESTAMP DEFAULT NULL,
    revoked_at TIMESTAMP DEFAULT NULL,
    lease_id UUID DEFAULT NULL, -- exclusive use of key by one service instance
    lease_owner VARCHAR(128) DEFAULT NULL, -- instance that holds lease, for debugging
    leased_until TIMESTAMP DEFAULT NULL, -- lease expires at, UTC

    CONSTRAINT wallet_keys_network_check CHECK (network IN (1, 2, 3)),
    CONSTRAINT wallet_keys_status_check CHECK (status IN (0, 1, 2, 3, 4)),
    CONSTRAINT wallet_keys_network_address_unique UNIQUE (network, address),
    CONSTRAINT wallet_keys_role_external_ref_unique UNIQUE (role_id, external_ref)
);

CREATE INDEX idx_wallet_keys_role_status ON wallet_keys (role_id, status);

CREATE INDEX idx_wallet_keys_kek_version ON wallet_keys (kek_version);

CREATE UNIQUE INDEX wallet_keys_lease_id_unique ON wallet_keys (lease_id) WHERE lease_id IS NOT NULL;

-- +goose Down
DROP TABLE wallet_keys;
DROP TABLE wallet_roles;
