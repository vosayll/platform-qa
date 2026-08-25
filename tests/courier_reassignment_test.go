package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"locali-e2e-engine/pkg/client"
)

func Test_Courier_Reassignment_Flow(t *testing.T) {
	ctx := context.Background()

	// 1. Create client and 2 different couriers
	_, clientToken, err := testEngine.Fixtures.CreateUniqueClient(ctx)
	require.NoError(t, err)
	testEngine.SessionMgr.SetClientSession("c1", clientToken, "+79991112233", "Client 1")

	courier1Phone, _, err := testEngine.Fixtures.CreateUniqueCourier(ctx)
	require.NoError(t, err)
	courier2Phone, _, err := testEngine.Fixtures.CreateUniqueCourier(ctx)
	require.NoError(t, err)

	adminToken, err := testEngine.SessionMgr.LoginAdmin(ctx, testEngine.Config.AdminLogin, testEngine.Config.AdminPassword)
	require.NoError(t, err)
	testEngine.SessionMgr.SetAdminSession("admin", adminToken, testEngine.Config.AdminLogin)

	// 2. Create order
	orderReq := client.CreateRestaurantOrderRequest{
		RestID:          "rest_reassign_test",
		DeliveryAddress: "Москва, Смоленская 5",
		PaymentType:     "card",
	}
	order, err := testEngine.ClientAPI.CreateRestaurantOrder(ctx, orderReq)
	require.NoError(t, err)

	// 3. Dispatch to Courier 1
	order1, err := testEngine.AdminAPI.AssignCourier(ctx, order.OrderID, courier1Phone)
	require.NoError(t, err)
	require.Equal(t, courier1Phone, order1.CourierID, "Order must be assigned to Courier 1")
	t.Logf("    ✓ Initially assigned to Courier 1: %s", courier1Phone)

	// 4. Reassign to Courier 2
	order2, err := testEngine.AdminAPI.AssignCourier(ctx, order.OrderID, courier2Phone)
	require.NoError(t, err)
	require.Equal(t, courier2Phone, order2.CourierID, "Order must be successfully reassigned to Courier 2")
	t.Logf("    ✓ Successfully reassigned to Courier 2: %s", courier2Phone)
}
