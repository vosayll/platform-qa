package tests

import (
	"testing"

	"github.com/stretchr/testify/require"
	"locali-e2e-engine/pkg/statemachine"
)

func Test_StateMachine_IllegalJumpsAndUnauthorizedRoles(t *testing.T) {
	sm := statemachine.NewOrderStateMachine()

	t.Run("Reject illegal jump: NEW directly to DELIVERED", func(t *testing.T) {
		err := sm.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusNew, statemachine.StatusDelivered, statemachine.RoleCourier)
		require.Error(t, err)
		require.Contains(t, err.Error(), "illegal state transition")
		t.Logf("    ✓ Correctly rejected illegal jump: %v", err)
	})

	t.Run("Reject illegal jump: PREPARING directly to DELIVERED", func(t *testing.T) {
		err := sm.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusPreparing, statemachine.StatusDelivered, statemachine.RoleCourier)
		require.Error(t, err)
		require.Contains(t, err.Error(), "illegal state transition")
		t.Logf("    ✓ Correctly rejected illegal jump: %v", err)
	})

	t.Run("Reject unauthorized role: Client trying to mark order as DELIVERED", func(t *testing.T) {
		err := sm.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusPickedUp, statemachine.StatusDelivered, statemachine.RoleClient)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not authorized")
		t.Logf("    ✓ Correctly rejected unauthorized role: %v", err)
	})

	t.Run("Reject unauthorized role: Restaurant trying to assign courier", func(t *testing.T) {
		err := sm.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusNew, statemachine.StatusCourierAssigned, statemachine.RoleRestaurant)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not authorized")
		t.Logf("    ✓ Correctly rejected unauthorized role: %v", err)
	})
}
