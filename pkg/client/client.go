// Package client calls wallet-service API of services (X-Service-Key): role keys, deposit keys, leases and signing.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	NetworkEthereum = "ethereum"
	NetworkTron     = "tron"
	NetworkArbitrum = "arbitrum"
)

const (
	StatusActive   = "active"
	StatusRotating = "rotating"
	StatusPending  = "pending"
	StatusDisabled = "disabled"
	StatusRevoked  = "revoked"
)

type Key struct {
	ID          uuid.UUID  `json:"id"`
	Service     string     `json:"service"`
	Purpose     string     `json:"purpose"`
	Label       string     `json:"label"`
	Network     string     `json:"network"`
	Kind        string     `json:"kind"`
	Alias       *string    `json:"alias,omitempty"`
	Address     string     `json:"address"`
	ExternalRef *string    `json:"external_ref,omitempty"`
	Status      string     `json:"status"`
	ReplacedBy  *uuid.UUID `json:"replaced_by,omitempty"`
	LeaseOwner  *string    `json:"lease_owner,omitempty"`
	LeasedUntil *time.Time `json:"leased_until,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type DepositKeyRequest struct {
	Network     string  `json:"network"`
	Label       string  `json:"label"`
	ExternalRef string  `json:"external_ref"`
	Alias       *string `json:"alias,omitempty"`
}

type LeaseRequest struct {
	Network    string      `json:"network"`
	Label      string      `json:"label"`
	Owner      string      `json:"owner"`                 // who holds lease, e.g. pod name
	TTLSeconds int         `json:"ttl_seconds,omitempty"` // 30..600, default 60
	Exclude    []uuid.UUID `json:"exclude,omitempty"`     // keys not to lease, e.g. without gas
}

type Lease struct {
	LeaseID   uuid.UUID `json:"lease_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Key       Key       `json:"key"`
}

type SignRequest struct {
	Network string     `json:"network"`
	RawTx   string     `json:"raw_tx"`
	LeaseID *uuid.UUID `json:"lease_id,omitempty"` // required for active single and pool keys
}

type SignedTx struct {
	SignedTx string `json:"signed_tx"` // hex: ethereum - eth_sendRawTransaction, tron - /wallet/broadcasthex
	TxHash   string `json:"tx_hash"`
}

type Client struct {
	baseURL    string
	serviceKey string
	http       *http.Client
}

func New(baseURL, serviceKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	return &Client{baseURL: strings.TrimRight(baseURL, "/"), serviceKey: serviceKey, http: httpClient}
}

func (c *Client) GetKey(ctx context.Context, purpose, label, network string) (*Key, error) {
	var key Key
	if err := c.do(ctx, http.MethodGet, rolePath(purpose, "key")+roleQuery(label, network), nil, &key); err != nil {
		return nil, err
	}

	return &key, nil
}

func (c *Client) ListKeys(ctx context.Context, purpose, label, network string) ([]Key, error) {
	var keys []Key
	if err := c.do(ctx, http.MethodGet, rolePath(purpose, "keys")+roleQuery(label, network), nil, &keys); err != nil {
		return nil, err
	}

	return keys, nil
}

func (c *Client) DepositKey(ctx context.Context, purpose string, req DepositKeyRequest) (*Key, error) {
	var key Key
	if err := c.do(ctx, http.MethodPost, rolePath(purpose, "deposit-key"), req, &key); err != nil {
		return nil, err
	}

	return &key, nil
}

func (c *Client) AcquireLease(ctx context.Context, purpose string, req LeaseRequest) (*Lease, error) {
	var lease Lease
	if err := c.do(ctx, http.MethodPost, rolePath(purpose, "lease"), req, &lease); err != nil {
		return nil, err
	}

	return &lease, nil
}

func (c *Client) ExtendLease(ctx context.Context, leaseID uuid.UUID, ttlSeconds int) (time.Time, error) {
	var out struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	body := struct {
		TTLSeconds int `json:"ttl_seconds,omitempty"`
	}{ttlSeconds}
	if err := c.do(ctx, http.MethodPost, "/api/v1/leases/"+leaseID.String()+"/extend", body, &out); err != nil {
		return time.Time{}, err
	}

	return out.ExpiresAt, nil
}

func (c *Client) ReleaseLease(ctx context.Context, leaseID uuid.UUID) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/leases/"+leaseID.String(), nil, nil)
}

func (c *Client) SignTransaction(ctx context.Context, keyID uuid.UUID, req SignRequest) (*SignedTx, error) {
	var signed SignedTx
	if err := c.do(ctx, http.MethodPost, "/api/v1/keys/"+keyID.String()+"/sign-transaction", req, &signed); err != nil {
		return nil, err
	}

	return &signed, nil
}

func rolePath(purpose, action string) string {
	return "/api/v1/roles/" + url.PathEscape(purpose) + "/" + action
}

func roleQuery(label, network string) string {
	return "?" + url.Values{"label": {label}, "network": {network}}.Encode()
}

type response struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *Error          `json:"error"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}

	req.Header.Set("X-Service-Key", c.serviceKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("wallet-service %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	var res response
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return fmt.Errorf("wallet-service %s %s: status %d, decode response: %w", method, path, resp.StatusCode, err)
	}

	if !res.Success {
		apiErr := res.Error
		if apiErr == nil {
			apiErr = &Error{}
		}

		apiErr.Status = resp.StatusCode
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			apiErr.RetryAfter = time.Duration(seconds) * time.Second
		}

		return fmt.Errorf("wallet-service %s %s: %w", method, path, apiErr)
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(res.Data, out); err != nil {
		return fmt.Errorf("wallet-service %s %s: decode data: %w", method, path, err)
	}

	return nil
}
