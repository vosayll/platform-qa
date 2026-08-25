package client

import (
	"context"
	"fmt"
)

// ClientAPI provides methods for Client App interaction
type ClientAPI struct {
	sessionMgr *SessionManager
}

// NewClientAPI creates a new Client API service
func NewClientAPI(sm *SessionManager) *ClientAPI {
	return &ClientAPI{sessionMgr: sm}
}

// Register registers a new client phone number
func (api *ClientAPI) Register(ctx context.Context, req ClientRegisterRequest) error {
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/clients/register", "", req, nil)
	if err != nil {
		return fmt.Errorf("client register failed: %w", err)
	}
	return nil
}

// Login authenticates client with verification code and sets active session
func (api *ClientAPI) Login(ctx context.Context, req ClientLoginRequest) (string, error) {
	var token string
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/clients/login", "", req, &token)
	if err != nil {
		return "", fmt.Errorf("client login failed: %w", err)
	}

	fullName := fmt.Sprintf("%s %s", req.FirstName, req.LastName)
	api.sessionMgr.SetClientSession(req.PhoneNumber, token, req.PhoneNumber, fullName)
	return token, nil
}

// CreateRestaurantOrder places a food delivery order
func (api *ClientAPI) CreateRestaurantOrder(ctx context.Context, req CreateRestaurantOrderRequest) (*OrderResponse, error) {
	return api.CreateRestaurantOrderWithIdempotency(ctx, req, "")
}

// CreateRestaurantOrderWithIdempotency places a food delivery order with optional Idempotency-Key
func (api *ClientAPI) CreateRestaurantOrderWithIdempotency(ctx context.Context, req CreateRestaurantOrderRequest, idempotencyKey string) (*OrderResponse, error) {
	token := api.sessionMgr.GetClientSession().Token
	var orderResp OrderResponse

	// If idempotency key is passed, we can send header via custom request
	headers := make(map[string]string)
	if idempotencyKey != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}

	_, err := api.sessionMgr.HTTPClient().RequestWithHeaders(ctx, "POST", "/api/clients/create-order", token, headers, req, &orderResp)
	if err != nil {
		return nil, fmt.Errorf("client create order failed: %w", err)
	}
	return &orderResp, nil
}

// CreateIndependentOrder places a point A to point B parcel delivery order
func (api *ClientAPI) CreateIndependentOrder(ctx context.Context, req CreateIndependentOrderRequest) (*OrderResponse, error) {
	token := api.sessionMgr.GetClientSession().Token
	var orderResp OrderResponse
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/clients/independent-order", token, req, &orderResp)
	if err != nil {
		return nil, fmt.Errorf("client create independent order failed: %w", err)
	}
	return &orderResp, nil
}

// CancelOrder cancels client order
func (api *ClientAPI) CancelOrder(ctx context.Context, orderID, reason string) error {
	token := api.sessionMgr.GetClientSession().Token
	path := fmt.Sprintf("/api/clients/orders/%s/cancel", orderID)
	req := map[string]string{"reason": reason}
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", path, token, req, nil)
	if err != nil {
		return fmt.Errorf("client cancel order failed: %w", err)
	}
	return nil
}

// GetOrder fetches current status of an order for client
func (api *ClientAPI) GetOrder(ctx context.Context, orderID string) (*OrderResponse, error) {
	token := api.sessionMgr.GetClientSession().Token
	var orderResp OrderResponse
	path := fmt.Sprintf("/api/clients/active-orders/%s", orderID)
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "GET", path, token, nil, &orderResp)
	if err != nil {
		return nil, fmt.Errorf("client get order failed: %w", err)
	}
	return &orderResp, nil
}
