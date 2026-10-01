package service

import (
	"context"
	"time"
)

func (s *RateLimitService) tryOpenAIAutoReauth(ctx context.Context, account *Account) bool {
	if s.openAIAutoReauth == nil {
		return false
	}
	return s.openAIAutoReauth.Handle401(ctx, account, func(id int64) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.tempUnschedCache != nil {
			_ = s.tempUnschedCache.DeleteTempUnsched(cleanupCtx, id)
		}
		// The claim owns only its DB temporary pause. Independent runtime blocks
		// (quota, workspace restrictions, etc.) must remain in force.
	})
}
