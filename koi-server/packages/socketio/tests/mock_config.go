package tests

import (
	"time"

	"github.com/goravel/framework/contracts/config"
)

type mockConfig struct {
	config map[string]any
}

func (m *mockConfig) Get(key string, defaultValue ...any) any {
	if val, ok := m.config[key]; ok {
		return val
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

func (m *mockConfig) GetBool(key string, defaultValue ...bool) bool {
	if val, ok := m.config[key].(bool); ok {
		return val
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return false
}

func (m *mockConfig) GetString(key string, defaultValue ...string) string {
	if val, ok := m.config[key].(string); ok {
		return val
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (m *mockConfig) GetInt(key string, defaultValue ...int) int {
	if val, ok := m.config[key].(int); ok {
		return val
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (m *mockConfig) GetFloat64(key string, defaultValue ...float64) float64 {
	if val, ok := m.config[key].(float64); ok {
		return val
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (m *mockConfig) GetDuration(key string, defaultValue ...time.Duration) time.Duration {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (m *mockConfig) GetStringSlice(key string, defaultValue ...[]string) []string {
	if val, ok := m.config[key].([]string); ok {
		return val
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

func (m *mockConfig) UnmarshalKey(key string, dest any) error {
	return nil
}

func (m *mockConfig) Add(key string, value any) {
	m.config[key] = value
}

func (m *mockConfig) Env(key string, defaultValue ...any) any {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

func (m *mockConfig) EnvBool(key string, defaultValue ...bool) bool {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return false
}

func (m *mockConfig) EnvString(key string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (m *mockConfig) EnvInt(key string, defaultValue ...int) int {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (m *mockConfig) EnvFloat64(key string, defaultValue ...float64) float64 {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (m *mockConfig) All() map[string]any {
	return m.config
}

func (m *mockConfig) Has(key string) bool {
	_, ok := m.config[key]
	return ok
}

func (m *mockConfig) Set(key string, value any) {
	m.config[key] = value
}

func (m *mockConfig) Load() error {
	return nil
}

func (m *mockConfig) Path() string {
	return ""
}

func (m *mockConfig) Watch(callback func()) {
}

func (m *mockConfig) Unwatch() {
}

func newMockConfig() config.Config {
	config := &mockConfig{
		config: make(map[string]any),
	}
	config.Add("socketio", map[string]any{
		"server": map[string]any{
			"enabled": true,
			"path":    "/socket.io",
		},
		"connection": map[string]any{
			"max_connections": 1000,
			"ping_interval":   25000,
			"ping_timeout":    5000,
		},
		"message": map[string]any{
			"max_message_size": 1048576,
			"allow_binary":     true,
		},
	})
	return config
}

func NewMockConfig() config.Config {
	return newMockConfig()
}
