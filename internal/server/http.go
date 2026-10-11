package server

import (
	"context"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/getkin/kin-openapi/openapi3"
	oapimiddleware "github.com/oapi-codegen/gin-middleware"

	"github.com/kubev2v/rvtools-analyser/internal/config"
	"github.com/kubev2v/rvtools-analyser/internal/server/middlewares"
)

const (
	ProductionServer string = "prod"
	DevServer        string = "dev"
)

type Server struct {
	srv *http.Server
}

type APIGroup struct {
	Swagger    *openapi3.T
	RegisterFn func(router *gin.RouterGroup)
}

func NewServer(cfg *config.Configuration, groups map[string]APIGroup) (*Server, error) {
	gin.SetMode(gin.DebugMode)
	if cfg.Server.ServerMode == ProductionServer {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()
	engine.MaxMultipartMemory = 64 << 20 // max 64Mb

	listenAddress := cfg.Server.ListenAddress
	if listenAddress == "" {
		listenAddress = "127.0.0.1"
	}
	srv := &http.Server{
		Addr:    net.JoinHostPort(listenAddress, strconv.Itoa(cfg.Server.HTTPPort)),
		Handler: engine,
	}

	if cfg.Server.ServerMode == ProductionServer {
		engine.Static("/static", cfg.Server.StaticsFolder)
		engine.Static("/assets", path.Join(cfg.Server.StaticsFolder, "assets"))
		engine.StaticFile("/", path.Join(cfg.Server.StaticsFolder, "index.html"))
		engine.StaticFile("/favicon.ico", path.Join(cfg.Server.StaticsFolder, "favicon.ico"))

		engine.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api") {
				c.JSON(404, gin.H{
					"error": "API endpoint not found",
				})
				return
			}
			c.File(path.Join(cfg.Server.StaticsFolder, "index.html"))
		})
	}

	for prefix, group := range groups {
		router := engine.Group(prefix)

		mws := []gin.HandlerFunc{
			middlewares.Logger(),
			ginzap.RecoveryWithZap(zap.S().Desugar(), true),
		}

		if group.Swagger != nil {
			oapiOpts := oapimiddleware.Options{
				SilenceServersWarning: true,
				ErrorHandler: func(c *gin.Context, message string, statusCode int) {
					c.JSON(statusCode, gin.H{"error": message})
					c.Abort()
				},
			}
			mws = append(mws, oapimiddleware.OapiRequestValidatorWithOptions(group.Swagger, &oapiOpts))
		}

		router.Use(mws...)
		group.RegisterFn(router)
	}

	return &Server{srv: srv}, nil
}

// Start starts the local HTTP server.
func (r *Server) Start(ctx context.Context) error {
	return r.srv.ListenAndServe()
}

func (r *Server) Stop(ctx context.Context) {
	if err := r.srv.Shutdown(ctx); err != nil {
		zap.S().Errorw("server shutdown", "error", err)
	}
}
