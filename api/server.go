package api

import (
	"encoding/json"
	"net/http"

	x402types "github.com/coinbase/x402/go/types"
	_ "github.com/gosuda/x402-facilitator/api/swagger"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"

	"github.com/gosuda/x402-facilitator/api/middleware"
	"github.com/gosuda/x402-facilitator/types"
)

// @title        x402 Facilitator API
// @version      1.0
// @description  API server for x402 payment facilitator
type server struct {
	*echo.Echo
	facilitator types.SchemeNetworkFacilitator
}

var _ http.Handler = (*server)(nil)

func NewServer(facilitator types.SchemeNetworkFacilitator) *server {
	s := &server{
		Echo:        echo.New(),
		facilitator: facilitator,
	}

	s.Use(middleware.RequestID())
	s.Use(middleware.Logger())
	s.Use(middleware.ErrorWrapper())
	s.Use(echomiddleware.RecoverWithConfig(echomiddleware.RecoverConfig{
		DisableErrorHandler: true,
	}))
	s.Use(echomiddleware.CORS())

	s.POST("/verify", s.Verify)
	s.POST("/settle", s.Settle)
	s.GET("/supported", s.Supported)
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

	// Convert local types to SDK types
	sdkPayload := x402types.PaymentPayload(settleRequest.PaymentHeader.PaymentPayload)
	sdkReq := x402types.PaymentRequirements(settleRequest.PaymentRequirements.PaymentRequirements)

	settle, err := s.facilitator.Settle(ctx, sdkPayload, sdkReq)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Convert SDK response to local response
	response := &types.PaymentSettleResponse{
		Success:   settle.Success,
		TxHash:    settle.Transaction,
		NetworkId: string(settle.Network),
		Error:     settle.ErrorReason,
	}

	return c.JSON(http.StatusOK, response)
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

	// Convert local types to SDK types
	sdkPayload := x402types.PaymentPayload(requirement.PaymentHeader.PaymentPayload)
	sdkReq := x402types.PaymentRequirements(requirement.PaymentRequirements.PaymentRequirements)

	verified, err := s.facilitator.Verify(ctx, sdkPayload, sdkReq)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Convert SDK response to local response
	response := &types.PaymentVerifyResponse{
		IsValid:       verified.IsValid,
		InvalidReason: verified.InvalidReason,
		Payer:         verified.Payer,
	}

	return c.JSON(http.StatusOK, response)
}

// Supported returns the list of supported payment kinds
// @Summary      List supported kinds
// @Description  Get supported payment kinds
// @Tags         payments
// @Produce      json
// @Success      200  {array}   types.SupportedKind
// @Failure      404  {object}  echo.HTTPError
// @Router       /supported [get]
func (s *server) Supported(c echo.Context) error {
	// Build supported kinds from facilitator's scheme info
	kinds := []*types.SupportedKind{
		{
			Scheme:  s.facilitator.Scheme(),
			Network: "eip155:*", // TODO: Get actual network from config
		},
	}

	if len(kinds) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "No supported payment kinds found")
	}

	return c.JSON(http.StatusOK, kinds)
}
