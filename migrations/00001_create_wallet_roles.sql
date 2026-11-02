-- +goose Up
CREATE TABLE wallet_roles (
    id UUID PRIMARY KEY,
    service VARCHAR(64) NOT NULL, -- owner service: exchange, banknote, cedex-retail
    purpose VARCHAR(32) NOT NULL, -- free form: executor, gas_station, owner, deposit
    label VARCHAR(64) NOT NULL DEFAULT '', -- purpose refinement: atoken, usdt_high_risk; '' - single role of purpose
    network SMALLINT NOT NULL, -- 1 - ethereum, 2 - tron, 3 - arbitrum
    kind SMALLINT NOT NULL, -- 1 - single (one active key), 2 - pool (several active keys), 3 - deposit (key per external_ref)
    created_at TIMESTAMP NOT NULL,

    CONSTRAINT wallet_roles_network_check CHECK (network IN (1, 2, 3)),
    CONSTRAINT wallet_roles_kind_check CHECK (kind IN (1, 2, 3)),
    CONSTRAINT wallet_roles_unique UNIQUE (service, purpose, label, network)
);

-- +goose Down
DROP TABLE wallet_roles;
