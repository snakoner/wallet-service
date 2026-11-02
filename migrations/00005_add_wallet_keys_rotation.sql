-- +goose Up
ALTER TABLE wallet_keys
    ADD COLUMN replaced_by UUID DEFAULT NULL REFERENCES wallet_keys(id), -- new key after rotation
    ADD COLUMN rotated_at TIMESTAMP DEFAULT NULL;

-- +goose Down
ALTER TABLE wallet_keys
    DROP COLUMN rotated_at,
    DROP COLUMN replaced_by;
