package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestNewBaseProvider(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	base := NewBaseProvider("test-provider", true, client)

	if base.ID() != "test-provider" {
		t.Errorf("Expected ID 'test-provider', got %s", base.ID())
	}

	if !base.ShouldIncludeRawOutput() {
		t.Error("Expected includeRawOutput to be true")
	}

	if base.GetHTTPClient() != client {
		t.Error("Expected GetHTTPClient to return the same client")
	}
}

func TestNewBaseProviderWithAPIKey(t *testing.T) {
	tests := []struct {
		name        string
		primaryKey  string
		fallbackKey string
		primaryVal  string
		fallbackVal string
		expectedKey string
	}{
		{
			name:        "Uses primary key when available",
			primaryKey:  "TEST_PRIMARY_KEY",
			fallbackKey: "TEST_FALLBACK_KEY",
			primaryVal:  "primary-value",
			fallbackVal: "fallback-value",
			expectedKey: "primary-value",
		},
		{
			name:        "Uses fallback key when primary is empty",
			primaryKey:  "TEST_PRIMARY_KEY_EMPTY",
			fallbackKey: "TEST_FALLBACK_KEY_SET",
			primaryVal:  "",
			fallbackVal: "fallback-value",
			expectedKey: "fallback-value",
		},
		{
			name:        "Returns empty when both are empty",
			primaryKey:  "TEST_PRIMARY_NONE",
			fallbackKey: "TEST_FALLBACK_NONE",
			primaryVal:  "",
			fallbackVal: "",
			expectedKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.primaryVal != "" {
				os.Setenv(tt.primaryKey, tt.primaryVal)
				defer os.Unsetenv(tt.primaryKey)
			}
			if tt.fallbackVal != "" {
				os.Setenv(tt.fallbackKey, tt.fallbackVal)
				defer os.Unsetenv(tt.fallbackKey)
			}

			base, apiKey := NewBaseProviderWithAPIKey("test-id", false, tt.primaryKey, tt.fallbackKey)

			if apiKey != tt.expectedKey {
				t.Errorf("Expected API key %q, got %q", tt.expectedKey, apiKey)
			}

			if base.ID() != "test-id" {
				t.Errorf("Expected ID 'test-id', got %s", base.ID())
			}

			if base.GetHTTPClient() == nil {
				t.Error("Expected HTTP client to be initialized")
			}

			if base.GetHTTPClient().Timeout != 60*time.Second {
				t.Errorf("Expected client timeout 60s, got %v", base.GetHTTPClient().Timeout)
			}
		})
	}
}

func TestBaseProvider_Close(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	base := NewBaseProvider("test-provider", false, client)

	err := base.Close()
	if err != nil {
		t.Errorf("Expected no error on Close, got %v", err)
	}

	// Test with nil client
	baseNil := BaseProvider{id: "test", includeRawOutput: false, client: nil}
	err = baseNil.Close()
	if err != nil {
		t.Errorf("Expected no error on Close with nil client, got %v", err)
	}
}

func TestBaseProvider_SupportsStreaming(t *testing.T) {
	base := NewBaseProvider("test-provider", false, nil)

	if !base.SupportsStreaming() {
		t.Error("Expected SupportsStreaming to return true by default")
	}
}

func TestCheckHTTPError(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		responseBody  string
		expectError   bool
		errorContains string
	}{
		{
			name:         "Success status returns no error",
			statusCode:   http.StatusOK,
			responseBody: `{"success": true}`,
			expectError:  false,
		},
		{
			name:          "400 Bad Request returns error",
			statusCode:    http.StatusBadRequest,
			responseBody:  `{"error": "invalid request"}`,
			expectError:   true,
			errorContains: "400",
		},
		{
			name:          "401 Unauthorized returns error",
			statusCode:    http.StatusUnauthorized,
			responseBody:  `{"error": "unauthorized"}`,
			expectError:   true,
			errorContains: "401",
		},
		{
			name:          "500 Internal Server Error returns error",
			statusCode:    http.StatusInternalServerError,
			responseBody:  `{"error": "server error"}`,
			expectError:   true,
			errorContains: "500",
		},
		{
			name:          "Error includes response body",
			statusCode:    http.StatusBadRequest,
			responseBody:  `{"error": "specific error message"}`,
			expectError:   true,
			errorContains: "specific error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Make request
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("Failed to make test request: %v", err)
			}

			// Test CheckHTTPError
			err = CheckHTTPError(resp)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if tt.errorContains != "" {
					errStr := err.Error()
					if len(errStr) > 0 && tt.errorContains != "" {
						// Check if error message contains expected string
						found := false
						for i := 0; i <= len(errStr)-len(tt.errorContains); i++ {
							if errStr[i:i+len(tt.errorContains)] == tt.errorContains {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("Expected error to contain %q, got %q", tt.errorContains, errStr)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				// For successful responses, body should not be closed by CheckHTTPError
				defer resp.Body.Close()
			}
		})
	}
}

func TestUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name         string
		jsonData     string
		expectError  bool
		checkLatency bool
		checkRaw     bool
	}{
		{
			name:        "Valid JSON unmarshals successfully",
			jsonData:    `{"message": "hello", "count": 42}`,
			expectError: false,
		},
		{
			name:         "Invalid JSON returns error",
			jsonData:     `{"invalid json`,
			expectError:  true,
			checkLatency: true,
			checkRaw:     true,
		},
		{
			name:        "Empty JSON object unmarshals successfully",
			jsonData:    `{}`,
			expectError: false,
		},
		{
			name:         "Malformed JSON sets latency and raw",
			jsonData:     `{broken}`,
			expectError:  true,
			checkLatency: true,
			checkRaw:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			chatResp := &ChatResponse{}
			start := time.Now()

			err := UnmarshalJSON([]byte(tt.jsonData), &result, chatResp, start)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				if tt.checkLatency && chatResp.Latency == 0 {
					t.Error("Expected latency to be set on error")
				}
				if tt.checkRaw && len(chatResp.Raw) == 0 {
					t.Error("Expected raw response to be set on error")
				}
				if tt.checkRaw && string(chatResp.Raw) != tt.jsonData {
					t.Errorf("Expected raw to be %q, got %q", tt.jsonData, string(chatResp.Raw))
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				// Verify data was unmarshaled
				if len(result) == 0 && tt.jsonData != `{}` {
					t.Error("Expected result to be populated")
				}
			}
		})
	}
}

func TestUnmarshalJSON_TypeMismatch(t *testing.T) {
	// Test unmarshaling into wrong type
	type StructType struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	var result StructType
	chatResp := &ChatResponse{}
	start := time.Now()

	// This JSON doesn't match the struct
	jsonData := `{"different": "fields", "other": 123}`

	err := UnmarshalJSON([]byte(jsonData), &result, chatResp, start)

	// Should not error as JSON is valid, just doesn't populate the struct fully
	if err != nil {
		t.Errorf("Expected no error for valid JSON, got: %v", err)
	}
}

func TestSetErrorResponse(t *testing.T) {
	tests := []struct {
		name        string
		respBody    string
		checkFields bool
	}{
		{
			name:        "Sets latency and raw body",
			respBody:    `{"error": "test error"}`,
			checkFields: true,
		},
		{
			name:        "Works with empty body",
			respBody:    "",
			checkFields: true,
		},
		{
			name:        "Works with large body",
			respBody:    string(make([]byte, 10000)),
			checkFields: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatResp := &ChatResponse{}
			start := time.Now()

			// Wait a tiny bit to ensure latency is measurable
			time.Sleep(1 * time.Millisecond)

			SetErrorResponse(chatResp, []byte(tt.respBody), start)

			if tt.checkFields {
				if chatResp.Latency == 0 {
					t.Error("Expected latency to be set")
				}
				if chatResp.Latency < time.Millisecond {
					t.Error("Expected latency to be at least 1ms")
				}
				if string(chatResp.Raw) != tt.respBody {
					t.Errorf("Expected raw to be %q, got %q", tt.respBody, string(chatResp.Raw))
				}
			}
		})
	}
}

func TestBaseProvider_Integration(t *testing.T) {
	// Test a realistic flow using base provider helpers
	t.Run("Realistic error handling flow", func(t *testing.T) {
		// Create a test server that returns an error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "invalid_request",
				"message": "The request was malformed",
			})
		}))
		defer server.Close()

		base, _ := NewBaseProviderWithAPIKey("test", false, "TEST_KEY_1", "TEST_KEY_2")
		start := time.Now()
		chatResp := &ChatResponse{}

		// Make request
		resp, err := base.GetHTTPClient().Get(server.URL)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		// Check for HTTP error
		err = CheckHTTPError(resp)
		if err == nil {
			t.Error("Expected CheckHTTPError to return error for 400 status")
		}

		// Simulate setting error response
		errorBody := []byte(`{"error": "test"}`)
		SetErrorResponse(chatResp, errorBody, start)

		if chatResp.Latency == 0 {
			t.Error("Expected latency to be set")
		}
		if len(chatResp.Raw) == 0 {
			t.Error("Expected raw to be set")
		}
	})

	t.Run("Realistic success flow", func(t *testing.T) {
		// Create a test server that returns success
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": "success",
				"data": map[string]string{
					"message": "Hello, world!",
				},
			})
		}))
		defer server.Close()

		base, _ := NewBaseProviderWithAPIKey("test", true, "TEST_KEY_1", "TEST_KEY_2")
		start := time.Now()
		chatResp := &ChatResponse{}

		// Make request
		resp, err := base.GetHTTPClient().Get(server.URL)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Check for HTTP error (should be none)
		err = CheckHTTPError(resp)
		if err != nil {
			t.Errorf("Expected no error for 200 status, got: %v", err)
		}

		// Read and unmarshal response
		var result map[string]interface{}
		respBody, _ := json.Marshal(map[string]interface{}{
			"response": "success",
		})

		err = UnmarshalJSON(respBody, &result, chatResp, start)
		if err != nil {
			t.Errorf("Expected successful unmarshal, got: %v", err)
		}

		if result["response"] != "success" {
			t.Error("Expected response to be unmarshaled correctly")
		}
	})
}
