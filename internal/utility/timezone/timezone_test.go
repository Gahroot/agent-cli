package timezone

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()
	if cmd.Use != "timezone" {
		t.Errorf("expected Use 'timezone', got %q", cmd.Use)
	}

	aliases := cmd.Aliases
	if len(aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(aliases))
	}

	subs := map[string]bool{}
	for _, s := range cmd.Commands() {
		subs[s.Use] = true
	}
	for _, name := range []string{"get [timezone]", "ip [ip-address]", "list"} {
		if !subs[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestGetCmd(t *testing.T) {
	// get command now uses Go's built-in time.LoadLocation, no HTTP needed
	cmd := newGetCmd()
	cmd.SetArgs([]string{"America/New_York"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("get command failed: %v", err)
	}
}

func TestGetNotFound(t *testing.T) {
	// get command now uses Go's built-in time.LoadLocation
	cmd := newGetCmd()
	cmd.SetArgs([]string{"Invalid/Timezone"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid timezone, got nil")
	}
}

func TestIPCmd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{
			"year":      2024,
			"month":     1,
			"day":       15,
			"hour":      7,
			"minute":    30,
			"seconds":   45,
			"dateTime":  "2024-01-15T07:30:45",
			"timeZone":  "America/Los_Angeles",
			"dayOfWeek": "Monday",
			"dstActive": false,
			"currentUtcOffset": map[string]any{
				"seconds": -28800,
			},
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer srv.Close()

	oldURL := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldURL }()

	cmd := newIPCmd()
	cmd.SetArgs([]string{"8.8.8.8"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("ip command failed: %v", err)
	}
}

func TestListCmd(t *testing.T) {
	// list command now uses built-in timezone list, no HTTP needed
	cmd := newListCmd()
	err := cmd.Execute()
	if err != nil {
		t.Errorf("list command failed: %v", err)
	}
}

func TestFetchTimezoneByIPHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	oldURL := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldURL }()

	err := fetchTimezoneByIP("8.8.8.8")
	if err == nil {
		t.Error("expected HTTP error, got nil")
	}
}

func TestFetchTimezoneByIPNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	oldURL := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldURL }()

	err := fetchTimezoneByIP("0.0.0.0")
	if err == nil {
		t.Error("expected not found error, got nil")
	}
}

func TestFetchTimezoneByIPParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid json"))
	}))
	defer srv.Close()

	oldURL := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldURL }()

	err := fetchTimezoneByIP("8.8.8.8")
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

func TestGetTimezoneLocalUTC(t *testing.T) {
	// UTC should always work
	err := getTimezoneLocal("UTC")
	if err != nil {
		t.Errorf("getTimezoneLocal(UTC) failed: %v", err)
	}
}

func TestGetTimezoneLocalInvalid(t *testing.T) {
	err := getTimezoneLocal("Not/A/Real/Zone")
	if err == nil {
		t.Error("expected error for invalid timezone, got nil")
	}
}

func TestListTimezonesOutput(t *testing.T) {
	err := listTimezones()
	if err != nil {
		t.Errorf("listTimezones failed: %v", err)
	}
}
