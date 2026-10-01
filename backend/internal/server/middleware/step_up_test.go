package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEnforceStepUpAllowsSensitiveOperationsWithoutTotp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/sensitive", nil)

	ok := enforceStepUp(c, nil, nil, nil)

	require.True(t, ok)
	require.False(t, c.IsAborted())
}

func TestEnforceStepUpAllowsAdminAPIKeyWithoutTotp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/sensitive", nil)
	c.Set("auth_method", "admin_api_key")

	ok := EnforceStepUp(c, nil, nil, nil)

	require.True(t, ok)
	require.False(t, c.IsAborted())
}

func TestStepUpMiddlewarePassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/sensitive", gin.HandlerFunc(stepUpAuth(nil, nil, nil)), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sensitive", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestStepUpSettingsOrNilKeepsNilNil(t *testing.T) {
	require.Nil(t, stepUpSettingsOrNil(nil))
}
