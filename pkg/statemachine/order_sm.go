package statemachine

import (
	"fmt"
	"sync"
)

// OrderStatus defines domain states for Orders in Locali ecosystem
type OrderStatus string

const (
	StatusNew             OrderStatus = "NEW"               // Backend status: 'new'
	StatusCourierAssigned OrderStatus = "COURIER_ASSIGNED"  // Backend status: 'new' with courier_id
	StatusPreparing       OrderStatus = "PREPARING"         // Backend status: 'cooking'
	StatusReadyForPickup  OrderStatus = "READY_FOR_PICKUP"  // Backend status: 'cooked'
	StatusPickedUp        OrderStatus = "PICKED_UP"         // Backend status: 'shipping' / courierStatus: 'going'
	StatusDelivered       OrderStatus = "DELIVERED"         // Backend status: 'complete'
	StatusCancelled       OrderStatus = "CANCELLED"         // Backend status: 'cancelled'
)

// Role represents an actor role in the ecosystem
type Role string

const (
	RoleClient     Role = "CLIENT"
	RoleRestaurant Role = "RESTAURANT"
	RoleCourier    Role = "COURIER"
	RoleAdmin      Role = "ADMIN"
)

// OrderType represents whether order is from restaurant or point-to-point parcel
type OrderType string

const (
	OrderTypeRestaurant  OrderType = "RESTAURANT"
	OrderTypeIndependent OrderType = "INDEPENDENT" // Point A to Point B
)

// TransitionRule specifies allowed from/to statuses and authorized roles
type TransitionRule struct {
	FromStatus   OrderStatus
	ToStatus     OrderStatus
	AllowedRoles []Role
}

// OrderStateMachine holds transition graph and validation logic
type OrderStateMachine struct {
	mu           sync.RWMutex
	orderHistory map[string][]OrderStatus
}

// NewOrderStateMachine creates an instance of state machine validator
func NewOrderStateMachine() *OrderStateMachine {
	return &OrderStateMachine{
		orderHistory: make(map[string][]OrderStatus),
	}
}

// Allowed Restaurant order transitions
var restaurantTransitions = map[OrderStatus][]TransitionRule{
	StatusNew: {
		{FromStatus: StatusNew, ToStatus: StatusCourierAssigned, AllowedRoles: []Role{RoleAdmin, RoleCourier}},
		{FromStatus: StatusNew, ToStatus: StatusPreparing, AllowedRoles: []Role{RoleRestaurant}},
		{FromStatus: StatusNew, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleClient, RoleRestaurant, RoleAdmin}},
	},
	StatusCourierAssigned: {
		{FromStatus: StatusCourierAssigned, ToStatus: StatusPreparing, AllowedRoles: []Role{RoleRestaurant}},
		{FromStatus: StatusCourierAssigned, ToStatus: StatusPickedUp, AllowedRoles: []Role{RoleCourier, RoleRestaurant}},
		{FromStatus: StatusCourierAssigned, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleRestaurant, RoleAdmin}},
	},
	StatusPreparing: {
		{FromStatus: StatusPreparing, ToStatus: StatusReadyForPickup, AllowedRoles: []Role{RoleRestaurant}},
		{FromStatus: StatusPreparing, ToStatus: StatusCourierAssigned, AllowedRoles: []Role{RoleAdmin}},
		{FromStatus: StatusPreparing, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleRestaurant, RoleAdmin}},
	},
	StatusReadyForPickup: {
		{FromStatus: StatusReadyForPickup, ToStatus: StatusPickedUp, AllowedRoles: []Role{RoleCourier, RoleRestaurant}},
		{FromStatus: StatusReadyForPickup, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleRestaurant, RoleAdmin}},
	},
	StatusPickedUp: {
		{FromStatus: StatusPickedUp, ToStatus: StatusDelivered, AllowedRoles: []Role{RoleCourier, RoleRestaurant, RoleAdmin}},
		{FromStatus: StatusPickedUp, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleAdmin}},
	},
	StatusDelivered: {},
	StatusCancelled: {},
}

// Allowed Independent (Point A to Point B) order transitions
var independentTransitions = map[OrderStatus][]TransitionRule{
	StatusNew: {
		{FromStatus: StatusNew, ToStatus: StatusCourierAssigned, AllowedRoles: []Role{RoleAdmin, RoleCourier}},
		{FromStatus: StatusNew, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleClient, RoleAdmin}},
	},
	StatusCourierAssigned: {
		{FromStatus: StatusCourierAssigned, ToStatus: StatusPickedUp, AllowedRoles: []Role{RoleCourier, RoleAdmin}},
		{FromStatus: StatusCourierAssigned, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleAdmin}},
	},
	StatusPickedUp: {
		{FromStatus: StatusPickedUp, ToStatus: StatusDelivered, AllowedRoles: []Role{RoleCourier, RoleAdmin}},
		{FromStatus: StatusPickedUp, ToStatus: StatusCancelled, AllowedRoles: []Role{RoleAdmin}},
	},
	StatusDelivered: {},
	StatusCancelled: {},
}

// ValidateTransition verifies if transition from current to target status is valid for orderType and actor role
func (sm *OrderStateMachine) ValidateTransition(orderType OrderType, from, to OrderStatus, actor Role) error {
	// Terminal states cannot transition to anything
	if from == StatusDelivered {
		return fmt.Errorf("state machine violation: order in terminal state '%s' cannot be modified", StatusDelivered)
	}
	if from == StatusCancelled {
		return fmt.Errorf("state machine violation: order in terminal state '%s' cannot be modified", StatusCancelled)
	}

	var ruleMap map[OrderStatus][]TransitionRule
	if orderType == OrderTypeRestaurant {
		ruleMap = restaurantTransitions
	} else {
		ruleMap = independentTransitions
	}

	rules, exists := ruleMap[from]
	if !exists {
		return fmt.Errorf("state machine violation: unknown source state '%s'", from)
	}

	for _, rule := range rules {
		if rule.ToStatus == to {
			// Check actor role permission
			roleAllowed := false
			for _, r := range rule.AllowedRoles {
				if r == actor {
					roleAllowed = true
					break
				}
			}
			if !roleAllowed {
				return fmt.Errorf("state machine violation: role '%s' is not authorized to transition order from '%s' to '%s'", actor, from, to)
			}
			return nil
		}
	}

	return fmt.Errorf("state machine violation: illegal state transition from '%s' directly to '%s' (skipped mandatory states)", from, to)
}

// RecordTransition logs a successful transition in the history
func (sm *OrderStateMachine) RecordTransition(orderID string, status OrderStatus) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.orderHistory[orderID] = append(sm.orderHistory[orderID], status)
}

// GetHistory returns transition path of an order
func (sm *OrderStateMachine) GetHistory(orderID string) []OrderStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	history, ok := sm.orderHistory[orderID]
	if !ok {
		return nil
	}
	cp := make([]OrderStatus, len(history))
	copy(cp, history)
	return cp
}
