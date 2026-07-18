// Copyright (C) 2022-2026, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/internal/service"
	"go.uber.org/zap"
	"maunium.net/go/mautrix/event"

	"github.com/gin-gonic/gin"
)

const putTransactionsTxnIDParam = "txnId"

// TODO@ [GIN-debug] [WARNING] Headers were already written. Wanted to override status code 200 with 401

func newServer(logger *zap.SugaredLogger, hsAccessToken string, port uint64, service service.Service) *server {
	ginRouter := gin.New()
	s := &server{
		logger: logger,
		gin:    ginRouter,
		httpServer: &http.Server{
			Addr:              fmt.Sprintf(":%d", port),
			Handler:           ginRouter,
			ReadHeaderTimeout: time.Second * 10,
		},
		service: service,
	}

	s.gin.Use(middlewareLogger(logger))
	s.gin.Use(middlewareRecover(logger))
	s.gin.Use(middlewareBearerAuth(hsAccessToken))

	s.gin.PUT(fmt.Sprintf("/_matrix/app/v1/transactions/:%s", putTransactionsTxnIDParam), s.putTransactions)
	s.gin.POST("/_matrix/app/v1/ping", s.ping)

	return s
}

type server struct {
	logger     *zap.SugaredLogger
	httpServer *http.Server
	gin        *gin.Engine
	service    service.Service
}

func (s *server) Start() chan error {
	errChan := make(chan error)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("HTTP server panicked: %v", r)
				s.logger.Errorf("recovered from panic: %v", err)
				errChan <- err
			}
			close(errChan)
		}()

		s.logger.Infof("HTTP server listen and serve on %s", s.httpServer.Addr)

		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Errorf("HTTP server stopped listen and serve with error: %w", err)
			errChan <- err
		}
	}()
	return errChan
}

func (s *server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// handlers

type putTransactionsReq struct {
	Events []event.Event `json:"events"`
}

func (s *server) putTransactions(c *gin.Context) {
	reqBody := &putTransactionsReq{}
	if err := c.Bind(reqBody); err != nil {
		s.logger.Error(err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err})
		return
	}

	if err := s.service.ProcessEvents(c.Request.Context(), reqBody.Events); err != nil {
		s.logger.Errorf("failed to process events (txID %s): %v", c.Param(putTransactionsTxnIDParam), err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{})
}

func (s *server) ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}

// middlewares

func middlewareBearerAuth(token string) gin.HandlerFunc {
	const bearer = "Bearer"
	const l = len(bearer)

	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if len(auth) > l+1 && strings.EqualFold(auth[:l], bearer) && auth[l+1:] == token {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

func middlewareRecover(logger *zap.SugaredLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("panic recovered: %v", r)
			}
		}()
		c.Next()
	}
}

func middlewareLogger(logger *zap.SugaredLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.Debug(c.Request.Method, c.Request.URL.Path)
		c.Next()
	}
}
