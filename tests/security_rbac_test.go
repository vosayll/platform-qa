package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"locali-e2e-engine/pkg/client"
)

func Test_Security_RBAC_And_Token_Isolation(t *testing.T) {
	ctx := context.Background()

	t.Run("Unauthenticated request with missing token returns 401", func(t *testing.T) {
		req := client.CreateRestaurantOrderRequest{
			RestID:          "rest_123",
			DeliveryAddress: "ул. Примерная",
		}
		// Send request without Bearer token
		_, err := testEngine.HTTPClient.Request(ctx, "POST", "/api/clients/create-order", "", req, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "401")
		t.Logf("    ✓ Correctly rejected unauthenticated request: %v", err)
	})

	t.Run("Client token cannot access Director/Admin APIs (403 Forbidden)", func(t *testing.T) {
		clientPhone, clientToken, err := testEngine.Fixtures.CreateUniqueClient(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, clientToken)

		// Client attempts to call admin give-order-to-courier
		assignReq := client.AssignCourierRequest{
			OrderID:   "order-test-id",
			CourierID: "courier-123",
		}
		_, err = testEngine.HTTPClient.Request(ctx, "POST", "/api/admin/give-order-to-courier", clientToken, assignReq, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "403")
		t.Logf("    ✓ Correctly blocked client (%s) from accessing Admin API: %v", clientPhone, err)
	})

	t.Run("Courier token cannot access Restaurant Dish Management APIs (403 Forbidden)", func(t *testing.T) {
		_, courierToken, err := testEngine.Fixtures.CreateUniqueCourier(ctx)
		require.NoError(t, err)

		dishReq := client.CreateDishRequest{
			Name:   "Пицца Несанкционированная",
			Price:  990,
			RestID: "rest_123",
		}
		_, err = testEngine.HTTPClient.Request(ctx, "POST", "/api/rests/dishes", courierToken, dishReq, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "403")
		t.Logf("    ✓ Correctly blocked courier from creating restaurant dishes: %v", err)
	})
}
