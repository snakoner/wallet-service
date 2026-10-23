package main

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	aes256 "github.com/w3-developers/go-aes256"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/config"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
	serviceclient "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/service_client"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/signing"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/ethereum"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/tron"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/validation"
)

// @title       Wallet Service API
// @version     1.0
// @description Wallet key storage and transaction signing.

// @securityDefinitions.apikey ServiceKey
// @in header
// @name X-Service-Key
func main() {
	cfg := config.MustLoad()

	serviceName := "wallet-service"

	log := logger.New(logger.Config{
		Level:       cfg.Log.Level,
		ServiceName: serviceName,
		Environment: cfg.App.Environment,
		Pretty:      cfg.Log.Pretty,
	})

	if err := validation.Register(); err != nil {
		log.Fatal().Err(err).Msg("failed to register validators")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DB.DSN())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to postgres")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ping postgres")
	}
	log.Info().Msg("connected to postgres")

	txm := db.NewPgxTxManager(pool)

	kek, err := hex.DecodeString(cfg.KeyStorage.KEK)
	if err != nil || len(kek) != 32 {
		log.Fatal().Msg("KEY_STORAGE_KEK must be 32 bytes in hex")
	}

	keyStorageService := keystorage.NewService(
		log,
		keystorage.NewRepository(),
		txm,
		aes256.NewAES256GCM(kek),
		cfg.KeyStorage.KEKVersion,
	)

	signingService := signing.NewService(
		log,
		keyStorageService,
		map[constants.Network]signing.TxSigner{
			constants.NetworkEthereum: ethereum.NewSigner(big.NewInt(cfg.Ethereum.ChainID)),
			constants.NetworkTron:     tron.NewSigner(),
			constants.NetworkArbitrum: ethereum.NewSigner(big.NewInt(cfg.Arbitrum.ChainID)),
		},
	)

	handlers := &app.Handlers{
		KeyStorage: keystorage.NewHandler(keyStorageService, log),
		Admin:      keystorage.NewAdminHandler(keyStorageService, log),
		Signing:    signing.NewHandler(signingService, log),
	}

	serviceClientService := serviceclient.NewService(log, serviceclient.NewRepository(), txm)
	serviceAuth := middleware.NewServiceAuth(serviceClientService, log)

	router := app.NewRouter(handlers, cfg, log, serviceAuth)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr(),
		Handler:      router,
		IdleTimeout:  60 * time.Second,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info().Str("addr", cfg.HTTP.Addr()).Msg("starting server")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal().Err(err).Msg("server shutdown error")
	}

	log.Info().Msg("server stopped")
}
