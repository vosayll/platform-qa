package client

import (
	"context"
	"fmt"
)

// RestAPI provides methods for Restaurant App interaction
type RestAPI struct {
	sessionMgr *SessionManager
}

// NewRestAPI creates a new Restaurant API service
func NewRestAPI(sm *SessionManager) *RestAPI {
	return &RestAPI{sessionMgr: sm}
}

// Register registers a new restaurant
func (api *RestAPI) Register(ctx context.Context, req RestRegisterRequest) error {
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/rests/register", "", req, nil)
	if err != nil {
		return fmt.Errorf("restaurant register failed: %w", err)
	}
	return nil
}

// Login authenticates restaurant and sets active session
func (api *RestAPI) Login(ctx context.Context, req RestLoginRequest) (string, error) {
	var token string
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/rests/login", "", req, &token)
	if err != nil {
		return "", fmt.Errorf("restaurant login failed: %w", err)
	}

	api.sessionMgr.SetRestSession(req.Login, token, req.Login, req.Login)
	return token, nil
}

// ChangeOrderStatus transitions restaurant order status (e.g. 'cooking', 'cooked', 'shipping', 'complete')
func (api *RestAPI) ChangeOrderStatus(ctx context.Context, orderID, status string) (*OrderResponse, error) {
	token := api.sessionMgr.GetRestSession().Token
	req := ChangeRestOrderStatusRequest{
		OrderID: orderID,
		Status:  status,
	}

	var updatedOrder OrderResponse
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/rests/order-status", token, req, &updatedOrder)
	if err != nil {
		return nil, fmt.Errorf("restaurant change order status to '%s' failed: %w", status, err)
	}
	return &updatedOrder, nil
}

// CreateDish adds a dish to the restaurant menu
func (api *RestAPI) CreateDish(ctx context.Context, req CreateDishRequest) (*DishResponse, error) {
	token := api.sessionMgr.GetRestSession().Token
	var dishResp DishResponse
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/rests/dishes", token, req, &dishResp)
	if err != nil {
		return nil, fmt.Errorf("restaurant create dish failed: %w", err)
	}
	return &dishResp, nil
}
