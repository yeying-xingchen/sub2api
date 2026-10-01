package service

import (
	"context"
	"encoding/json"
)

type openAIAccountUpdateSnapshotKey struct{}

// WithOpenAIAccountUpdateSnapshot lets the repository reject an admin write
// whose initial credential snapshot was replaced by an in-flight reauth/refresh.
// The serialized snapshot is never logged or returned to the caller.
func WithOpenAIAccountUpdateSnapshot(ctx context.Context, account *Account) context.Context {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return ctx
	}
	credentials := account.Credentials
	if credentials == nil {
		credentials = map[string]any{}
	}
	if snapshot, err := json.Marshal(credentials); err == nil {
		return context.WithValue(ctx, openAIAccountUpdateSnapshotKey{}, string(snapshot))
	}
	return ctx
}

func OpenAIAccountUpdateSnapshot(ctx context.Context) (string, bool) {
	snapshot, ok := ctx.Value(openAIAccountUpdateSnapshotKey{}).(string)
	return snapshot, ok
}
