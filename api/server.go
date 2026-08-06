package api

import (
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	_ "github.com/gosuda/x402-facilitator/api/swagger"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"

	"github.com/gosuda/x402-facilitator/api/middleware"
	"github.com/gosuda/x402-facilitator/facilitator"
	"github.com/gosuda/x402-facilitator/types"
)

// @title        x402 Facilitator API
// @version      1.0
// @description  API server for x402 payment facilitator
type server struct {
	*echo.Echo
	facilitator facilitator.Facilitator
}

// Options tune the surface without changing it for anyone who does not ask.
//
// Both default to off, so an existing deployment behaves exactly as before. They exist because a
// facilitator reachable from the open internet is a key holding gas: without a limiter it is a
// faucet that anyone can drain by asking it to broadcast, and the cost of that is real money
// rather than noisy logs.
type Options struct {
	// SettleRateLimit caps requests per second per client IP on the settling paths. Zero leaves
	// them unlimited.
	SettleRateLimit float64
	// SettleBurst allows short bursts above the rate. Defaults to twice the rate when unset.
	SettleBurst int
}

var _ http.Handler = (*server)(nil)

func NewServer(facilitator facilitator.Facilitator, opts ...Options) *server {
	s := &server{
		Echo:        echo.New(),
		facilitator: facilitator,
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}

	s.Use(middleware.RequestID())
	s.Use(middleware.Logger())
	s.Use(middleware.ErrorWrapper())
	s.Use(echomiddleware.RecoverWithConfig(echomiddleware.RecoverConfig{
		DisableErrorHandler: true,
	}))
	s.Use(echomiddleware.CORS())

	// The limiter guards the paths that spend: /settle broadcasts, and /verify is the cheap
	// probe an attacker would use to hunt for a context worth broadcasting.
	var paid []echo.MiddlewareFunc
	if o.SettleRateLimit > 0 {
		burst := o.SettleBurst
		if burst <= 0 {
			burst = int(o.SettleRateLimit * 2)
		}
		paid = append(paid, echomiddleware.RateLimiterWithConfig(echomiddleware.RateLimiterConfig{
			Store: echomiddleware.NewRateLimiterMemoryStoreWithConfig(
				echomiddleware.RateLimiterMemoryStoreConfig{
					Rate:      rate.Limit(o.SettleRateLimit),
					Burst:     burst,
					ExpiresIn: 3 * time.Minute,
				},
			),
		}))
	}

	s.POST("/verify", s.Verify, paid...)
	s.POST("/settle", s.Settle, paid...)
	s.GET("/supported", s.Supported)
	s.GET("/health", s.Health)
	s.GET("/ready", s.Ready)
	s.GET("/swagger/*", echoSwagger.WrapHandler)

	return s
}

// Settle handles payment settlement requests
// @Summary      Settle payment
// @Description  Settle a payment using the facilitator
// @Tags         payments
// @Accept       json
// @Produce      json
// @Param        body  body      types.PaymentSettleRequest  true  "Settlement request"
// @Success      200   {object}  types.PaymentSettleResponse
// @Failure      400   {object}  echo.HTTPError
// @Failure      500   {object}  echo.HTTPError
// @Router       /settle [post]
func (s *server) Settle(c echo.Context) error {
	ctx := c.Request().Context()

	settleRequest := &types.PaymentSettleRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(settleRequest); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Received malformed settlement request")
	}

	settle, err := s.facilitator.Settle(ctx, &settleRequest.PaymentPayload, &settleRequest.PaymentRequirements)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, settle)
}

// Verify handles payment verification requests
// @Summary      Verify payment
// @Description  Verify a payment using the facilitator
// @Tags         payments
// @Accept       json
// @Produce      json
// @Param        body  body      types.PaymentVerifyRequest  true  "Payment verification request"
// @Success      200   {object}  types.PaymentVerifyResponse
// @Failure      400   {object}  echo.HTTPError
// @Failure      500   {object}  echo.HTTPError
// @Router       /verify [post]
func (s *server) Verify(c echo.Context) error {
	ctx := c.Request().Context()

	// validate payment requirements
	requirement := &types.PaymentVerifyRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(requirement); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Received malformed payment requirements")
	}

	verified, err := s.facilitator.Verify(ctx, &requirement.PaymentPayload, &requirement.PaymentRequirements)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, verified)
}

// Supported returns the list of supported payment kinds
// @Summary      List supported kinds
// @Description  Get supported payment kinds
// @Tags         payments
// @Produce      json
// @Success      200  {object}  types.SupportedResponse
// @Failure      404  {object}  echo.HTTPError
// @Router       /supported [get]
func (s *server) Supported(c echo.Context) error {
	resp := s.facilitator.Supported()
	if resp == nil || len(resp.Kinds) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "No supported payment kinds found")
	}

	return c.JSON(http.StatusOK, resp)
}

// Health answers whether the process is alive. Nothing else - a liveness probe that consults the
// chain would have a restart loop as its failure mode, which is the wrong cure for a bad RPC.
//
// @Summary  Liveness
// @Success  200 {object} map[string]string
// @Router   /health [get]
func (s *server) Health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Ready answers whether this instance can presently do its job, which is a different question.
// A facilitator that cannot reach the chain, or whose fee payer is out of gas, is healthy and
// useless; 503 takes it out of rotation without killing it.
//
// A facilitator that does not implement ReadinessChecker has nothing to add beyond being alive,
// and says so rather than claiming a check it never ran.
//
// @Summary  Readiness
// @Success  200 {object} map[string]string
// @Failure  503 {object} map[string]string
// @Router   /ready [get]
func (s *server) Ready(c echo.Context) error {
	checker, ok := s.facilitator.(facilitator.ReadinessChecker)
	if !ok {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "checked": "liveness only"})
	}
	if err := checker.Ready(c.Request().Context()); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"reason": err.Error(),
		})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
}
