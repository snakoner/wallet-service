package key_storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

func (s *Service) CreateRole(ctx context.Context, params CreateRoleParams) (*Role, error) {
	if strings.TrimSpace(params.Service) == "" || strings.TrimSpace(params.Purpose) == "" {
		return nil, ErrInvalidParams
	}

	if _, ok := keysByNetwork[params.Network]; !ok {
		return nil, ErrUnsupportedNetwork
	}

	if _, ok := constants.RoleKindStrings[params.Kind]; !ok {
		return nil, ErrInvalidParams.WithMessage("unsupported role kind")
	}

	rolePolicy, err := policy.Parse(params.Policy, params.Network)
	if err != nil {
		return nil, ErrInvalidParams.WithMessage("invalid policy: " + err.Error())
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate role id: %w", err)
	}

	role := &Role{
		ID:        id,
		Service:   strings.TrimSpace(params.Service),
		Purpose:   strings.TrimSpace(params.Purpose),
		Label:     strings.TrimSpace(params.Label),
		Network:   params.Network,
		Kind:      params.Kind,
		Policy:    rolePolicy.JSON(),
		CreatedAt: time.Now().UTC(),
	}

	if err := s.repo.CreateRole(ctx, s.txm.GetPool(), role); err != nil {
		return nil, err
	}

	s.log.Info().
		Str("role_id", role.ID.String()).
		Str("service", role.Service).
		Str("purpose", role.Purpose).
		Str("label", role.Label).
		Str("network", role.Network.String()).
		Str("kind", role.Kind.String()).
		Msg("wallet role created")

	return role, nil
}

func (s *Service) ListRoles(ctx context.Context, service string) ([]Role, error) {
	return s.repo.ListRoles(ctx, s.txm.GetPool(), strings.TrimSpace(service))
}

func (s *Service) UpdatePolicy(ctx context.Context, roleID uuid.UUID, raw json.RawMessage) (*Role, error) {
	role, err := s.repo.GetRoleByID(ctx, s.txm.GetPool(), roleID)
	if err != nil {
		return nil, err
	}

	rolePolicy, err := policy.Parse(raw, role.Network)
	if err != nil {
		return nil, ErrInvalidParams.WithMessage("invalid policy: " + err.Error())
	}

	role.Policy = rolePolicy.JSON()
	if err := s.repo.UpdateRolePolicy(ctx, s.txm.GetPool(), role.ID, role.Policy); err != nil {
		return nil, err
	}

	s.log.Info().
		Str("role_id", role.ID.String()).
		Str("service", role.Service).
		Str("purpose", role.Purpose).
		RawJSON("policy", role.Policy).
		Msg("wallet role policy updated")

	return role, nil
}
