package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/config"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

func TestAdminRoutesDisabledInProduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := logger.New(logger.Config{Level: "error"})
	handlers := &Handlers{Admin: keystorage.NewAdminHandler(nil, log)}

	for env, want := range map[string]int{"production": http.StatusNotFound, "development": http.StatusBadRequest} {
		t.Run(env, func(t *testing.T) {
			cfg := &config.Config{App: config.AppConfig{Environment: env}}
			r := NewRouter(handlers, cfg, log, middleware.NewServiceAuth(nil, log))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/roles", nil))
			if w.Code != want {
				t.Fatalf("code = %d, want %d", w.Code, want)
			}
		})
	}
}
