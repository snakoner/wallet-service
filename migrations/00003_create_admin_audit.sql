-- +goose Up
CREATE TABLE admin_audit (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP NOT NULL,
    actor VARCHAR(128) NOT NULL, -- who: WALLET_ADMIN_ACTOR of wallet-admin, X-Admin-Actor of admin API
    source VARCHAR(16) NOT NULL, -- cli, api
    action VARCHAR(64) NOT NULL, -- key.revoke, role.policy_update...
    service VARCHAR(64) DEFAULT NULL, -- service of changed role, key or service key
    role_id UUID DEFAULT NULL, -- no FK: audit must not block changes of other tables
    key_id UUID DEFAULT NULL,
    service_client_id UUID DEFAULT NULL,
    details JSONB NOT NULL DEFAULT '{}' -- what changed, never private keys or service keys
);

CREATE INDEX idx_admin_audit_created ON admin_audit (created_at);

CREATE INDEX idx_admin_audit_role_created ON admin_audit (role_id, created_at) WHERE role_id IS NOT NULL;

CREATE INDEX idx_admin_audit_key_created ON admin_audit (key_id, created_at) WHERE key_id IS NOT NULL;

-- +goose Down
DROP TABLE admin_audit;
