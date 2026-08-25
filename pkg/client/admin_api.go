package client

import (
	"context"
	"fmt"
)

// AdminAPI provides methods for Director / Dispatcher Admin panel interaction
type AdminAPI struct {
	sessionMgr *SessionManager
}

// NewAdminAPI creates a new Admin/Director API service
func NewAdminAPI(sm *SessionManager) *AdminAPI {
	return &AdminAPI{sessionMgr: sm}
}

// AssignCourier dispatches an order to a specified courier
func (api *AdminAPI) AssignCourier(ctx context.Context, orderID, courierID string) (*OrderResponse, error) {
	token := api.sessionMgr.GetAdminSession().Token
	req := AssignCourierRequest{
		OrderID:   orderID,
		CourierID: courierID,
	}

	var updatedOrder OrderResponse
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/admin/give-order-to-courier", token, req, &updatedOrder)
	if err != nil {
		return nil, fmt.Errorf("admin assign courier failed: %w", err)
	}
	return &updatedOrder, nil
}

// CancelOrder cancels an order from admin panel
func (api *AdminAPI) CancelOrder(ctx context.Context, orderID, reason string) error {
	token := api.sessionMgr.GetAdminSession().Token
	req := map[string]string{
		"order_id": orderID,
		"reason":   reason,
	}

	_, err := api.sessionMgr.HTTPClient().Request(ctx, "POST", "/api/admin/cancel-order", token, req, nil)
	if err != nil {
		return fmt.Errorf("admin cancel order failed: %w", err)
	}
	return nil
}

// GetOrder fetches detailed order data from admin view
func (api *AdminAPI) GetOrder(ctx context.Context, orderID string) (*OrderResponse, error) {
	token := api.sessionMgr.GetAdminSession().Token
	var orderResp OrderResponse
	path := fmt.Sprintf("/api/admin/admin-single?order_id=%s", orderID)
	_, err := api.sessionMgr.HTTPClient().Request(ctx, "GET", path, token, nil, &orderResp)
	if err != nil {
		return nil, fmt.Errorf("admin get order failed: %w", err)
	}
	return &orderResp, nil
}
