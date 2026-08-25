package tests

import (
	"os"
	"testing"

	"locali-e2e-engine/config"
	"locali-e2e-engine/pkg/dsl"
	"locali-e2e-engine/testserver"
)

var (
	testEngine *dsl.Engine
	mockServer *testserver.MockBackend
)

func TestMain(m *testing.M) {
	cfg := config.LoadFromEnv()

	// If BASE_URL is not provided or points to localhost default and not reachable, start embedded mock backend
	if os.Getenv("BASE_URL") == "" {
		mockServer = testserver.NewMockBackend()
		cfg.BaseURL = mockServer.URL()
	}

	var err error
	testEngine, err = dsl.NewEngine(cfg)
	if err != nil {
		panic("Failed to initialize test engine: " + err.Error())
	}

	code := m.Run()

	if mockServer != nil {
		mockServer.Close()
	}

	os.Exit(code)
}
