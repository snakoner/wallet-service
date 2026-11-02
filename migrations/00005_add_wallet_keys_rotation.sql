-- +goose Up
-- 2 - rotating (has unsent txs), 3 - pending (new key after rotation, no gas)
ALTER TABLE wallet_keys DROP CONSTRAINT wallet_keys_status_check;
ALTER TABLE wallet_keys ADD CONSTRAINT wallet_keys_status_check CHECK (status IN (1, 2, 3, 4, 5));

ALTER TABLE wallet_keys
    ADD COLUMN replaced_by UUID DEFAULT NULL REFERENCES wallet_keys(id), -- new key after rotation
    ADD COLUMN rotated_at TIMESTAMP DEFAULT NULL;

-- +goose Down
ALTER TABLE wallet_keys
    DROP COLUMN rotated_at,
    DROP COLUMN replaced_by;

ALTER TABLE wallet_keys DROP CONSTRAINT wallet_keys_status_check;
ALTER TABLE wallet_keys ADD CONSTRAINT wallet_keys_status_check CHECK (status IN (1, 4, 5));
