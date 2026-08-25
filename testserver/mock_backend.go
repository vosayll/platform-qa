package testserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"locali-e2e-engine/pkg/client"
)

// MockBackend simulates the Locali microservice ecosystem with strict RBAC, idempotency and edge-case handling
type MockBackend struct {
	server       *httptest.Server
	mu           sync.RWMutex
	orders       map[string]*client.OrderResponse
	orderSeq     int
	clients      map[string]string // phone -> token
	rests        map[string]string // login -> token
	couriers     map[string]string // phone -> token
	dishes       map[int]*client.DishResponse
	dishSeq      int
	idempotency  map[string]*client.OrderResponse // key -> response
}

// NewMockBackend creates and starts an HTTP mock server
func NewMockBackend() *MockBackend {
	mb := &MockBackend{
		orders:      make(map[string]*client.OrderResponse),
		clients:     make(map[string]string),
		rests:       make(map[string]string),
		couriers:    make(map[string]string),
		dishes:      make(map[int]*client.DishResponse),
		idempotency: make(map[string]*client.OrderResponse),
	}

	mux := http.NewServeMux()

	// Admin
	mux.HandleFunc("/api/admin/login", mb.handleAdminLogin)
	mux.HandleFunc("/api/admin/give-order-to-courier", mb.authGuard("admin", mb.handleAdminAssignCourier))
	mux.HandleFunc("/api/admin/cancel-order", mb.authGuard("admin", mb.handleAdminCancelOrder))
	mux.HandleFunc("/api/admin/admin-single", mb.authGuard("admin", mb.handleAdminGetOrder))
	mux.HandleFunc("/api/admin/clients", mb.authGuard("admin", mb.handleAdminListClients))

	// Client
	mux.HandleFunc("/api/clients/register", mb.handleClientRegister)
	mux.HandleFunc("/api/clients/login", mb.handleClientLogin)
	mux.HandleFunc("/api/clients/create-order", mb.authGuard("client", mb.handleClientCreateOrder))
	mux.HandleFunc("/api/clients/independent-order", mb.authGuard("client", mb.handleClientCreateIndependentOrder))
	mux.HandleFunc("/api/clients/orders/", mb.authGuard("client", mb.handleClientOrderOperations))
	mux.HandleFunc("/api/clients/active-orders/", mb.authGuard("client", mb.handleClientGetOrder))

	// Restaurant
	mux.HandleFunc("/api/rests/register", mb.handleRestRegister)
	mux.HandleFunc("/api/rests/login", mb.handleRestLogin)
	mux.HandleFunc("/api/rests/order-status", mb.authGuard("rest", mb.handleRestOrderStatus))
	mux.HandleFunc("/api/rests/dishes", mb.authGuard("rest", mb.handleRestDishes))

	// Courier
	mux.HandleFunc("/api/couriers/register", mb.handleCourierRegister)
	mux.HandleFunc("/api/couriers/login", mb.handleCourierLogin)
	mux.HandleFunc("/api/couriers/take-order", mb.authGuard("courier", mb.handleCourierTakeOrder))
	mux.HandleFunc("/api/couriers/change-status", mb.authGuard("courier", mb.handleCourierChangeStatus))

	mb.server = httptest.NewServer(mux)
	return mb
}

// URL returns the mock server address
func (mb *MockBackend) URL() string {
	return mb.server.URL
}

// Close stops the server
func (mb *MockBackend) Close() {
	mb.server.Close()
}

func (mb *MockBackend) authGuard(requiredRole string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"unauthorized","message":"Authorization header missing"}`, http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Role checks
		if requiredRole == "admin" && !strings.Contains(token, "admin") {
			http.Error(w, `{"error":"forbidden","message":"ADMIN_ONLY access required"}`, http.StatusForbidden)
			return
		}
		if requiredRole == "rest" && !strings.Contains(token, "rest") {
			http.Error(w, `{"error":"forbidden","message":"REST_ONLY access required"}`, http.StatusForbidden)
			return
		}
		if requiredRole == "courier" && !strings.Contains(token, "courier") {
			http.Error(w, `{"error":"forbidden","message":"COURIER_ONLY access required"}`, http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

func (mb *MockBackend) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req client.AdminLoginRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	token := "jwt_mock_admin_token_super_root"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(token)
}

func (mb *MockBackend) handleClientRegister(w http.ResponseWriter, r *http.Request) {
	var req client.ClientRegisterRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Validate phone format: +7XXXXXXXXXX
	if !strings.HasPrefix(req.PhoneNumber, "+7") || len(req.PhoneNumber) < 11 {
		http.Error(w, `{"error":"bad_request","message":"Поддерживаются только номера РФ формата +7XXXXXXXXXX"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "success"})
}

func (mb *MockBackend) handleClientLogin(w http.ResponseWriter, r *http.Request) {
	var req client.ClientLoginRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.VerificationCode == "" {
		http.Error(w, `{"error":"bad_request","message":"Не указан проверочный код"}`, http.StatusBadRequest)
		return
	}

	token := fmt.Sprintf("jwt_mock_client_%s", req.PhoneNumber)
	mb.mu.Lock()
	mb.clients[req.PhoneNumber] = token
	mb.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(token)
}

func (mb *MockBackend) handleRestRegister(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "success"})
}

func (mb *MockBackend) handleRestLogin(w http.ResponseWriter, r *http.Request) {
	var req client.RestLoginRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	token := fmt.Sprintf("jwt_mock_rest_%s", req.Login)
	mb.mu.Lock()
	mb.rests[req.Login] = token
	mb.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(token)
}

func (mb *MockBackend) handleCourierRegister(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "success"})
}

func (mb *MockBackend) handleCourierLogin(w http.ResponseWriter, r *http.Request) {
	var req client.CourierLoginRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	token := fmt.Sprintf("jwt_mock_courier_%s", req.PhoneNumber)
	mb.mu.Lock()
	mb.couriers[req.PhoneNumber] = token
	mb.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(token)
}

func (mb *MockBackend) handleClientCreateOrder(w http.ResponseWriter, r *http.Request) {
	idemKey := r.Header.Get("Idempotency-Key")

	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Check Idempotency
	if idemKey != "" {
		if cached, exists := mb.idempotency[idemKey]; exists {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cached)
			return
		}
	}

	var req client.CreateRestaurantOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}

	if req.RestID == "" || req.DeliveryAddress == "" {
		http.Error(w, `{"error":"bad_request","message":"restId and deliveryAddress are required"}`, http.StatusBadRequest)
		return
	}

	mb.orderSeq++
	orderID := uuid.New().String()
	order := &client.OrderResponse{
		OrderID:         orderID,
		OrderNumber:     1000 + mb.orderSeq,
		Status:          "new",
		CourierStatus:   "",
		ClientID:        "client_uuid",
		RestID:          req.RestID,
		Price:           1250.0,
		DeliveryAddress: req.DeliveryAddress,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	mb.orders[orderID] = order

	if idemKey != "" {
		mb.idempotency[idemKey] = order
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleClientCreateIndependentOrder(w http.ResponseWriter, r *http.Request) {
	var req client.CreateIndependentOrderRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.AddressA == "" || req.AddressB == "" {
		http.Error(w, `{"error":"bad_request","message":"AddressA and AddressB are required"}`, http.StatusBadRequest)
		return
	}

	if req.Price < 0 {
		http.Error(w, `{"error":"bad_request","message":"Price cannot be negative"}`, http.StatusBadRequest)
		return
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.orderSeq++
	orderID := uuid.New().String()
	order := &client.OrderResponse{
		OrderID:       orderID,
		OrderNumber:   2000 + mb.orderSeq,
		Status:        "new",
		CourierStatus: "new",
		ClientID:      "client_uuid",
		AddressA:      req.AddressA,
		AddressB:      req.AddressB,
		Price:         req.Price,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	mb.orders[orderID] = order

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleClientOrderOperations(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/clients/orders/{order_id}/cancel
	if len(parts) >= 5 && parts[4] == "cancel" {
		orderID := parts[3]
		mb.mu.Lock()
		defer mb.mu.Unlock()

		order, exists := mb.orders[orderID]
		if !exists {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}

		// Client can only cancel when order is 'new'
		if order.Status != "new" {
			http.Error(w, `{"error":"forbidden","message":"Client cannot cancel order once cooking or shipping has started"}`, http.StatusForbidden)
			return
		}

		order.Status = "cancelled"
		order.UpdatedAt = time.Now()

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "order cancelled"})
		return
	}

	http.NotFound(w, r)
}

func (mb *MockBackend) handleAdminAssignCourier(w http.ResponseWriter, r *http.Request) {
	var req client.AssignCourierRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	mb.mu.Lock()
	defer mb.mu.Unlock()

	order, exists := mb.orders[req.OrderID]
	if !exists {
		http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
		return
	}

	if order.Status == "cancelled" || order.Status == "complete" {
		http.Error(w, `{"error":"bad_request","message":"Cannot assign courier to completed or cancelled order"}`, http.StatusBadRequest)
		return
	}

	order.CourierID = req.CourierID
	order.CourierName = fmt.Sprintf("Courier %s", req.CourierID)
	order.CourierStatus = "new"
	order.UpdatedAt = time.Now()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleAdminCancelOrder(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	_ = json.NewDecoder(r.Body).Decode(&req)
	orderID := req["order_id"]

	mb.mu.Lock()
	defer mb.mu.Unlock()

	order, exists := mb.orders[orderID]
	if !exists {
		http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
		return
	}

	order.Status = "cancelled"
	order.CourierStatus = "cancelled"
	order.UpdatedAt = time.Now()

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "order cancelled by admin"})
}

func (mb *MockBackend) handleRestOrderStatus(w http.ResponseWriter, r *http.Request) {
	var req client.ChangeRestOrderStatusRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	mb.mu.Lock()
	defer mb.mu.Unlock()

	order, exists := mb.orders[req.OrderID]
	if !exists {
		http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
		return
	}

	if order.Status == "complete" || order.Status == "cancelled" {
		http.Error(w, `{"error":"bad_request","message":"Cannot change status of terminal order"}`, http.StatusBadRequest)
		return
	}

	order.Status = req.Status
	order.UpdatedAt = time.Now()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleCourierTakeOrder(w http.ResponseWriter, r *http.Request) {
	var req client.CourierTakeOrderRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	mb.mu.Lock()
	defer mb.mu.Unlock()

	order, exists := mb.orders[req.OrderID]
	if !exists {
		http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
		return
	}

	order.CourierID = req.CourierID
	order.CourierStatus = "going"
	order.UpdatedAt = time.Now()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleCourierChangeStatus(w http.ResponseWriter, r *http.Request) {
	var req client.CourierChangeStatusRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	mb.mu.Lock()
	defer mb.mu.Unlock()

	order, exists := mb.orders[req.OrderID]
	if !exists {
		http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
		return
	}

	if order.Status == "complete" || order.Status == "cancelled" {
		http.Error(w, `{"error":"bad_request","message":"Cannot change status of terminal order"}`, http.StatusBadRequest)
		return
	}

	order.CourierStatus = req.Status
	if req.Status == "complete" {
		order.Status = "complete"
	} else if req.Status == "shipping" {
		order.Status = "shipping"
	}
	order.UpdatedAt = time.Now()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleClientGetOrder(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	orderID := parts[len(parts)-1]

	mb.mu.RLock()
	defer mb.mu.RUnlock()

	order, exists := mb.orders[orderID]
	if !exists {
		http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleAdminGetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := r.URL.Query().Get("order_id")

	mb.mu.RLock()
	defer mb.mu.RUnlock()

	order, exists := mb.orders[orderID]
	if !exists {
		http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

func (mb *MockBackend) handleAdminListClients(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (mb *MockBackend) handleRestDishes(w http.ResponseWriter, r *http.Request) {
	var req client.CreateDishRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.dishSeq++
	dish := &client.DishResponse{
		ID:     mb.dishSeq,
		Name:   req.Name,
		Price:  req.Price,
		RestID: req.RestID,
	}
	mb.dishes[mb.dishSeq] = dish

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dish)
}
