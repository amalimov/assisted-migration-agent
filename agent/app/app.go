package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/opa"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	v2Handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/bootstrap"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
	service "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

const apiV2 = "/api/v2"

func Run(ctx context.Context, cfg *config.Configuration) error {
	srv, cleanup, err := initV2(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Stop(stopCtx)
	}()

	zap.S().Infof("Starting HTTP server on port %d", cfg.Server.HTTPPort)
	err = srv.Start(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to start http server: %w", err)
	}
	zap.S().Info("server shutdown")
	return nil
}

func initV2(cfg *config.Configuration) (*server.Server, func(), error) {
	opaValidator, err := opa.NewValidatorFromDir(cfg.Agent.OpaPoliciesFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize OPA validator: %w", err)
	}

	pool, err := bootstrap.InitPool(cfg.Agent.DataFolder)
	if err != nil {
		return nil, nil, err
	}

	consoleClient, km, err := initShared(cfg)
	if err != nil {
		return nil, nil, err
	}

	v2SvcMgr := service.NewServiceManager(
		service.WithConfig(cfg),
		service.WithPool(pool),
		service.WithConsoleClient(consoleClient),
		service.WithKeyManager(km),
		service.WithOpaValidator(opaValidator),
	)
	if err := v2SvcMgr.Initialize(); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize v2 service: %w", err)
	}

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		handlers.RegisterValidators(v)
	}

	v2H := v2Handlers.NewHandler(*cfg, v2SvcMgr)

	swagger, err := v2.GetSwagger()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load v2 swagger spec: %w", err)
	}

	srv, err := server.NewServer(cfg, map[string]server.APIGroup{
		apiV2: {
			Swagger: swagger,
			RegisterFn: func(router *gin.RouterGroup) {
				v2.RegisterHandlers(router, v2H)
			},
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http server: %w", err)
	}

	cleanup := func() {
		v2SvcMgr.Stop(context.Background())
		pool.Close()
		zap.S().Info("v2 service and pool closed")
	}

	return srv, cleanup, nil
}

func initShared(cfg *config.Configuration) (*console.Client, *crypto.KeyManager, error) {
	jwt := ""
	if cfg.Auth.Enabled {
		data, err := os.ReadFile(cfg.Auth.JWTFilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read agent's jwt: %w", err)
		}
		if len(data) == 0 {
			return nil, nil, errors.New("failed to read agent's jwt. the JWT is empty")
		}
		jwt = strings.TrimSpace(string(data))
	}

	consoleClient, err := console.NewConsoleClient(cfg.Console.URL, jwt)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create console client: %w", err)
	}

	keyManager, err := crypto.NewKeyManager(cfg.Agent.DataFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize key manager: %w", err)
	}

	return consoleClient, keyManager, nil
}
