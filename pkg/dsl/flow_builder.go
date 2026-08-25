package dsl

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"locali-e2e-engine/config"
	"locali-e2e-engine/pkg/client"
	"locali-e2e-engine/pkg/events"
	"locali-e2e-engine/pkg/fixtures"
	"locali-e2e-engine/pkg/statemachine"
)

// Engine is the central test orchestrator
type Engine struct {
	Config       *config.Config
	HTTPClient   *client.HTTPClient
	SessionMgr   *client.SessionManager
	ClientAPI    *client.ClientAPI
	RestAPI      *client.RestAPI
	CourierAPI   *client.CourierAPI
	AdminAPI     *client.AdminAPI
	StateMachine *statemachine.OrderStateMachine
	Events       *events.NatsEventListener
	Fixtures     *fixtures.FixtureManager
}

// NewEngine initializes the full testing engine
func NewEngine(cfg *config.Config) (*Engine, error) {
	if cfg == nil {
		cfg = config.LoadFromEnv()
	}

	httpClient := client.NewHTTPClient(cfg.BaseURL, cfg.RequestTimeout)
	sessionMgr := client.NewSessionManager(httpClient)

	clientAPI := client.NewClientAPI(sessionMgr)
	restAPI := client.NewRestAPI(sessionMgr)
	courierAPI := client.NewCourierAPI(sessionMgr)
	adminAPI := client.NewAdminAPI(sessionMgr)

	eventListener, err := events.NewNatsEventListener(cfg.NatsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to init event listener: %w", err)
	}

	sm := statemachine.NewOrderStateMachine()
	fixMgr := fixtures.NewFixtureManager(clientAPI, restAPI, courierAPI, adminAPI)
	fixMgr.SetVerificationCode(cfg.VerificationCode)

	// Preset tokens from env: register them as active sessions and let fixtures
	// reuse them without register/login calls (isolation is knowingly sacrificed
	// when running against a real backend with pre-provisioned identities).
	if cfg.ClientToken != "" {
		sessionMgr.AddToken("client", "Preset (env)", "", cfg.ClientToken, true)
		fixMgr.SetPresetToken("client", cfg.ClientToken)
	}
	if cfg.RestToken != "" {
		sessionMgr.AddToken("rest", "Preset (env)", "", cfg.RestToken, true)
		fixMgr.SetPresetToken("rest", cfg.RestToken)
	}
	if cfg.CourierToken != "" {
		sessionMgr.AddToken("courier", "Preset (env)", "", cfg.CourierToken, true)
		fixMgr.SetPresetToken("courier", cfg.CourierToken)
	}

	return &Engine{
		Config:       cfg,
		HTTPClient:   httpClient,
		SessionMgr:   sessionMgr,
		ClientAPI:    clientAPI,
		RestAPI:      restAPI,
		CourierAPI:   courierAPI,
		AdminAPI:     adminAPI,
		StateMachine: sm,
		Events:       eventListener,
		Fixtures:     fixMgr,
	}, nil
}

// ScenarioBuilder provides fluent BDD style test step execution
type ScenarioBuilder struct {
	t       *testing.T
	name    string
	engine  *Engine
	ctx     *TestContext
	started time.Time
}

// TestContext holds state between scenario steps
type TestContext struct {
	t            *testing.T
	Engine       *Engine
	GoCtx        context.Context
	OrderType    statemachine.OrderType
	CurrentOrder *client.OrderResponse
	LastStatus   statemachine.OrderStatus
	ClientPhone  string
	RestID       string
	CourierID    string
	AdminToken   string
	Data         map[string]interface{}
}

// Scenario starts a new declarative test scenario
func (e *Engine) Scenario(t *testing.T, name string) *ScenarioBuilder {
	ctx := &TestContext{
		t:         t,
		Engine:    e,
		GoCtx:     context.Background(),
		Data:      make(map[string]interface{}),
		OrderType: statemachine.OrderTypeRestaurant,
	}

	sb := &ScenarioBuilder{
		t:       t,
		name:    name,
		engine:  e,
		ctx:     ctx,
		started: time.Now(),
	}

	t.Logf("\n======================================================\n[SCENARIO START] %s\n======================================================", name)
	return sb
}

// Given executes background or initial setup
func (sb *ScenarioBuilder) Given(description string, step func(ctx *TestContext)) *ScenarioBuilder {
	sb.t.Logf("  [GIVEN] %s", description)
	step(sb.ctx)
	return sb
}

// When executes an action step
func (sb *ScenarioBuilder) When(description string, step func(ctx *TestContext)) *ScenarioBuilder {
	sb.t.Logf("  [WHEN]  %s", description)
	step(sb.ctx)
	return sb
}

// Then executes assertion and validation step
func (sb *ScenarioBuilder) Then(description string, step func(ctx *TestContext)) *ScenarioBuilder {
	sb.t.Logf("  [THEN]  %s", description)
	step(sb.ctx)
	return sb
}

// And chains an additional check or step
func (sb *ScenarioBuilder) And(description string, step func(ctx *TestContext)) *ScenarioBuilder {
	sb.t.Logf("  [AND]   %s", description)
	step(sb.ctx)
	return sb
}

// SetupAllRoles generates/authenticates unique Client, Restaurant, Courier and Admin identities
func (c *TestContext) SetupAllRoles() {
	var err error

	// Admin Auth
	if c.Engine.Config.AdminToken != "" {
		c.Engine.SessionMgr.SetAdminSession("admin_root", c.Engine.Config.AdminToken, c.Engine.Config.AdminLogin)
		c.AdminToken = c.Engine.Config.AdminToken
	} else {
		token, err := c.Engine.SessionMgr.LoginAdmin(c.GoCtx, c.Engine.Config.AdminLogin, c.Engine.Config.AdminPassword)
		require.NoError(c.t, err, "Admin login must succeed")
		c.AdminToken = token
	}

	// Client Auth
	c.ClientPhone, _, err = c.Engine.Fixtures.CreateUniqueClient(c.GoCtx)
	require.NoError(c.t, err, "Client fixture creation must succeed")

	// Restaurant Auth
	c.RestID, _, err = c.Engine.Fixtures.CreateUniqueRestaurant(c.GoCtx)
	require.NoError(c.t, err, "Restaurant fixture creation must succeed")

	// Courier Auth
	c.CourierID, _, err = c.Engine.Fixtures.CreateUniqueCourier(c.GoCtx)
	require.NoError(c.t, err, "Courier fixture creation must succeed")

	// Register teardown at test conclusion
	c.t.Cleanup(func() {
		errs := c.Engine.Fixtures.Teardown(context.Background())
		if len(errs) > 0 {
			c.t.Logf("[TEARDOWN WARNING] %d errors during cleanup: %v", len(errs), errs)
		}
	})
}

// AssertOrderStatus checks order status with State Machine transition validation
func (c *TestContext) AssertOrderStatus(expected statemachine.OrderStatus, actor statemachine.Role) {
	require.NotNil(c.t, c.CurrentOrder, "Current order must not be nil")

	// State machine check
	if c.LastStatus != "" {
		err := c.Engine.StateMachine.ValidateTransition(c.OrderType, c.LastStatus, expected, actor)
		require.NoError(c.t, err, "State Machine transition rule violation")
	}

	c.Engine.StateMachine.RecordTransition(c.CurrentOrder.OrderID, expected)
	c.LastStatus = expected

	c.t.Logf("    ✓ State validated: %s (Actor: %s, OrderID: %s)", expected, actor, c.CurrentOrder.OrderID)
}
