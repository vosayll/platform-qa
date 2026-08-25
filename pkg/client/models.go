package client

import "time"

// Auth Tokens response
type AuthTokenResponse struct {
	Token   string `json:"token"`
	Message string `json:"message"`
}

// Client Register & Login
type ClientRegisterRequest struct {
	PhoneNumber string   `json:"phoneNumber"`
	Address     string   `json:"address,omitempty"`
	CityKey     string   `json:"cityKey,omitempty"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
}

type ClientLoginRequest struct {
	PhoneNumber      string `json:"phoneNumber"`
	VerificationCode string `json:"verificationCode"`
	FirstName        string `json:"firstName,omitempty"`
	LastName         string `json:"lastName,omitempty"`
}

// Restaurant Register & Login
type RestRegisterRequest struct {
	RestName       string `json:"restName"`
	Login          string `json:"login"`
	Password       string `json:"password"`
	Email          string `json:"email,omitempty"`
	PhoneNumber    string `json:"phoneNumber,omitempty"`
	City           string `json:"city,omitempty"`
	DeliveryMethod string `json:"deliveryMethod,omitempty"`
}

type RestLoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// Courier Register & Login
type CourierRegisterRequest struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	PhoneNumber  string `json:"phoneNumber"`
	Login        string `json:"login"`
	Password     string `json:"password"`
	DeliveryType string `json:"deliveryType,omitempty"`
	CityKey      string `json:"cityKey,omitempty"`
}

type CourierLoginRequest struct {
	PhoneNumber string `json:"phoneNumber"`
	Password    string `json:"password"`
}

// Admin Login
type AdminLoginRequest struct {
	Login       string `json:"login,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
	Password    string `json:"password"`
}

// Order Creation Requests
type CreateRestaurantOrderRequest struct {
	RestID          string        `json:"restId"`
	Items           []OrderItem   `json:"items,omitempty"`
	DeliveryAddress string        `json:"deliveryAddress"`
	DeliveryPrice   float64       `json:"deliveryPrice,omitempty"`
	PaymentType     string        `json:"paymentType"`
	Comment         string        `json:"comment,omitempty"`
	DishIdArray     []int         `json:"dishIdArray,omitempty"`
}

type OrderItem struct {
	DishID   int `json:"dishId"`
	Quantity int `json:"quantity"`
}

type CreateIndependentOrderRequest struct {
	AddressA    string  `json:"addressA"`
	AddressB    string  `json:"addressB"`
	Comment     string  `json:"comment,omitempty"`
	PaymentType string  `json:"paymentType"`
	Price       float64 `json:"price,omitempty"`
}

// Order Status & Assignment Requests
type AssignCourierRequest struct {
	OrderID   string `json:"order_id"`
	CourierID string `json:"courier_id"`
}

type ChangeRestOrderStatusRequest struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"` // "cooking", "cooked", "shipping", "complete", "cancelled"
}

type CourierTakeOrderRequest struct {
	OrderID   string `json:"order_id"`
	CourierID string `json:"courier_id"`
}

type CourierChangeStatusRequest struct {
	OrderID       string `json:"order_id"`
	Status        string `json:"status"` // "going", "shipping", "complete"
	IsIndependent bool   `json:"isIndependent,omitempty"`
}

// Domain Order Model Response
type OrderResponse struct {
	OrderID          string    `json:"order_id"`
	OrderNumber      int       `json:"orderNumber"`
	Status           string    `json:"status"`
	CourierStatus    string    `json:"courierStatus"`
	ClientID         string    `json:"clientId"`
	RestID           string    `json:"restId"`
	CourierID        string    `json:"courier_id"`
	CourierName      string    `json:"courierName"`
	Price            float64   `json:"price"`
	DeliveryAddress  string    `json:"deliveryAddress"`
	AddressA         string    `json:"addressA,omitempty"`
	AddressB         string    `json:"addressB,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Dish creation
type CreateDishRequest struct {
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description,omitempty"`
	RestID      string  `json:"rest_id,omitempty"`
}

type DishResponse struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Price  float64 `json:"price"`
	RestID string  `json:"rest_id"`
}
