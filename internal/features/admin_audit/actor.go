package admin_audit

import (
	"context"
	"errors"
	"strings"
)

const maxActorLength = 128

var ErrNoActor = errors.New("admin actor is not set: set WALLET_ADMIN_ACTOR for wallet-admin")

type actorKey struct{}

type actor struct {
	name   string
	source string
}

func WithActor(ctx context.Context, name, source string) context.Context {
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > maxActorLength {
		name = string(runes[:maxActorLength])
	}

	if name == "" {
		return ctx
	}

	return context.WithValue(ctx, actorKey{}, actor{name: name, source: source})
}

func ActorFrom(ctx context.Context) (name, source string, ok bool) {
	a, ok := ctx.Value(actorKey{}).(actor)
	return a.name, a.source, ok
}
