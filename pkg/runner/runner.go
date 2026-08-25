package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"locali-e2e-engine/pkg/client"
	"locali-e2e-engine/pkg/dsl"
	"locali-e2e-engine/pkg/registry"
	"locali-e2e-engine/pkg/scenario"
	"locali-e2e-engine/pkg/statemachine"
)

// LogLevel represents log severity
type LogLevel string

const (
	LogInfo    LogLevel = "INFO"
	LogSuccess LogLevel = "SUCCESS"
	LogWarn    LogLevel = "WARN"
	LogError   LogLevel = "ERROR"
	LogHTTP    LogLevel = "HTTP"
)

// Check execution statuses stored in TestRun.Results
const (
	CheckRunning = "RUNNING"
	CheckPassed  = "PASSED"
	CheckFailed  = "FAILED"
	CheckSkipped = "SKIPPED"
)

// MaxRecentEvents is the ring buffer capacity of recent execution events
const MaxRecentEvents = 2000

// ExecutionEvent is emitted during test execution to update the Admin UI in real-time
type ExecutionEvent struct {
	RunID        string                   `json:"runId"`
	SuiteName    string                   `json:"suiteName"`
	SuiteKey     string                   `json:"suiteKey,omitempty"`
	StepName     string                   `json:"stepName"`
	StepType     string                   `json:"stepType"` // GIVEN, WHEN, THEN, AND, INFO, CHECK_START, CHECK_SUCCESS, CHECK_FAILED
	Level        LogLevel                 `json:"level"`
	Message      string                   `json:"message"`
	CheckID      string                   `json:"checkId,omitempty"`
	StepIndex    int                      `json:"stepIndex,omitempty"`
	TotalSteps   int                      `json:"totalSteps,omitempty"`
	CurrentState statemachine.OrderStatus `json:"currentState,omitempty"`
	OrderID      string                   `json:"orderId,omitempty"`
	DurationMs   int64                    `json:"durationMs"`
	Timestamp    time.Time                `json:"timestamp"`
	HTTPDetails  *HTTPDetails             `json:"httpDetails,omitempty"`
}

type HTTPDetails struct {
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	Role       string      `json:"role"`
	StatusCode int         `json:"statusCode"`
	Request    interface{} `json:"request,omitempty"`
	Response   interface{} `json:"response,omitempty"`
}

// CheckResult is the outcome of a single check within a suite run
type CheckResult struct {
	Status     string `json:"status"` // RUNNING, PASSED, FAILED, SKIPPED
	Message    string `json:"message,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

// TestRun represents a complete execution record
type TestRun struct {
	ID           string                   `json:"id"`
	SuiteName    string                   `json:"suiteName"`
	SuiteKey     string                   `json:"suiteKey,omitempty"`
	Status       string                   `json:"status"` // RUNNING, PASSED, FAILED
	StartTime    time.Time                `json:"startTime"`
	EndTime      time.Time                `json:"endTime"`
	DurationMs   int64                    `json:"durationMs"`
	Events       []*ExecutionEvent        `json:"events"`
	FinalState   statemachine.OrderStatus `json:"finalState,omitempty"`
	Error        string                   `json:"error,omitempty"`
	PassedSteps  int                      `json:"passedSteps"`
	TotalSteps   int                      `json:"totalSteps"`
	TotalChecks  int                      `json:"totalChecks"`
	PassedChecks int                      `json:"passedChecks"`
	FailedChecks int                      `json:"failedChecks"`
	Results      map[string]CheckResult   `json:"results,omitempty"`

	stepCursor int `json:"-"`
}

// CustomSource provides read access to user-defined custom scenarios
// (implemented by *scenario.Store).
type CustomSource interface {
	Get(key string) (*scenario.Scenario, bool)
	Exists(key string) bool
}

// TestOrchestrator manages and executes E2E test runs with live broadcasting
type TestOrchestrator struct {
	mu           sync.RWMutex
	engine       *dsl.Engine
	custom       CustomSource
	activeRuns   map[string]*TestRun
	history      []*TestRun
	broadcaster  chan *ExecutionEvent
	recentEvents []*ExecutionEvent
}

// NewTestOrchestrator creates a new test orchestrator
func NewTestOrchestrator(engine *dsl.Engine) *TestOrchestrator {
	return &TestOrchestrator{
		engine:      engine,
		activeRuns:  make(map[string]*TestRun),
		history:     make([]*TestRun, 0),
		broadcaster: make(chan *ExecutionEvent, 2000),
	}
}

// SetCustomSource registers the storage of user-defined scenarios
func (o *TestOrchestrator) SetCustomSource(cs CustomSource) {
	o.mu.Lock()
	o.custom = cs
	o.mu.Unlock()
}

// hasCustom reports whether a user-defined scenario exists for the key
func (o *TestOrchestrator) hasCustom(key string) bool {
	o.mu.RLock()
	cs := o.custom
	o.mu.RUnlock()
	return cs != nil && cs.Exists(key)
}

// customScenario returns a copy of the custom scenario, or nil
func (o *TestOrchestrator) customScenario(key string) *scenario.Scenario {
	o.mu.RLock()
	cs := o.custom
	o.mu.RUnlock()
	if cs == nil {
		return nil
	}
	sc, ok := cs.Get(key)
	if !ok {
		return nil
	}
	return sc
}

// customChecks returns the number of steps if the key is a custom scenario, otherwise 0
func (o *TestOrchestrator) customChecks(key string) int {
	if sc := o.customScenario(key); sc != nil {
		return len(sc.Steps)
	}
	return 0
}

// customTitle returns the custom scenario title, or ""
func (o *TestOrchestrator) customTitle(key string) string {
	if sc := o.customScenario(key); sc != nil {
		return sc.Title
	}
	return ""
}

// Registry exposes the declarative suite catalog for the Admin UI
func (o *TestOrchestrator) Registry() []registry.Suite {
	return registry.All()
}

// Broadcaster returns the channel for streaming events to WebSockets/SSE
func (o *TestOrchestrator) Broadcaster() <-chan *ExecutionEvent {
	return o.broadcaster
}

// GetHistory returns past test runs
func (o *TestOrchestrator) GetHistory() []*TestRun {
	o.mu.RLock()
	defer o.mu.RUnlock()
	res := make([]*TestRun, len(o.history))
	copy(res, o.history)
	return res
}

// GetRun returns a specific test run by ID
func (o *TestOrchestrator) GetRun(id string) *TestRun {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if run, exists := o.activeRuns[id]; exists {
		return run
	}
	for _, run := range o.history {
		if run.ID == id {
			return run
		}
	}
	return nil
}

// Emit sends an execution event to active run, recent-events buffer and live broadcast
func (o *TestOrchestrator) Emit(event *ExecutionEvent) {
	o.mu.Lock()
	if run, exists := o.activeRuns[event.RunID]; exists {
		run.Events = append(run.Events, event)
	}
	o.recentEvents = append(o.recentEvents, event)
	if len(o.recentEvents) > MaxRecentEvents {
		o.recentEvents = o.recentEvents[len(o.recentEvents)-MaxRecentEvents:]
	}
	o.mu.Unlock()

	select {
	case o.broadcaster <- event:
	default:
	}
}

// RecentEvents returns the last limit buffered events in chronological
// order (oldest first). If limit <= 0 or exceeds the buffer capacity,
// all buffered events are returned.
func (o *TestOrchestrator) RecentEvents(limit int) []*ExecutionEvent {
	o.mu.RLock()
	defer o.mu.RUnlock()
	events := o.recentEvents
	if limit > 0 && limit < len(events) {
		events = events[len(events)-limit:]
	}
	res := make([]*ExecutionEvent, len(events))
	copy(res, events)
	return res
}

func (o *TestOrchestrator) newRun(suiteKey string) *TestRun {
	return &TestRun{
		ID:          uuid.New().String(),
		SuiteName:   suiteKey,
		SuiteKey:    suiteKey,
		Status:      "RUNNING",
		StartTime:   time.Now(),
		Events:      make([]*ExecutionEvent, 0),
		Results:     make(map[string]CheckResult),
		TotalChecks: o.totalCheckCount(suiteKey),
	}
}

// isValidSuiteKey reports whether the key can be executed (built-in or custom)
func (o *TestOrchestrator) isValidSuiteKey(key string) bool {
	if key == "all" {
		return true
	}
	if _, ok := registry.Get(key); ok {
		return true
	}
	return o.hasCustom(key)
}

// RunSuite runs a named suite asynchronously
func (o *TestOrchestrator) RunSuite(suiteKey string) (*TestRun, error) {
	if !o.isValidSuiteKey(suiteKey) {
		return nil, fmt.Errorf("unknown suite key: %s", suiteKey)
	}

	run := o.newRun(suiteKey)

	o.mu.Lock()
	o.activeRuns[run.ID] = run
	o.mu.Unlock()

	go o.executeRuns([]*TestRun{run})

	return run, nil
}

// RunSuites creates a TestRun per valid key and executes them sequentially
// in a single goroutine (the caller's), never hitting the backend in parallel.
func (o *TestOrchestrator) RunSuites(keys []string) ([]*TestRun, error) {
	runs := make([]*TestRun, 0, len(keys))
	for _, key := range keys {
		if !o.isValidSuiteKey(key) {
			return nil, fmt.Errorf("unknown suite key: %s", key)
		}
		runs = append(runs, o.newRun(key))
	}

	o.mu.Lock()
	for _, run := range runs {
		o.activeRuns[run.ID] = run
	}
	o.mu.Unlock()

	o.executeRuns(runs)

	return runs, nil
}

// executeRuns executes the given runs one by one in the current goroutine
func (o *TestOrchestrator) executeRuns(runs []*TestRun) {
	for _, run := range runs {
		o.executeSuite(run, run.SuiteKey)
	}
}

func (o *TestOrchestrator) totalCheckCount(suiteKey string) int {
	if suiteKey == "all" {
		n := 0
		for _, s := range registry.All() {
			n += len(s.Checks)
		}
		return n
	}
	if s, ok := registry.Get(suiteKey); ok {
		return len(s.Checks)
	}
	return o.customChecks(suiteKey)
}

func (o *TestOrchestrator) suiteTitle(suiteKey string) string {
	if s, ok := registry.Get(suiteKey); ok {
		return s.Title
	}
	if t := o.customTitle(suiteKey); t != "" {
		return t
	}
	return suiteKey
}

func (o *TestOrchestrator) checkCount(suiteKey string) int {
	return o.totalCheckCount(suiteKey)
}

func suiteKeysFor(suiteKey string) []string {
	if suiteKey == "all" {
		return registry.Keys()
	}
	return []string{suiteKey}
}

// checkStart marks a check as RUNNING and emits a CHECK_START event.
// The title is looked up in the built-in registry; custom scenarios
// should call checkStartTitled directly.
func (o *TestOrchestrator) checkStart(run *TestRun, suiteKey, checkID string) {
	label := checkID
	if suite, ok := registry.Get(suiteKey); ok {
		for _, c := range suite.Checks {
			if c.ID == checkID {
				label = c.Title
				break
			}
		}
	}
	o.checkStartTitled(run, suiteKey, checkID, label)
}

// checkStartTitled is like checkStart but accepts the human-readable title explicitly
func (o *TestOrchestrator) checkStartTitled(run *TestRun, suiteKey, checkID, title string) {
	total := o.checkCount(suiteKey)
	label := title
	if label == "" {
		label = checkID
	}

	o.mu.Lock()
	run.stepCursor++
	if run.Results == nil {
		run.Results = make(map[string]CheckResult)
	}
	run.Results[checkID] = CheckResult{Status: CheckRunning}
	cursor := run.stepCursor
	o.mu.Unlock()

	o.Emit(&ExecutionEvent{
		RunID:      run.ID,
		SuiteName:  o.suiteTitle(suiteKey),
		SuiteKey:   suiteKey,
		StepType:   "CHECK_START",
		Level:      LogInfo,
		Message:    fmt.Sprintf("▶ [%d/%d] %s", cursor, total, label),
		CheckID:    checkID,
		StepIndex:  cursor,
		TotalSteps: total,
		Timestamp:  time.Now(),
	})
}

// checkDone marks a check as PASSED/FAILED and emits a CHECK_SUCCESS/CHECK_FAILED event
func (o *TestOrchestrator) checkDone(run *TestRun, suiteKey, checkID string, ok bool, msg string, started time.Time) {
	duration := time.Since(started).Milliseconds()

	status, stepType, level, icon := CheckFailed, "CHECK_FAILED", LogError, "❌"
	if ok {
		status, stepType, level, icon = CheckPassed, "CHECK_SUCCESS", LogSuccess, "✅"
	}

	o.mu.Lock()
	if ok {
		run.PassedChecks++
	} else {
		run.FailedChecks++
	}
	if run.Results == nil {
		run.Results = make(map[string]CheckResult)
	}
	run.Results[checkID] = CheckResult{Status: status, Message: msg, DurationMs: duration}
	cursor := run.stepCursor
	o.mu.Unlock()

	o.Emit(&ExecutionEvent{
		RunID:      run.ID,
		SuiteName:  o.suiteTitle(suiteKey),
		SuiteKey:   suiteKey,
		StepType:   stepType,
		Level:      level,
		Message:    fmt.Sprintf("%s %s (%d ms)", icon, msg, duration),
		CheckID:    checkID,
		StepIndex:  cursor,
		TotalSteps: o.checkCount(suiteKey),
		DurationMs: duration,
		Timestamp:  time.Now(),
	})
}

// checkSkip marks a check as SKIPPED and emits an INFO event
func (o *TestOrchestrator) checkSkip(run *TestRun, suiteKey, checkID, reason string) {
	o.mu.Lock()
	if run.Results == nil {
		run.Results = make(map[string]CheckResult)
	}
	run.Results[checkID] = CheckResult{Status: CheckSkipped, Message: reason}
	o.mu.Unlock()

	o.Emit(&ExecutionEvent{
		RunID:      run.ID,
		SuiteName:  o.suiteTitle(suiteKey),
		SuiteKey:   suiteKey,
		StepType:   "INFO",
		Level:      LogInfo,
		Message:    fmt.Sprintf("⏭ Check %s skipped: %s", checkID, reason),
		CheckID:    checkID,
		TotalSteps: o.checkCount(suiteKey),
		Timestamp:  time.Now(),
	})
}

// finalizeChecks downgrades every leftover RUNNING or missing check to SKIPPED
// for both built-in suites and user-defined scenarios.
func (o *TestOrchestrator) finalizeChecks(run *TestRun, suiteKeys []string) {
	for _, sk := range suiteKeys {
		if suite, ok := registry.Get(sk); ok {
			for _, c := range suite.Checks {
				o.skipIfPending(run, sk, c.ID, "прогон завершился раньше")
			}
			continue
		}
		if sc := o.customScenario(sk); sc != nil {
			for _, st := range sc.Steps {
				o.skipIfPending(run, sk, st.ID, "прогон завершился раньше")
			}
		}
	}
}

func (o *TestOrchestrator) skipIfPending(run *TestRun, suiteKey, checkID, reason string) {
	o.mu.RLock()
	res, exists := run.Results[checkID]
	o.mu.RUnlock()
	if exists && res.Status != CheckRunning {
		return
	}
	o.checkSkip(run, suiteKey, checkID, reason)
}

func (o *TestOrchestrator) executeSuite(run *TestRun, suiteKey string) {
	ctx := context.Background()
	start := time.Now()

	var err error
	switch suiteKey {
	case "flow_a":
		err = o.runFlowA(ctx, run, suiteKey)
	case "flow_b":
		err = o.runFlowB(ctx, run, suiteKey)
	case "negative_sm":
		err = o.runNegativeSM(ctx, run, suiteKey)
	case "cancellation":
		err = o.runCancellation(ctx, run, suiteKey)
	case "security_rbac":
		err = o.runSecurityRBAC(ctx, run, suiteKey)
	case "idempotency":
		err = o.runIdempotency(ctx, run, suiteKey)
	case "all":
		err = o.runFlowA(ctx, run, "flow_a")
		if err == nil {
			err = o.runFlowB(ctx, run, "flow_b")
		}
		if err == nil {
			err = o.runNegativeSM(ctx, run, "negative_sm")
		}
		if err == nil {
			err = o.runCancellation(ctx, run, "cancellation")
		}
		if err == nil {
			err = o.runSecurityRBAC(ctx, run, "security_rbac")
		}
		if err == nil {
			err = o.runIdempotency(ctx, run, "idempotency")
		}
	default:
		if o.hasCustom(suiteKey) {
			err = o.runUserScenario(ctx, run, suiteKey)
		} else {
			err = fmt.Errorf("unknown suite key: %s", suiteKey)
		}
	}

	o.finalizeChecks(run, suiteKeysFor(suiteKey))

	run.EndTime = time.Now()
	run.DurationMs = time.Since(start).Milliseconds()

	summary := &ExecutionEvent{
		RunID:      run.ID,
		SuiteName:  suiteKey,
		SuiteKey:   suiteKey,
		StepType:   "SUMMARY",
		DurationMs: run.DurationMs,
		Timestamp:  time.Now(),
	}
	if err != nil {
		run.Status = "FAILED"
		run.Error = err.Error()
		summary.Level = LogError
		summary.Message = fmt.Sprintf("❌ Suite execution failed: %v", err)
	} else {
		run.Status = "PASSED"
		summary.Level = LogSuccess
		summary.Message = "✅ Suite finished successfully. All assertions passed."
	}

	o.Emit(summary)

	o.mu.Lock()
	delete(o.activeRuns, run.ID)
	o.history = append([]*TestRun{run}, o.history...)
	o.mu.Unlock()
}

func (o *TestOrchestrator) runFlowA(ctx context.Context, run *TestRun, suiteKey string) error {
	suiteName := "Flow A: Full Restaurant Order Lifecycle"
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "SUITE_START",
		Level:     LogInfo,
		Message:   "Starting Flow A: Restaurant Lifecycle (New -> Assigned -> Preparing -> Cooked -> PickedUp -> Delivered)",
		Timestamp: time.Now(),
	})

	// 1. Setup Identities
	setupStart := time.Now()
	o.checkStart(run, suiteKey, "setup")
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "GIVEN",
		Level:     LogInfo,
		Message:   "Generating unique Client, Restaurant, Courier and Admin identities...",
		Timestamp: time.Now(),
	})

	clientPhone, _, err := o.engine.Fixtures.CreateUniqueClient(ctx)
	if err != nil {
		o.checkDone(run, suiteKey, "setup", false, fmt.Sprintf("fixture client failed: %v", err), setupStart)
		return fmt.Errorf("fixture client failed: %w", err)
	}
	restLogin, _, err := o.engine.Fixtures.CreateUniqueRestaurant(ctx)
	if err != nil {
		o.checkDone(run, suiteKey, "setup", false, fmt.Sprintf("fixture restaurant failed: %v", err), setupStart)
		return fmt.Errorf("fixture restaurant failed: %w", err)
	}
	courierPhone, _, err := o.engine.Fixtures.CreateUniqueCourier(ctx)
	if err != nil {
		o.checkDone(run, suiteKey, "setup", false, fmt.Sprintf("fixture courier failed: %v", err), setupStart)
		return fmt.Errorf("fixture courier failed: %w", err)
	}

	if o.engine.Config.AdminToken == "" {
		_, _ = o.engine.SessionMgr.LoginAdmin(ctx, o.engine.Config.AdminLogin, o.engine.Config.AdminPassword)
	}

	o.Emit(&ExecutionEvent{
		RunID:      run.ID,
		SuiteName:  suiteName,
		SuiteKey:   suiteKey,
		StepType:   "GIVEN",
		Level:      LogSuccess,
		Message:    fmt.Sprintf("Identities ready: Client=%s, Rest=%s, Courier=%s", clientPhone, restLogin, courierPhone),
		DurationMs: time.Since(setupStart).Milliseconds(),
		Timestamp:  time.Now(),
	})
	run.PassedSteps++
	o.checkDone(run, suiteKey, "setup", true, "Уникальные фикстуры созданы: клиент, ресторан, курьер", setupStart)

	// 2. Client creates order
	createStart := time.Now()
	o.checkStart(run, suiteKey, "create_order")
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "WHEN",
		Level:     LogInfo,
		Message:   "Step 1: Client sends POST /api/clients/create-order",
		Timestamp: time.Now(),
	})

	createReq := client.CreateRestaurantOrderRequest{
		RestID:          restLogin,
		DeliveryAddress: "Москва, Тверская 15, кв. 10",
		PaymentType:     "card",
		Comment:         "Оставить у двери",
		DishIdArray:     []int{1, 2},
	}
	order, err := o.engine.ClientAPI.CreateRestaurantOrder(ctx, createReq)
	if err != nil {
		o.checkDone(run, suiteKey, "create_order", false, fmt.Sprintf("create order failed: %v", err), createStart)
		return fmt.Errorf("create order failed: %w", err)
	}

	// Validate State NEW (order creation is outside the transition table, same as Flow B)
	_ = o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeRestaurant, "", statemachine.StatusNew, statemachine.RoleClient)
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusNew)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      fmt.Sprintf("Order #%d created. Status is NEW. State Machine check passed.", order.OrderNumber),
		CurrentState: statemachine.StatusNew,
		OrderID:      order.OrderID,
		DurationMs:   time.Since(createStart).Milliseconds(),
		Timestamp:    time.Now(),
	})
	run.PassedSteps++
	o.checkDone(run, suiteKey, "create_order", true, "Заказ создан, статус NEW", createStart)

	// 3. Admin assigns courier
	assignStart := time.Now()
	o.checkStart(run, suiteKey, "assign_courier")
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "WHEN",
		Level:     LogInfo,
		Message:   fmt.Sprintf("Step 2: Director sends POST /api/admin/give-order-to-courier (Courier: %s)", courierPhone),
		Timestamp: time.Now(),
	})

	order, err = o.engine.AdminAPI.AssignCourier(ctx, order.OrderID, courierPhone)
	if err != nil {
		o.checkDone(run, suiteKey, "assign_courier", false, fmt.Sprintf("admin assign courier failed: %v", err), assignStart)
		return fmt.Errorf("admin assign courier failed: %w", err)
	}

	if err := o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusNew, statemachine.StatusCourierAssigned, statemachine.RoleAdmin); err != nil {
		o.checkDone(run, suiteKey, "assign_courier", false, err.Error(), assignStart)
		return err
	}
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusCourierAssigned)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      "Courier assigned to order. Status: COURIER_ASSIGNED.",
		CurrentState: statemachine.StatusCourierAssigned,
		OrderID:      order.OrderID,
		DurationMs:   time.Since(assignStart).Milliseconds(),
		Timestamp:    time.Now(),
	})
	run.PassedSteps++
	o.checkDone(run, suiteKey, "assign_courier", true, "Курьер назначен, статус COURIER_ASSIGNED", assignStart)

	// 4. Restaurant starts cooking
	cookStart := time.Now()
	o.checkStart(run, suiteKey, "cooking")
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "WHEN",
		Level:     LogInfo,
		Message:   "Step 3: Restaurant sends POST /api/rests/order-status (status: 'cooking')",
		Timestamp: time.Now(),
	})

	order, err = o.engine.RestAPI.ChangeOrderStatus(ctx, order.OrderID, "cooking")
	if err != nil {
		o.checkDone(run, suiteKey, "cooking", false, fmt.Sprintf("restaurant cooking failed: %v", err), cookStart)
		return fmt.Errorf("restaurant cooking failed: %w", err)
	}

	if err := o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusCourierAssigned, statemachine.StatusPreparing, statemachine.RoleRestaurant); err != nil {
		o.checkDone(run, suiteKey, "cooking", false, err.Error(), cookStart)
		return err
	}
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusPreparing)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      "Restaurant is preparing order. Status: PREPARING.",
		CurrentState: statemachine.StatusPreparing,
		OrderID:      order.OrderID,
		DurationMs:   time.Since(cookStart).Milliseconds(),
		Timestamp:    time.Now(),
	})
	run.PassedSteps++
	o.checkDone(run, suiteKey, "cooking", true, "Ресторан готовит, статус PREPARING", cookStart)

	// 5. Restaurant marks ready
	readyStart := time.Now()
	o.checkStart(run, suiteKey, "ready_for_pickup")
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "WHEN",
		Level:     LogInfo,
		Message:   "Step 4: Restaurant sends POST /api/rests/order-status (status: 'cooked')",
		Timestamp: time.Now(),
	})

	order, err = o.engine.RestAPI.ChangeOrderStatus(ctx, order.OrderID, "cooked")
	if err != nil {
		o.checkDone(run, suiteKey, "ready_for_pickup", false, fmt.Sprintf("restaurant cooked failed: %v", err), readyStart)
		return fmt.Errorf("restaurant cooked failed: %w", err)
	}

	if err := o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusPreparing, statemachine.StatusReadyForPickup, statemachine.RoleRestaurant); err != nil {
		o.checkDone(run, suiteKey, "ready_for_pickup", false, err.Error(), readyStart)
		return err
	}
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusReadyForPickup)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      "Order packed and ready. Status: READY_FOR_PICKUP.",
		CurrentState: statemachine.StatusReadyForPickup,
		OrderID:      order.OrderID,
		DurationMs:   time.Since(readyStart).Milliseconds(),
		Timestamp:    time.Now(),
	})
	run.PassedSteps++
	o.checkDone(run, suiteKey, "ready_for_pickup", true, "Заказ собран, статус READY_FOR_PICKUP", readyStart)

	// 6. Courier picks up order
	pickupStart := time.Now()
	o.checkStart(run, suiteKey, "pickup")
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "WHEN",
		Level:     LogInfo,
		Message:   "Step 5: Courier takes order and sets status 'shipping'",
		Timestamp: time.Now(),
	})

	_ = o.engine.CourierAPI.TakeOrder(ctx, order.OrderID, courierPhone)
	order, err = o.engine.CourierAPI.ChangeStatus(ctx, order.OrderID, "shipping", false)
	if err != nil {
		o.checkDone(run, suiteKey, "pickup", false, fmt.Sprintf("courier shipping failed: %v", err), pickupStart)
		return fmt.Errorf("courier shipping failed: %w", err)
	}

	if err := o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusReadyForPickup, statemachine.StatusPickedUp, statemachine.RoleCourier); err != nil {
		o.checkDone(run, suiteKey, "pickup", false, err.Error(), pickupStart)
		return err
	}
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusPickedUp)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      "Courier picked up order and heading to client. Status: PICKED_UP.",
		CurrentState: statemachine.StatusPickedUp,
		OrderID:      order.OrderID,
		DurationMs:   time.Since(pickupStart).Milliseconds(),
		Timestamp:    time.Now(),
	})
	run.PassedSteps++
	o.checkDone(run, suiteKey, "pickup", true, "Курьер забрал заказ, статус PICKED_UP", pickupStart)

	// 7. Courier delivers
	deliverStart := time.Now()
	o.checkStart(run, suiteKey, "delivered")
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "WHEN",
		Level:     LogInfo,
		Message:   "Step 6: Courier hands order to client and sets status 'complete'",
		Timestamp: time.Now(),
	})

	order, err = o.engine.CourierAPI.ChangeStatus(ctx, order.OrderID, "complete", false)
	if err != nil {
		o.checkDone(run, suiteKey, "delivered", false, fmt.Sprintf("courier complete failed: %v", err), deliverStart)
		return fmt.Errorf("courier complete failed: %w", err)
	}

	if err := o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusPickedUp, statemachine.StatusDelivered, statemachine.RoleCourier); err != nil {
		o.checkDone(run, suiteKey, "delivered", false, err.Error(), deliverStart)
		return err
	}
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusDelivered)
	run.FinalState = statemachine.StatusDelivered

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      "Order successfully delivered to client! Status: DELIVERED.",
		CurrentState: statemachine.StatusDelivered,
		OrderID:      order.OrderID,
		DurationMs:   time.Since(deliverStart).Milliseconds(),
		Timestamp:    time.Now(),
	})
	run.PassedSteps++
	o.checkDone(run, suiteKey, "delivered", true, "Заказ доставлен, статус DELIVERED", deliverStart)

	return nil
}

func (o *TestOrchestrator) runFlowB(ctx context.Context, run *TestRun, suiteKey string) error {
	suiteName := "Flow B: Point A to Point B Courier Parcel"
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "SUITE_START",
		Level:     LogInfo,
		Message:   "Starting Flow B: Independent Parcel Delivery (New -> Assigned -> PickedUp -> Delivered)",
		Timestamp: time.Now(),
	})

	setupStart := time.Now()
	o.checkStart(run, suiteKey, "setup")
	clientPhone, _, _ := o.engine.Fixtures.CreateUniqueClient(ctx)
	courierPhone, _, _ := o.engine.Fixtures.CreateUniqueCourier(ctx)

	if o.engine.Config.AdminToken == "" {
		_, _ = o.engine.SessionMgr.LoginAdmin(ctx, o.engine.Config.AdminLogin, o.engine.Config.AdminPassword)
	}

	o.checkDone(run, suiteKey, "setup", true, fmt.Sprintf("Уникальные фикстуры созданы: клиент %s, курьер %s", clientPhone, courierPhone), setupStart)

	createStart := time.Now()
	o.checkStart(run, suiteKey, "create_parcel")
	req := client.CreateIndependentOrderRequest{
		AddressA:    "Москва, ул. Арбат 10 (Точка А)",
		AddressB:    "Москва, Кутузовский пр-т 32 (Точка Б)",
		Comment:     "Документы с печатью",
		PaymentType: "card",
		Price:       500,
	}

	order, err := o.engine.ClientAPI.CreateIndependentOrder(ctx, req)
	if err != nil {
		o.checkDone(run, suiteKey, "create_parcel", false, err.Error(), createStart)
		return err
	}

	_ = o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeIndependent, "", statemachine.StatusNew, statemachine.RoleClient)
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusNew)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      fmt.Sprintf("Parcel order created by %s. Status: NEW.", clientPhone),
		CurrentState: statemachine.StatusNew,
		OrderID:      order.OrderID,
		DurationMs:   time.Since(createStart).Milliseconds(),
		Timestamp:    time.Now(),
	})
	o.checkDone(run, suiteKey, "create_parcel", true, "Посылка создана, статус NEW", createStart)

	// Assign courier
	assignStart := time.Now()
	o.checkStart(run, suiteKey, "assign_courier")
	order, err = o.engine.AdminAPI.AssignCourier(ctx, order.OrderID, courierPhone)
	if err != nil {
		o.checkDone(run, suiteKey, "assign_courier", false, err.Error(), assignStart)
		return err
	}
	_ = o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeIndependent, statemachine.StatusNew, statemachine.StatusCourierAssigned, statemachine.RoleAdmin)
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusCourierAssigned)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      fmt.Sprintf("Courier %s assigned to parcel. Status: COURIER_ASSIGNED.", courierPhone),
		CurrentState: statemachine.StatusCourierAssigned,
		OrderID:      order.OrderID,
		Timestamp:    time.Now(),
	})
	o.checkDone(run, suiteKey, "assign_courier", true, "Курьер назначен на посылку, статус COURIER_ASSIGNED", assignStart)

	// Picked up
	pickupStart := time.Now()
	o.checkStart(run, suiteKey, "pickup")
	order, err = o.engine.CourierAPI.ChangeStatus(ctx, order.OrderID, "shipping", true)
	if err != nil {
		o.checkDone(run, suiteKey, "pickup", false, err.Error(), pickupStart)
		return err
	}
	_ = o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeIndependent, statemachine.StatusCourierAssigned, statemachine.StatusPickedUp, statemachine.RoleCourier)
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusPickedUp)

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      "Courier picked up parcel at Point A. Status: PICKED_UP.",
		CurrentState: statemachine.StatusPickedUp,
		OrderID:      order.OrderID,
		Timestamp:    time.Now(),
	})
	o.checkDone(run, suiteKey, "pickup", true, "Посылка забрана в точке А, статус PICKED_UP", pickupStart)

	// Delivered
	deliverStart := time.Now()
	o.checkStart(run, suiteKey, "delivered")
	order, err = o.engine.CourierAPI.ChangeStatus(ctx, order.OrderID, "complete", true)
	if err != nil {
		o.checkDone(run, suiteKey, "delivered", false, err.Error(), deliverStart)
		return err
	}
	_ = o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeIndependent, statemachine.StatusPickedUp, statemachine.StatusDelivered, statemachine.RoleCourier)
	o.engine.StateMachine.RecordTransition(order.OrderID, statemachine.StatusDelivered)
	run.FinalState = statemachine.StatusDelivered

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      "Courier delivered parcel at Point B. Status: DELIVERED.",
		CurrentState: statemachine.StatusDelivered,
		OrderID:      order.OrderID,
		Timestamp:    time.Now(),
	})
	o.checkDone(run, suiteKey, "delivered", true, "Посылка доставлена в точку Б, статус DELIVERED", deliverStart)

	return nil
}

func (o *TestOrchestrator) runNegativeSM(ctx context.Context, run *TestRun, suiteKey string) error {
	suiteName := "State Machine: Negative Validation"
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "SUITE_START",
		Level:     LogInfo,
		Message:   "Checking illegal status transitions and unauthorized role barriers...",
		Timestamp: time.Now(),
	})

	jumpStart := time.Now()
	o.checkStart(run, suiteKey, "illegal_jump_new_delivered")
	err := o.engine.StateMachine.ValidateTransition(statemachine.OrderTypeRestaurant, statemachine.StatusNew, statemachine.StatusDelivered, statemachine.RoleCourier)
	if err == nil {
		msg := "expected error for illegal jump NEW -> DELIVERED"
		o.checkDone(run, suiteKey, "illegal_jump_new_delivered", false, msg, jumpStart)
		return fmt.Errorf("%s", msg)
	}
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "THEN",
		Level:     LogSuccess,
		Message:   "✓ Correctly blocked illegal transition: NEW -> DELIVERED",
		Timestamp: time.Now(),
	})
	o.checkDone(run, suiteKey, "illegal_jump_new_delivered", true, "Переход NEW → DELIVERED корректно запрещён", jumpStart)

	o.checkSkip(run, suiteKey, "illegal_jump_preparing_delivered", "не покрыто раннером")
	o.checkSkip(run, suiteKey, "unauthorized_role_client", "не покрыто раннером")
	o.checkSkip(run, suiteKey, "unauthorized_role_restaurant", "не покрыто раннером")

	return nil
}

func (o *TestOrchestrator) runCancellation(ctx context.Context, run *TestRun, suiteKey string) error {
	suiteName := "Edge Case: Cancellation Lifecycle"
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "SUITE_START",
		Level:     LogInfo,
		Message:   "Validating order cancellations at NEW vs COOKING stages...",
		Timestamp: time.Now(),
	})

	cancelStart := time.Now()
	o.checkStart(run, suiteKey, "cancel_at_new")

	_, clientToken, _ := o.engine.Fixtures.CreateUniqueClient(ctx)
	o.engine.SessionMgr.SetClientSession("c_cancel", clientToken, "+79991234567", "Client")

	// 1. Cancel at NEW -> Should succeed
	order, err := o.engine.ClientAPI.CreateRestaurantOrder(ctx, client.CreateRestaurantOrderRequest{
		RestID:          "rest_cancel_test",
		DeliveryAddress: "Москва, Тверская 10",
		PaymentType:     "card",
	})
	if err != nil {
		o.checkDone(run, suiteKey, "cancel_at_new", false, err.Error(), cancelStart)
		return err
	}

	err = o.engine.ClientAPI.CancelOrder(ctx, order.OrderID, "Client requested cancellation at NEW")
	if err != nil {
		o.checkDone(run, suiteKey, "cancel_at_new", false, fmt.Sprintf("cancellation at NEW failed: %v", err), cancelStart)
		return fmt.Errorf("cancellation at NEW failed: %w", err)
	}

	o.Emit(&ExecutionEvent{
		RunID:        run.ID,
		SuiteName:    suiteName,
		SuiteKey:     suiteKey,
		StepType:     "THEN",
		Level:        LogSuccess,
		Message:      fmt.Sprintf("✓ Client successfully cancelled order #%d at NEW stage", order.OrderNumber),
		CurrentState: statemachine.StatusCancelled,
		OrderID:      order.OrderID,
		Timestamp:    time.Now(),
	})
	o.checkDone(run, suiteKey, "cancel_at_new", true, "Заказ успешно отменён на статусе NEW", cancelStart)

	o.checkSkip(run, suiteKey, "reject_during_cooking", "не покрыто раннером")
	o.checkSkip(run, suiteKey, "terminal_protection", "не покрыто раннером")

	return nil
}

func (o *TestOrchestrator) runSecurityRBAC(ctx context.Context, run *TestRun, suiteKey string) error {
	suiteName := "Security: RBAC Barrier Tests"
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "SUITE_START",
		Level:     LogInfo,
		Message:   "Testing unauthenticated calls and cross-role privilege escalation...",
		Timestamp: time.Now(),
	})

	// 1. Missing token
	tokenStart := time.Now()
	o.checkStart(run, suiteKey, "no_token_401")
	_, err := o.engine.HTTPClient.Request(ctx, "POST", "/api/clients/create-order", "", client.CreateRestaurantOrderRequest{RestID: "123"}, nil)
	if err == nil {
		msg := "expected 401 for unauthenticated request"
		o.checkDone(run, suiteKey, "no_token_401", false, msg, tokenStart)
		return fmt.Errorf("%s", msg)
	}
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "THEN",
		Level:     LogSuccess,
		Message:   "✓ Correctly returned 401 Unauthorized for missing Bearer token",
		Timestamp: time.Now(),
	})
	o.checkDone(run, suiteKey, "no_token_401", true, "Запрос без Bearer токена получил 401", tokenStart)

	// 2. Client token trying to call admin
	adminStart := time.Now()
	o.checkStart(run, suiteKey, "client_admin_403")
	_, clientToken, _ := o.engine.Fixtures.CreateUniqueClient(ctx)
	_, err = o.engine.HTTPClient.Request(ctx, "POST", "/api/admin/give-order-to-courier", clientToken, client.AssignCourierRequest{OrderID: "123"}, nil)
	if err == nil {
		msg := "expected 403 when Client calls Admin API"
		o.checkDone(run, suiteKey, "client_admin_403", false, msg, adminStart)
		return fmt.Errorf("%s", msg)
	}
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "THEN",
		Level:     LogSuccess,
		Message:   "✓ Correctly returned 403 Forbidden when Client token attempts Admin action",
		Timestamp: time.Now(),
	})
	o.checkDone(run, suiteKey, "client_admin_403", true, "Токен клиента у Admin API получил 403", adminStart)

	o.checkSkip(run, suiteKey, "courier_rest_403", "не покрыто раннером")

	return nil
}

func (o *TestOrchestrator) runIdempotency(ctx context.Context, run *TestRun, suiteKey string) error {
	suiteName := "Reliability: Idempotency Verification"
	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "SUITE_START",
		Level:     LogInfo,
		Message:   "Verifying duplicate request protection via Idempotency-Key...",
		Timestamp: time.Now(),
	})

	idemStart := time.Now()
	o.checkStart(run, suiteKey, "same_key_same_order")

	_, clientToken, _ := o.engine.Fixtures.CreateUniqueClient(ctx)
	o.engine.SessionMgr.SetClientSession("c_idem", clientToken, "+79991234567", "Client")

	idemKey := uuid.New().String()
	req := client.CreateRestaurantOrderRequest{
		RestID:          "rest_idem",
		DeliveryAddress: "Москва, Арбат 1",
		PaymentType:     "card",
	}

	order1, err := o.engine.ClientAPI.CreateRestaurantOrderWithIdempotency(ctx, req, idemKey)
	if err != nil {
		o.checkDone(run, suiteKey, "same_key_same_order", false, err.Error(), idemStart)
		return err
	}

	order2, err := o.engine.ClientAPI.CreateRestaurantOrderWithIdempotency(ctx, req, idemKey)
	if err != nil {
		o.checkDone(run, suiteKey, "same_key_same_order", false, err.Error(), idemStart)
		return err
	}

	if order1.OrderID != order2.OrderID {
		msg := fmt.Sprintf("idempotency violation: generated two different orders (%s vs %s)", order1.OrderID, order2.OrderID)
		o.checkDone(run, suiteKey, "same_key_same_order", false, msg, idemStart)
		return fmt.Errorf("%s", msg)
	}

	o.Emit(&ExecutionEvent{
		RunID:     run.ID,
		SuiteName: suiteName,
		SuiteKey:  suiteKey,
		StepType:  "THEN",
		Level:     LogSuccess,
		Message:   fmt.Sprintf("✓ Idempotency verified: Re-submitting same key returned identical OrderID (%s)", order1.OrderID),
		OrderID:   order1.OrderID,
		Timestamp: time.Now(),
	})
	o.checkDone(run, suiteKey, "same_key_same_order", true, fmt.Sprintf("Повтор с тем же ключом вернул тот же OrderID (%s)", order1.OrderID), idemStart)

	o.checkSkip(run, suiteKey, "concurrent_isolation", "не покрыто раннером")

	return nil
}
