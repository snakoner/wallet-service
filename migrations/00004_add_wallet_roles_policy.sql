-- +goose Up
ALTER TABLE wallet_roles ADD COLUMN policy JSONB NOT NULL DEFAULT '{}'; -- signing rules

-- +goose Down
ALTER TABLE wallet_roles DROP COLUMN policy;
