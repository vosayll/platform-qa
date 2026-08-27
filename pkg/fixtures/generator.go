package fixtures

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"locali-e2e-engine/pkg/client"
)

// presetIdentity is a ready-to-use token provided via engine config (real backend mode).
// Isolation is knowingly sacrificed: no register/login calls are made for this role.
type presetIdentity struct {
	Identifier string
	Token      string
}

// FixtureManager generates and registers isolated test entities with teardown capability
type FixtureManager struct {
	mu         sync.Mutex
	clientAPI  *client.ClientAPI
	restAPI    *client.RestAPI
	courierAPI *client.CourierAPI
	adminAPI   *client.AdminAPI
	cleanups   []func(ctx context.Context) error

	verificationCode string
	onInputRequest   func(runID, phone string)
	presetClient     *presetIdentity
	presetRest       *presetIdentity
	presetCourier    *presetIdentity
}

// InputCtxKey is the context key under which the orchestrator exposes the
// interactive verification-code requester for the duration of a run.
type InputCtxKey struct{}

// InputRequester pauses the run and asks the operator for the one-time code
// delivered to the client's Telegram group.
type InputRequester interface {
	RequestVerificationCode(ctx context.Context, notify func(runID, phone string), phone string) (string, error)
}

func inputRequesterFromCtx(ctx context.Context) InputRequester {
	if v, ok := ctx.Value(InputCtxKey{}).(InputRequester); ok && v != nil {
		return v
	}
	return nil
}

// SetOnInputRequest installs the callback invoked right before the manager
// starts waiting for an operator-entered code (the server wires it to UI events)
func (fm *FixtureManager) SetOnInputRequest(fn func(runID, phone string)) {
	fm.mu.Lock()
	fm.onInputRequest = fn
	fm.mu.Unlock()
}

// NewFixtureManager creates a new fixture manager
func NewFixtureManager(c *client.ClientAPI, r *client.RestAPI, cr *client.CourierAPI, a *client.AdminAPI) *FixtureManager {
	return &FixtureManager{
		clientAPI:        c,
		restAPI:          r,
		courierAPI:       cr,
		adminAPI:         a,
		cleanups:         make([]func(ctx context.Context) error, 0),
		verificationCode: "",
	}
}

// SetVerificationCode injects the backend verification code from the engine config.
// An empty value clears the universal code and enables the manual (operator) flow.
func (fm *FixtureManager) SetVerificationCode(code string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.verificationCode = code
}

// SetPresetToken registers a ready-made token for a role (client|rest|courier).
// Corresponding Create* methods will return this token without register/login calls.
func (fm *FixtureManager) SetPresetToken(role, token string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if token == "" {
		return
	}
	switch strings.ToLower(role) {
	case "client":
		fm.presetClient = &presetIdentity{Identifier: "preset_client", Token: token}
	case "rest", "restaurant":
		fm.presetRest = &presetIdentity{Identifier: "preset_rest", Token: token}
	case "courier":
		fm.presetCourier = &presetIdentity{Identifier: "preset_courier", Token: token}
	}
}

// RegisterCleanup adds a cleanup function to be executed during Teardown
func (fm *FixtureManager) RegisterCleanup(fn func(ctx context.Context) error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.cleanups = append(fm.cleanups, fn)
}

// Teardown executes all cleanup hooks in reverse LIFO order
func (fm *FixtureManager) Teardown(ctx context.Context) []error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	var errs []error
	for i := len(fm.cleanups) - 1; i >= 0; i-- {
		if err := fm.cleanups[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	fm.cleanups = nil
	return errs
}

// CreateUniqueClient registers and logs in a unique client.
// If a preset CLIENT_TOKEN is configured, returns it without register/login (isolation sacrificed).
func (fm *FixtureManager) CreateUniqueClient(ctx context.Context) (phone, token string, err error) {
	fm.mu.Lock()
	preset := fm.presetClient
	code := fm.verificationCode
	requestInput := fm.onInputRequest
	fm.mu.Unlock()

	if preset != nil {
		return fmt.Sprintf("%s_%d", preset.Identifier, rand.Intn(9000000)+1000000), preset.Token, nil
	}

	randNum := rand.Intn(9000000) + 1000000
	phone = fmt.Sprintf("+7999%d", randNum)

	regReq := client.ClientRegisterRequest{
		PhoneNumber: phone,
		Address:     "ул. Тестовая, д. 10, кв. 42",
		CityKey:     "msk",
	}
	if err := fm.clientAPI.Register(ctx, regReq); err != nil {
		return "", "", fmt.Errorf("fixture create client register: %w", err)
	}

	// Manual mode: no universal code is configured (real stand, one-shot codes
	// arrive in Telegram), so pause the run and ask the operator. When a code
	// IS configured or no requester travels in ctx (go test / dsl path),
	// behaviour stays exactly as before.
	if code == "" {
		if ref := inputRequesterFromCtx(ctx); ref != nil {
			entered, ierr := ref.RequestVerificationCode(ctx, requestInput, phone)
			if ierr != nil {
				return "", "", fmt.Errorf("клиент %s: %w", phone, ierr)
			}
			if entered == "" {
				return "", "", fmt.Errorf("клиент %s: получен пустой код верификации", phone)
			}
			code = entered
		} else {
			// Legacy fallback for paths without a run context (go test / dsl):
			// the embedded mock accepts any non-empty code.
			code = "1234"
		}
	}

	loginReq := client.ClientLoginRequest{
		PhoneNumber:      phone,
		VerificationCode: code,
		FirstName:        "TestClient",
		LastName:         "Auto",
	}
	token, err = fm.clientAPI.Login(ctx, loginReq)
	if err != nil {
		return "", "", fmt.Errorf("client login failed (%w). Подсказка: для реального бэкенда задайте VERIFICATION_CODE или предоставьте готовые токены CLIENT_TOKEN/REST_TOKEN/COURIER_TOKEN", err)
	}

	return phone, token, nil
}

// CreateUniqueRestaurant registers and logs in a unique restaurant.
// If a preset REST_TOKEN is configured, returns it without register/login (isolation sacrificed).
func (fm *FixtureManager) CreateUniqueRestaurant(ctx context.Context) (restID, token string, err error) {
	fm.mu.Lock()
	preset := fm.presetRest
	fm.mu.Unlock()

	if preset != nil {
		return fmt.Sprintf("%s_%d", preset.Identifier, rand.Intn(900000)+100000), preset.Token, nil
	}
	randSuffix := rand.Intn(90000) + 10000
	login := fmt.Sprintf("rest_e2e_%d", randSuffix)
	password := "SecretPass123!"
	name := fmt.Sprintf("Restaurant E2E #%d", randSuffix)

	regReq := client.RestRegisterRequest{
		RestName:       name,
		Login:          login,
		Password:       password,
		Email:          fmt.Sprintf("%s@test.locali", login),
		PhoneNumber:    fmt.Sprintf("+7911%d", randSuffix*10),
		City:           "Москва",
		DeliveryMethod: "locali",
	}

	if err := fm.restAPI.Register(ctx, regReq); err != nil {
		return "", "", fmt.Errorf("fixture create restaurant register: %w", err)
	}

	loginReq := client.RestLoginRequest{
		Login:    login,
		Password: password,
	}
	token, err = fm.restAPI.Login(ctx, loginReq)
	if err != nil {
		return "", "", fmt.Errorf("restaurant login failed (%w). Подсказка: для реального бэкенда задайте VERIFICATION_CODE или предоставьте готовые токены CLIENT_TOKEN/REST_TOKEN/COURIER_TOKEN", err)
	}

	return login, token, nil
}

// CreateUniqueCourier registers and logs in a unique courier.
// If a preset COURIER_TOKEN is configured, returns it without register/login (isolation sacrificed).
func (fm *FixtureManager) CreateUniqueCourier(ctx context.Context) (courierID, token string, err error) {
	fm.mu.Lock()
	preset := fm.presetCourier
	fm.mu.Unlock()

	if preset != nil {
		return fmt.Sprintf("%s_%d", preset.Identifier, rand.Intn(900000)+100000), preset.Token, nil
	}
	randSuffix := rand.Intn(90000) + 10000
	phone := fmt.Sprintf("+7922%d", randSuffix*10)
	login := phone
	password := "CourierPass123!"

	regReq := client.CourierRegisterRequest{
		FirstName:    "FastCourier",
		LastName:     fmt.Sprintf("Num%d", randSuffix),
		PhoneNumber:  phone,
		Login:        login,
		Password:     password,
		DeliveryType: "car",
		CityKey:      "msk",
	}

	if err := fm.courierAPI.Register(ctx, regReq); err != nil {
		return "", "", fmt.Errorf("fixture create courier register: %w", err)
	}

	loginReq := client.CourierLoginRequest{
		PhoneNumber: phone,
		Password:    password,
	}
	token, err = fm.courierAPI.Login(ctx, loginReq)
	if err != nil {
		return "", "", fmt.Errorf("courier login failed (%w). Подсказка: для реального бэкенда задайте VERIFICATION_CODE или предоставьте готовые токены CLIENT_TOKEN/REST_TOKEN/COURIER_TOKEN", err)
	}

	return phone, token, nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
