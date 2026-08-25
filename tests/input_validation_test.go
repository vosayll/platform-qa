package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"locali-e2e-engine/pkg/client"
)

func Test_Input_Validation_And_Boundary_Conditions(t *testing.T) {
	ctx := context.Background()

	t.Run("Reject non-Russian phone format in client registration", func(t *testing.T) {
		regReq := client.ClientRegisterRequest{
			PhoneNumber: "+12025550199", // US format
		}
		err := testEngine.ClientAPI.Register(ctx, regReq)
		require.Error(t, err)
		require.Contains(t, err.Error(), "400")
		t.Logf("    ✓ Correctly rejected invalid non-RU phone format: %v", err)
	})

	t.Run("Reject login with empty verification code", func(t *testing.T) {
		loginReq := client.ClientLoginRequest{
			PhoneNumber:      "+79991234567",
			VerificationCode: "", // Empty
		}
		_, err := testEngine.ClientAPI.Login(ctx, loginReq)
		require.Error(t, err)
		require.Contains(t, err.Error(), "400")
		t.Logf("    ✓ Correctly rejected login without verification code: %v", err)
	})

	t.Run("Reject parcel order with negative price", func(t *testing.T) {
		_, clientToken, _ := testEngine.Fixtures.CreateUniqueClient(ctx)
		testEngine.SessionMgr.SetClientSession("c_neg", clientToken, "+79991234567", "Client")

		negReq := client.CreateIndependentOrderRequest{
			AddressA: "Точка А",
			AddressB: "Точка Б",
			Price:    -500, // Invalid negative price
		}
		_, err := testEngine.ClientAPI.CreateIndependentOrder(ctx, negReq)
		require.Error(t, err)
		require.Contains(t, err.Error(), "400")
		t.Logf("    ✓ Correctly rejected order with negative price: %v", err)
	})

	t.Run("Reject order creation without delivery address", func(t *testing.T) {
		_, clientToken, _ := testEngine.Fixtures.CreateUniqueClient(ctx)
		testEngine.SessionMgr.SetClientSession("c_addr", clientToken, "+79991234567", "Client")

		noAddrReq := client.CreateRestaurantOrderRequest{
			RestID:          "rest_123",
			DeliveryAddress: "", // Empty
		}
		_, err := testEngine.ClientAPI.CreateRestaurantOrder(ctx, noAddrReq)
		require.Error(t, err)
		require.Contains(t, err.Error(), "400")
		t.Logf("    ✓ Correctly rejected order without delivery address: %v", err)
	})
}
