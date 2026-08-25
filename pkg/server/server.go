package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"locali-e2e-engine/config"
	"locali-e2e-engine/pkg/client"
	"locali-e2e-engine/pkg/dsl"
	"locali-e2e-engine/pkg/registry"
	"locali-e2e-engine/pkg/runner"
	"locali-e2e-engine/pkg/scenario"
	"locali-e2e-engine/pkg/stands"
	"locali-e2e-engine/testserver"
)

// Server is the HTTP platform server hosting the Web Admin Panel and REST/SSE APIs
type Server struct {
	cfg          *config.Config
	engine       *dsl.Engine
	orchestrator *runner.TestOrchestrator
	mockBackend  *testserver.MockBackend
	store        *scenario.Store
	stands       *stands.Store
	webDir       string

	// Per-stand token vault state. lastStandID tracks the stand that owns the
	// SessionMgr contents right now ("" before the first applyStand); vaultMu
	// serializes vault save/restore so concurrent applyStand calls can never
	// race on the snapshot files.
	vaultDir    string
	vaultMu     sync.Mutex
	lastStandID string
}

// NewServer initializes the platform server
func NewServer(cfg *config.Config, webDir string) (*Server, error) {
	// If BaseURL is empty or default, launch embedded mock backend
	var mock *testserver.MockBackend
	if cfg.BaseURL == "" || cfg.BaseURL == "http://localhost:3000" {
		mock = testserver.NewMockBackend()
		cfg.BaseURL = mock.URL()
		log.Printf("[SERVER] Started embedded Locali Mock Backend on %s", cfg.BaseURL)
	}

	engine, err := dsl.NewEngine(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to init engine: %w", err)
	}

	// Initialize default admin token in vault if present
	if cfg.AdminToken != "" {
		engine.SessionMgr.AddToken("admin", "Default Admin (Env)", cfg.AdminLogin, cfg.AdminToken, true)
	}

	orchestrator := runner.NewTestOrchestrator(engine)

	// User-defined scenarios storage (JSON files under <DataDir>/scenarios)
	scenarioStore, err := scenario.NewStore(filepath.Join(cfg.DataDir, "scenarios"))
	if err != nil {
		return nil, fmt.Errorf("failed to init scenario store: %w", err)
	}
	orchestrator.SetCustomSource(scenarioStore)

	// Multi-stand targets storage (<DataDir>/stands.json)
	var defaultStand stands.Stand
	if mock != nil {
		defaultStand = stands.Stand{Name: "Встроенный мок", BaseURL: cfg.BaseURL, IsMock: true}
	} else {
		defaultStand = stands.Stand{Name: "Основной стенд", BaseURL: cfg.BaseURL}
	}
	standsStore, err := stands.NewStore(cfg.DataDir, defaultStand)
	if err != nil {
		return nil, fmt.Errorf("failed to init stands store: %w", err)
	}

	srv := &Server{
		cfg:          cfg,
		engine:       engine,
		orchestrator: orchestrator,
		mockBackend:  mock,
		store:        scenarioStore,
		stands:       standsStore,
		webDir:       webDir,
		vaultDir:     stands.VaultsDir(cfg.DataDir),
	}

	if act, ok := standsStore.Active(); ok {
		if act.IsMock && mock != nil && act.BaseURL != mock.URL() {
			act.BaseURL = mock.URL()
			_ = standsStore.ResyncMock(act.ID, act.BaseURL)
		}
		srv.applyStand(act)
	}

	return srv, nil
}

// Start starts listening on given port
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()

	// System & Config
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)

	// Multi-Token Vault Endpoints
	mux.HandleFunc("/api/tokens/vault", s.handleTokenVault)
	mux.HandleFunc("/api/tokens/add", s.handleTokenAdd)
	mux.HandleFunc("/api/tokens/activate", s.handleTokenActivate)
	mux.HandleFunc("/api/tokens/delete", s.handleTokenDelete)
	mux.HandleFunc("/api/tokens/auth-login", s.handleTokenAuthLogin)
	mux.HandleFunc("/api/tokens/register", s.handleTokenRegister)
	mux.HandleFunc("/api/tokens/preset", s.handleTokenPreset)

	// Sessions & Runners
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/generate", s.handleGenerateSession)
	mux.HandleFunc("/api/sessions/token", s.handleSetToken)
	mux.HandleFunc("/api/suites", s.handleSuites)
	mux.HandleFunc("/api/runs", s.handleRuns)
	mux.HandleFunc("/api/runs/", s.handleRunByID)
	mux.HandleFunc("/api/events", s.handleEventsSSE)
	mux.HandleFunc("/api/events/recent", s.handleRecentEvents)
	mux.HandleFunc("/api/manual/action", s.handleManualAction)

	// User-defined custom scenarios
	mux.HandleFunc("/api/scenarios", s.handleScenarios)
	mux.HandleFunc("/api/scenarios/", s.handleScenarioByID)

	// Stand targets (multi-backend switching)
	mux.HandleFunc("/api/stands", s.handleStands)
	mux.HandleFunc("/api/stands/", s.handleStandByID)

	// Static Web Admin UI
	fileServer := http.FileServer(http.Dir(s.webDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(s.webDir, r.URL.Path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("================================================================")
	log.Printf("🚀 Locali E2E Test Platform Admin UI: http://localhost:%d", port)
	log.Printf("🎯 Target Backend Base URL: %s", s.cfg.BaseURL)
	log.Printf("================================================================")

	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := map[string]interface{}{
		"status":     "online",
		"baseURL":    s.cfg.BaseURL,
		"adminLogin": s.cfg.AdminLogin,
		"hasAdmin":   s.engine.SessionMgr.GetAdminSession().Token != "",
		"hasClient":  s.engine.SessionMgr.GetClientSession().Token != "",
		"hasRest":    s.engine.SessionMgr.GetRestSession().Token != "",
		"hasCourier": s.engine.SessionMgr.GetCourierSession().Token != "",
		"isMockMode": s.mockBackend != nil,
	}
	if act, ok := s.stands.Active(); ok {
		resp["standId"] = act.ID
		resp["standName"] = act.Name
	} else {
		resp["standId"] = ""
		resp["standName"] = ""
	}

	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			BaseURL       string `json:"baseURL"`
			AdminLogin    string `json:"adminLogin"`
			AdminPassword string `json:"adminPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.BaseURL != "" {
			s.cfg.BaseURL = req.BaseURL
			s.engine.HTTPClient.SetBaseURL(req.BaseURL)
			if act, ok := s.stands.Active(); ok {
				_ = s.stands.Update(act.ID, act.Name, req.BaseURL, act.VerifyCode)
			}
		}
		if req.AdminLogin != "" {
			s.cfg.AdminLogin = req.AdminLogin
		}
		if req.AdminPassword != "" {
			s.cfg.AdminPassword = req.AdminPassword
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.cfg)
}

// Token Vault Handlers
func (s *Server) handleTokenVault(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.engine.SessionMgr.GetVault())
}

func (s *Server) handleTokenAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role       string `json:"role"`
		Name       string `json:"name"`
		Identifier string `json:"identifier"`
		Token      string `json:"token"`
		SetActive  bool   `json:"setActive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Token == "" || req.Role == "" {
		http.Error(w, `{"error":"role and token are required"}`, http.StatusBadRequest)
		return
	}

	profile := s.engine.SessionMgr.AddToken(req.Role, req.Name, req.Identifier, req.Token, req.SetActive)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func (s *Server) handleTokenActivate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.engine.SessionMgr.SetActiveProfile(req.Role, req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleTokenDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.engine.SessionMgr.DeleteProfile(req.Role, req.ID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleTokenAuthLogin(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var req struct {
		Role        string `json:"role"` // client, rest, courier, admin
		Login       string `json:"login"`
		PhoneNumber string `json:"phoneNumber"`
		Password    string `json:"password"`
		Code        string `json:"code"`
		ProfileName string `json:"profileName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var token string
	var identifier string
	var err error

	switch strings.ToLower(req.Role) {
	case "admin":
		identifier = req.Login
		if identifier == "" {
			identifier = s.cfg.AdminLogin
		}
		pwd := req.Password
		if pwd == "" {
			pwd = s.cfg.AdminPassword
		}
		token, err = s.engine.SessionMgr.LoginAdmin(ctx, identifier, pwd)

	case "rest", "restaurant":
		identifier = req.Login
		token, err = s.engine.RestAPI.Login(ctx, client.RestLoginRequest{
			Login:    req.Login,
			Password: req.Password,
		})

	case "courier":
		identifier = req.PhoneNumber
		if identifier == "" {
			identifier = req.Login
		}
		token, err = s.engine.CourierAPI.Login(ctx, client.CourierLoginRequest{
			PhoneNumber: identifier,
			Password:    req.Password,
		})

	case "client":
		identifier = req.PhoneNumber
		code := req.Code
		if code == "" {
			code = s.cfg.VerificationCode
		}
		token, err = s.engine.ClientAPI.Login(ctx, client.ClientLoginRequest{
			PhoneNumber:      identifier,
			VerificationCode: code,
		})
	default:
		http.Error(w, "invalid role (client, rest, courier, admin)", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Authentication failed: %v", err), http.StatusUnauthorized)
		return
	}

	profile := s.engine.SessionMgr.AddToken(req.Role, req.ProfileName, identifier, token, true)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func (s *Server) handleTokenRegister(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var req struct {
		Role           string `json:"role"` // client, rest, courier, admin
		ProfileName    string `json:"profileName"`
		Login          string `json:"login"`
		Password       string `json:"password"`
		RestName       string `json:"restName"`
		Email          string `json:"email"`
		PhoneNumber    string `json:"phoneNumber"`
		FirstName      string `json:"firstName"`
		LastName       string `json:"lastName"`
		DeliveryType   string `json:"deliveryType"`
		CityKey        string `json:"cityKey"`
		City           string `json:"city"`
		Address        string `json:"address"`
		DeliveryMethod string `json:"deliveryMethod"`
		LoginOnly      bool   `json:"loginOnly"`
		Code           string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var token string
	var identifier string
	var err error

	switch strings.ToLower(req.Role) {
	case "admin":
		identifier = req.Login
		if identifier == "" {
			identifier = s.cfg.AdminLogin
		}
		pwd := req.Password
		if pwd == "" {
			pwd = s.cfg.AdminPassword
		}
		token, err = s.engine.SessionMgr.LoginAdmin(ctx, identifier, pwd)

	case "rest", "restaurant":
		login := req.Login
		if login == "" {
			http.Error(w, "login is required for restaurant", http.StatusBadRequest)
			return
		}
		identifier = login
		if !req.LoginOnly {
			regReq := client.RestRegisterRequest{
				RestName:       req.RestName,
				Login:          login,
				Password:       req.Password,
				Email:          req.Email,
				PhoneNumber:    req.PhoneNumber,
				City:           req.City,
				DeliveryMethod: req.DeliveryMethod,
			}
			if regReq.RestName == "" {
				regReq.RestName = "Restaurant " + login
			}
			if regReq.City == "" {
				regReq.City = "Москва"
			}
			if regReq.DeliveryMethod == "" {
				regReq.DeliveryMethod = "locali"
			}
			if err := s.engine.RestAPI.Register(ctx, regReq); err != nil {
				log.Printf("[SERVER] Rest register note: %v (attempting login)", err)
			}
		}
		token, err = s.engine.RestAPI.Login(ctx, client.RestLoginRequest{
			Login:    login,
			Password: req.Password,
		})

	case "courier":
		phone := req.PhoneNumber
		if phone == "" {
			phone = req.Login
		}
		if phone == "" {
			http.Error(w, "phone number / login is required for courier", http.StatusBadRequest)
			return
		}
		identifier = phone
		regReq := client.CourierRegisterRequest{
			FirstName:    req.FirstName,
			LastName:     req.LastName,
			PhoneNumber:  phone,
			Login:        phone,
			Password:     req.Password,
			DeliveryType: req.DeliveryType,
			CityKey:      req.CityKey,
		}
		if regReq.FirstName == "" {
			regReq.FirstName = "Courier"
		}
		if regReq.LastName == "" {
			regReq.LastName = "Custom"
		}
		if regReq.DeliveryType == "" {
			regReq.DeliveryType = "car"
		}
		if regReq.CityKey == "" {
			regReq.CityKey = "msk"
		}
		if err := s.engine.CourierAPI.Register(ctx, regReq); err != nil {
			log.Printf("[SERVER] Courier register note: %v (attempting login)", err)
		}
		token, err = s.engine.CourierAPI.Login(ctx, client.CourierLoginRequest{
			PhoneNumber: phone,
			Password:    req.Password,
		})

	case "client":
		phone := req.PhoneNumber
		if phone == "" {
			http.Error(w, "phone number is required for client", http.StatusBadRequest)
			return
		}
		identifier = phone
		regReq := client.ClientRegisterRequest{
			PhoneNumber: phone,
			Address:     req.Address,
			CityKey:     req.CityKey,
		}
		if regReq.Address == "" {
			regReq.Address = "Москва, ул. Тверская 1"
		}
		if regReq.CityKey == "" {
			regReq.CityKey = "msk"
		}
		if err := s.engine.ClientAPI.Register(ctx, regReq); err != nil {
			log.Printf("[SERVER] Client register note: %v (attempting login)", err)
		}
		code := req.Code
		if code == "" {
			code = s.cfg.VerificationCode
		}
		token, err = s.engine.ClientAPI.Login(ctx, client.ClientLoginRequest{
			PhoneNumber:      phone,
			VerificationCode: code,
			FirstName:        req.FirstName,
			LastName:         req.LastName,
		})

	default:
		http.Error(w, "invalid role (client, rest, courier, admin)", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Registration/Login failed: %v", err), http.StatusUnauthorized)
		return
	}

	profile := s.engine.SessionMgr.AddToken(req.Role, req.ProfileName, identifier, token, true)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func (s *Server) handleTokenPreset(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// 1. Create 2 Clients
	c1Phone, c1Token, _ := s.engine.Fixtures.CreateUniqueClient(ctx)
	s.engine.SessionMgr.AddToken("client", "Клиент 1 (VIP)", c1Phone, c1Token, true)
	c2Phone, c2Token, _ := s.engine.Fixtures.CreateUniqueClient(ctx)
	s.engine.SessionMgr.AddToken("client", "Клиент 2 (Стандарт)", c2Phone, c2Token, false)

	// 2. Create 2 Restaurants
	r1Login, r1Token, _ := s.engine.Fixtures.CreateUniqueRestaurant(ctx)
	s.engine.SessionMgr.AddToken("rest", "Ресторан 1 (Бургеры)", r1Login, r1Token, true)
	r2Login, r2Token, _ := s.engine.Fixtures.CreateUniqueRestaurant(ctx)
	s.engine.SessionMgr.AddToken("rest", "Ресторан 2 (Суши)", r2Login, r2Token, false)

	// 3. Create 2 Couriers
	cr1Phone, cr1Token, _ := s.engine.Fixtures.CreateUniqueCourier(ctx)
	s.engine.SessionMgr.AddToken("courier", "Курьер 1 (Авто)", cr1Phone, cr1Token, true)
	cr2Phone, cr2Token, _ := s.engine.Fixtures.CreateUniqueCourier(ctx)
	s.engine.SessionMgr.AddToken("courier", "Курьер 2 (Вело)", cr2Phone, cr2Token, false)

	// 4. Admin
	adminToken, _ := s.engine.SessionMgr.LoginAdmin(ctx, s.cfg.AdminLogin, s.cfg.AdminPassword)
	s.engine.SessionMgr.AddToken("admin", "Главный Диспетчер", s.cfg.AdminLogin, adminToken, true)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.engine.SessionMgr.GetVault())
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"client":  s.engine.SessionMgr.GetClientSession(),
		"rest":    s.engine.SessionMgr.GetRestSession(),
		"courier": s.engine.SessionMgr.GetCourierSession(),
		"admin":   s.engine.SessionMgr.GetAdminSession(),
	})
}

func (s *Server) handleGenerateSession(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var req struct {
		Role string `json:"role"` // client, rest, courier, admin
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	switch strings.ToLower(req.Role) {
	case "client":
		phone, token, err := s.engine.Fixtures.CreateUniqueClient(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": phone, "token": token})
	case "rest":
		login, token, err := s.engine.Fixtures.CreateUniqueRestaurant(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": login, "token": token})
	case "courier":
		phone, token, err := s.engine.Fixtures.CreateUniqueCourier(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": phone, "token": token})
	case "admin":
		token, err := s.engine.SessionMgr.LoginAdmin(ctx, s.cfg.AdminLogin, s.cfg.AdminPassword)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": s.cfg.AdminLogin, "token": token})
	default:
		http.Error(w, "invalid role (use: client, rest, courier, admin)", http.StatusBadRequest)
	}
}

func (s *Server) handleSetToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role  string `json:"role"`
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.engine.SessionMgr.AddToken(req.Role, "Custom "+req.Role, req.ID, req.Token, true)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleSuites(w http.ResponseWriter, r *http.Request) {
	suites := s.orchestrator.Registry()
	for _, sc := range s.store.List() {
		checks := make([]registry.CheckItem, 0, len(sc.Steps))
		for _, st := range sc.Steps {
			title := st.Title
			if title == "" {
				title = st.ID
			}
			checks = append(checks, registry.CheckItem{ID: st.ID, Title: title})
		}
		suites = append(suites, registry.Suite{
			Key:         sc.Key,
			Title:       sc.Title,
			Description: sc.Description,
			Category:    "custom",
			Tags:        sc.Tags,
			Checks:      checks,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"suites": suites,
	})
}

// handleScenarios serves GET /api/scenarios (list) and POST /api/scenarios (create).
func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"scenarios": s.store.List()})
		return

	case http.MethodPost:
		var sc scenario.Scenario
		if err := json.NewDecoder(r.Body).Decode(&sc); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid json: %v"}`, err), http.StatusBadRequest)
			return
		}
		sc.Category = "custom"
		if err := sc.Validate(); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		if s.store.Exists(sc.Key) {
			http.Error(w, fmt.Sprintf(`{"error":"scenario %q already exists"}`, sc.Key), http.StatusConflict)
			return
		}
		if err := s.store.Save(&sc); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(sc)
		return

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleScenarioByID serves GET/PUT/DELETE /api/scenarios/{key}.
func (s *Server) handleScenarioByID(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/scenarios/")
	if key == "" || strings.Contains(key, "/") {
		http.Error(w, `{"error":"scenario key is required"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		sc, ok := s.store.Get(key)
		if !ok {
			http.Error(w, fmt.Sprintf(`{"error":"scenario %q not found"}`, key), http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(sc)

	case http.MethodPut:
		var sc scenario.Scenario
		if err := json.NewDecoder(r.Body).Decode(&sc); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid json: %v"}`, err), http.StatusBadRequest)
			return
		}
		sc.Category = "custom"
		if sc.Key == "" {
			sc.Key = key
		} else if sc.Key != key {
			http.Error(w, `{"error":"key in body must match key in URL"}`, http.StatusBadRequest)
			return
		}
		if err := sc.Validate(); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		if err := s.store.Save(&sc); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(sc)

	case http.MethodDelete:
		if !s.store.Exists(key) {
			http.Error(w, fmt.Sprintf(`{"error":"scenario %q not found"}`, key), http.StatusNotFound)
			return
		}
		if err := s.store.Delete(key); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStands(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"stands": s.stands.ListWithActive()})

	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			BaseURL    string `json:"baseURL"`
			VerifyCode string `json:"verifyCode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid json: %v"}`, err), http.StatusBadRequest)
			return
		}
		st, err := s.stands.Add(req.Name, req.BaseURL, req.VerifyCode)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(st)

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStandByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/stands/")
	if id := strings.TrimSuffix(rest, "/activate"); id != rest && !strings.Contains(id, "/") && id != "" {
		s.handleStandActivate(w, r, id)
		return
	}

	id := rest
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, `{"error":"stand id is required"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name       string `json:"name"`
			BaseURL    string `json:"baseURL"`
			VerifyCode string `json:"verifyCode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid json: %v"}`, err), http.StatusBadRequest)
			return
		}
		cur, ok := s.stands.Get(id)
		if !ok {
			http.Error(w, fmt.Sprintf(`{"error":"stand %q not found"}`, id), http.StatusNotFound)
			return
		}
		if err := s.stands.Update(id, req.Name, req.BaseURL, req.VerifyCode); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		updated, _ := s.stands.Get(id)
		isActive := false
		if act, ok := s.stands.Active(); ok {
			isActive = act.ID == id
		}
		if isActive && (updated.BaseURL != cur.BaseURL || updated.VerifyCode != cur.VerifyCode) {
			s.applyStand(updated)
		}
		_ = json.NewEncoder(w).Encode(updated)

	case http.MethodDelete:
		if _, ok := s.stands.Get(id); !ok {
			http.Error(w, fmt.Sprintf(`{"error":"stand %q not found"}`, id), http.StatusNotFound)
			return
		}
		if err := s.stands.Delete(id); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusConflict)
			return
		}
		s.vaultMu.Lock()
		stands.RemoveVault(s.cfg.DataDir, id)
		s.vaultMu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStandActivate(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	st, err := s.stands.Activate(id)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}
	s.applyStand(st)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "stand": st})
}

// applyStand points the engine at the given stand: swaps the base URL and,
// when the stand defines one, its verification code. The token vault is
// swapped too: the outgoing stand's vault is persisted to disk, then the
// incoming stand's vault (if a snapshot exists) is restored into the very
// same SessionManager instance — role APIs hold that pointer, so it is never
// replaced, only refilled.
func (s *Server) applyStand(st stands.Stand) {
	s.vaultMu.Lock()
	defer s.vaultMu.Unlock()

	s.applyTargetLocked(st)

	// Same stand re-applied (e.g. verify-code edit of the active stand):
	// keep the in-memory vault untouched.
	if st.ID == s.lastStandID {
		return
	}

	firstLoad := s.lastStandID == ""

	if !firstLoad {
		s.saveVaultLocked(s.lastStandID)
	}

	snap, ok, err := stands.LoadVault(s.cfg.DataDir, st.ID)
	if err != nil {
		log.Printf("[SERVER] Vault load for stand %q failed, starting empty: %v", st.ID, err)
		ok = false
	}

	switch {
	case ok:
		s.engine.SessionMgr.Restore(snap)
	case firstLoad:
		// Nothing persisted yet for the startup stand: keep whatever the
		// engine seeded (env preset tokens) — it belongs to this stand.
	default:
		s.engine.SessionMgr.Restore(nil)
	}

	if firstLoad {
		s.seedEnvTokens()
	}

	s.lastStandID = st.ID
}

// applyTargetLocked repoints the engine at the given stand without touching
// the token vault.
func (s *Server) applyTargetLocked(st stands.Stand) {
	s.cfg.BaseURL = st.BaseURL
	s.engine.HTTPClient.SetBaseURL(st.BaseURL)
	if st.VerifyCode != "" {
		s.cfg.VerificationCode = st.VerifyCode
		s.engine.Fixtures.SetVerificationCode(st.VerifyCode)
	}
	log.Printf("[SERVER] Switched to stand %q (%s): %s", st.Name, st.ID, st.BaseURL)
}

// saveVaultLocked persists the current vault contents under the given stand id.
func (s *Server) saveVaultLocked(standID string) {
	if err := stands.SaveVault(s.cfg.DataDir, standID, s.engine.SessionMgr.Snapshot()); err != nil {
		log.Printf("[SERVER] Vault save for stand %q failed: %v", standID, err)
	}
}

// seedEnvTokens re-registers env-provided preset tokens (ADMIN_TOKEN and
// friends) in the freshly loaded startup vault, mirroring the engine bootstrap
// behaviour. Tokens already present are not duplicated.
func (s *Server) seedEnvTokens() {
	presets := []struct{ role, name, identifier, token string }{
		{"client", "Preset (env)", "", s.cfg.ClientToken},
		{"rest", "Preset (env)", "", s.cfg.RestToken},
		{"courier", "Preset (env)", "", s.cfg.CourierToken},
		{"admin", "Default Admin (Env)", s.cfg.AdminLogin, s.cfg.AdminToken},
	}
	for _, p := range presets {
		if p.token == "" || s.engine.SessionMgr.HasToken(p.token) {
			continue
		}
		s.engine.SessionMgr.AddToken(p.role, p.name, p.identifier, p.token, true)
	}
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Suite  string   `json:"suite"`
			Suites []string `json:"suites"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if len(req.Suites) > 0 {
			runs, err := s.orchestrator.RunSuites(req.Suites)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(runs)
			return
		}

		if req.Suite == "" {
			req.Suite = "flow_a"
		}

		run, err := s.orchestrator.RunSuite(req.Suite)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(run)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.orchestrator.GetHistory())
}

func (s *Server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	run := s.orchestrator.GetRun(id)
	if run == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(run)
}

func (s *Server) handleEventsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	eventsChan := s.orchestrator.Broadcaster()

	// Initial ping
	fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-eventsChan:
			data, err := json.Marshal(event)
			if err == nil {
				fmt.Fprintf(w, "event: test_event\ndata: %s\n\n", string(data))
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleRecentEvents(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > runner.MaxRecentEvents {
		limit = runner.MaxRecentEvents
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"events": s.orchestrator.RecentEvents(limit),
	})
}

func (s *Server) handleManualAction(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var req struct {
		Action    string `json:"action"` // create_order, assign_courier, change_rest_status, courier_status
		OrderID   string `json:"orderId"`
		RestID    string `json:"restId"`
		CourierID string `json:"courierId"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var result interface{}
	var err error

	switch req.Action {
	case "create_order":
		result, err = s.engine.ClientAPI.CreateRestaurantOrder(ctx, client.CreateRestaurantOrderRequest{
			RestID:          req.RestID,
			DeliveryAddress: "Москва, Тверская 15",
			PaymentType:     "card",
		})
	case "assign_courier":
		result, err = s.engine.AdminAPI.AssignCourier(ctx, req.OrderID, req.CourierID)
	case "change_rest_status":
		result, err = s.engine.RestAPI.ChangeOrderStatus(ctx, req.OrderID, req.Status)
	case "courier_status":
		result, err = s.engine.CourierAPI.ChangeStatus(ctx, req.OrderID, req.Status, false)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
