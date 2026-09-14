package storage

import "context"

type adminActorContextKey struct{}

// WithAdminActor carries the authenticated administrator into the storage
// transaction that records an administrative mutation. The value is never
// derived from request payloads.
func WithAdminActor(ctx context.Context, adminUserID string) context.Context {
	if ctx == nil || !validOpaqueIdentifier(adminUserID) {
		return ctx
	}
	return context.WithValue(ctx, adminActorContextKey{}, adminUserID)
}

func adminActor(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(adminActorContextKey{}).(string)
	return value
}

func includeAdminActor(ctx context.Context, details map[string]string) map[string]string {
	actor := adminActor(ctx)
	if actor == "" {
		return details
	}
	copy := make(map[string]string, len(details)+1)
	for key, value := range details {
		copy[key] = value
	}
	copy["admin_user_id"] = actor
	return copy
}
