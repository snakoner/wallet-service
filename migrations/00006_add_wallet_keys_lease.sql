-- +goose Up
ALTER TABLE wallet_keys
    ADD COLUMN lease_id UUID DEFAULT NULL, -- exclusive use of key by one service instance
    ADD COLUMN lease_owner VARCHAR(128) DEFAULT NULL, -- instance that holds lease, for debugging
    ADD COLUMN leased_until TIMESTAMP DEFAULT NULL; -- lease expires at, UTC

CREATE UNIQUE INDEX wallet_keys_lease_id_unique ON wallet_keys (lease_id) WHERE lease_id IS NOT NULL;

-- +goose Down
DROP INDEX wallet_keys_lease_id_unique;

ALTER TABLE wallet_keys
    DROP COLUMN leased_until,
    DROP COLUMN lease_owner,
    DROP COLUMN lease_id;
