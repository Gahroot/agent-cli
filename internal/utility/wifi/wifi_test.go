package wifi

import (
	"testing"
)

func TestNewCmd(t *testing.T) {
	cmd := NewCmd()

	if cmd.Use != "wifi" {
		t.Errorf("expected Use=wifi, got %s", cmd.Use)
	}

	found := false
	for _, a := range cmd.Aliases {
		if a == "wf" {
			found = true
		}
	}
	if !found {
		t.Error("expected alias 'wf'")
	}

	subs := map[string]bool{"scan": false, "current": false}
	for _, sub := range cmd.Commands() {
		subs[sub.Use] = true
	}
	for name, present := range subs {
		if !present {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestParseChannelNumber(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"40 (5GHz, 80MHz)", 40},
		{"11 (2GHz, 20MHz)", 11},
		{"149 (5GHz)", 149},
		{"6", 6},
		{"", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		got := parseChannelNumber(tt.input)
		if got != tt.want {
			t.Errorf("parseChannelNumber(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCleanSecurityMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"spairport_security_mode_wpa2_personal", "wpa2-personal"},
		{"spairport_security_mode_wpa2_personal_mixed", "wpa2-personal-mixed"},
		{"spairport_security_mode_wpa3_personal", "wpa3-personal"},
		{"spairport_security_mode_open", "open"},
		{"", ""},
		{"custom_mode", "custom-mode"},
	}
	for _, tt := range tests {
		got := cleanSecurityMode(tt.input)
		if got != tt.want {
			t.Errorf("cleanSecurityMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseSignalNoise(t *testing.T) {
	tests := []struct {
		input     string
		wantRSSI  int
		wantNoise int
	}{
		{"-48 dBm / -92 dBm", -48, -92},
		{"-55 dBm / -90 dBm", -55, -90},
		{"-70dBm/-95dBm", -70, -95},
		{"", 0, 0},
		{"no match", 0, 0},
	}
	for _, tt := range tests {
		rssi, noise := parseSignalNoise(tt.input)
		if rssi != tt.wantRSSI {
			t.Errorf("parseSignalNoise(%q) rssi = %d, want %d", tt.input, rssi, tt.wantRSSI)
		}
		if noise != tt.wantNoise {
			t.Errorf("parseSignalNoise(%q) noise = %d, want %d", tt.input, noise, tt.wantNoise)
		}
	}
}

func TestParseSystemProfilerScan(t *testing.T) {
	input := []byte(`{
  "SPAirPortDataType": [
    {
      "spairport_airport_interfaces": [
        {
          "_name": "en0",
          "spairport_status_information": "spairport_status_connected",
          "spairport_airport_other_local_wireless_networks": [
            {
              "_name": "HomeNetwork",
              "spairport_network_channel": "11 (2GHz, 20MHz)",
              "spairport_security_mode": "spairport_security_mode_wpa2_personal",
              "spairport_signal_noise": "-55 dBm / -90 dBm"
            },
            {
              "_name": "GuestWiFi",
              "spairport_network_channel": "6 (2GHz, 20MHz)",
              "spairport_security_mode": "spairport_security_mode_open"
            },
            {
              "_name": "OfficeNet",
              "spairport_network_channel": "36 (5GHz, 160MHz)",
              "spairport_security_mode": "spairport_security_mode_wpa3_personal",
              "spairport_signal_noise": "-42 dBm / -93 dBm"
            }
          ]
        }
      ]
    }
  ]
}`)

	networks := parseSystemProfilerScan(input)

	if len(networks) != 3 {
		t.Fatalf("expected 3 networks, got %d", len(networks))
	}

	if networks[0].SSID != "HomeNetwork" {
		t.Errorf("network 0 SSID = %q, want 'HomeNetwork'", networks[0].SSID)
	}
	if networks[0].Channel != 11 {
		t.Errorf("network 0 Channel = %d, want 11", networks[0].Channel)
	}
	if networks[0].RSSI != -55 {
		t.Errorf("network 0 RSSI = %d, want -55", networks[0].RSSI)
	}
	if networks[0].Security != "wpa2-personal" {
		t.Errorf("network 0 Security = %q, want 'wpa2-personal'", networks[0].Security)
	}

	if networks[1].SSID != "GuestWiFi" {
		t.Errorf("network 1 SSID = %q, want 'GuestWiFi'", networks[1].SSID)
	}
	if networks[1].RSSI != 0 {
		t.Errorf("network 1 RSSI = %d, want 0 (no signal data)", networks[1].RSSI)
	}
	if networks[1].Security != "open" {
		t.Errorf("network 1 Security = %q, want 'open'", networks[1].Security)
	}

	if networks[2].SSID != "OfficeNet" {
		t.Errorf("network 2 SSID = %q, want 'OfficeNet'", networks[2].SSID)
	}
	if networks[2].Channel != 36 {
		t.Errorf("network 2 Channel = %d, want 36", networks[2].Channel)
	}
}

func TestParseSystemProfilerScanEmpty(t *testing.T) {
	networks := parseSystemProfilerScan([]byte(`{}`))
	if len(networks) != 0 {
		t.Errorf("expected 0 networks for empty input, got %d", len(networks))
	}
}

func TestParseSystemProfilerScanNoNetworks(t *testing.T) {
	input := []byte(`{
  "SPAirPortDataType": [
    {
      "spairport_airport_interfaces": [
        {
          "_name": "en0",
          "spairport_status_information": "spairport_status_connected"
        }
      ]
    }
  ]
}`)
	networks := parseSystemProfilerScan(input)
	if len(networks) != 0 {
		t.Errorf("expected 0 networks when no nearby networks, got %d", len(networks))
	}
}

func TestParseSystemProfilerScanInvalidJSON(t *testing.T) {
	networks := parseSystemProfilerScan([]byte(`not json`))
	if len(networks) != 0 {
		t.Errorf("expected 0 networks for invalid JSON, got %d", len(networks))
	}
}

func TestParseSystemProfilerCurrent(t *testing.T) {
	input := []byte(`{
  "SPAirPortDataType": [
    {
      "spairport_airport_interfaces": [
        {
          "_name": "en0",
          "spairport_status_information": "spairport_status_connected",
          "spairport_current_network_information": {
            "_name": "MyNetwork",
            "spairport_network_channel": "149 (5GHz, 80MHz)",
            "spairport_security_mode": "spairport_security_mode_wpa2_personal",
            "spairport_signal_noise": "-55 dBm / -90 dBm",
            "spairport_network_rate": 866
          }
        }
      ]
    }
  ]
}`)

	info := parseSystemProfilerCurrent(input)

	if !info.Connected {
		t.Error("expected connected=true")
	}
	if info.SSID != "MyNetwork" {
		t.Errorf("SSID = %q, want 'MyNetwork'", info.SSID)
	}
	if info.RSSI != -55 {
		t.Errorf("RSSI = %d, want -55", info.RSSI)
	}
	if info.Noise != -90 {
		t.Errorf("Noise = %d, want -90", info.Noise)
	}
	if info.Channel != 149 {
		t.Errorf("Channel = %d, want 149", info.Channel)
	}
	if info.TxRate != "866 Mbps" {
		t.Errorf("TxRate = %q, want '866 Mbps'", info.TxRate)
	}
	if info.Security != "wpa2-personal" {
		t.Errorf("Security = %q, want 'wpa2-personal'", info.Security)
	}
}

func TestParseSystemProfilerCurrentDisconnected(t *testing.T) {
	input := []byte(`{
  "SPAirPortDataType": [
    {
      "spairport_airport_interfaces": [
        {
          "_name": "en0",
          "spairport_status_information": "spairport_status_init"
        }
      ]
    }
  ]
}`)

	info := parseSystemProfilerCurrent(input)
	if info.Connected {
		t.Error("expected connected=false for disconnected state")
	}
	if info.SSID != "" {
		t.Errorf("expected empty SSID, got %q", info.SSID)
	}
}

func TestParseSystemProfilerCurrentNoInterface(t *testing.T) {
	info := parseSystemProfilerCurrent([]byte(`{}`))
	if info.Connected {
		t.Error("expected connected=false for empty data")
	}
}

func TestFindWiFiInterface(t *testing.T) {
	// Should find en0
	input := []byte(`{
  "SPAirPortDataType": [
    {
      "spairport_airport_interfaces": [
        {"_name": "awdl0"},
        {"_name": "en0", "spairport_status_information": "spairport_status_connected"}
      ]
    }
  ]
}`)
	iface := findWiFiInterface(input)
	if iface == nil {
		t.Fatal("expected to find en0 interface")
	}
	if iface.Name != "en0" {
		t.Errorf("expected en0, got %q", iface.Name)
	}
}

func TestFindWiFiInterfaceFallback(t *testing.T) {
	// Should fall back to first interface when en0 not found
	input := []byte(`{
  "SPAirPortDataType": [
    {
      "spairport_airport_interfaces": [
        {"_name": "en1", "spairport_status_information": "spairport_status_connected"}
      ]
    }
  ]
}`)
	iface := findWiFiInterface(input)
	if iface == nil {
		t.Fatal("expected to find fallback interface")
	}
	if iface.Name != "en1" {
		t.Errorf("expected en1 fallback, got %q", iface.Name)
	}
}

func TestFindWiFiInterfaceEmpty(t *testing.T) {
	iface := findWiFiInterface([]byte(`{}`))
	if iface != nil {
		t.Error("expected nil for empty data")
	}
}

func TestConnectionInfoTypes(t *testing.T) {
	info := ConnectionInfo{
		SSID:      "TestNet",
		BSSID:     "aa:bb:cc:dd:ee:ff",
		RSSI:      -45,
		Noise:     -90,
		Channel:   36,
		TxRate:    "866 Mbps",
		Security:  "wpa3",
		Connected: true,
	}

	if info.SSID != "TestNet" {
		t.Errorf("expected SSID=TestNet, got %s", info.SSID)
	}
	if !info.Connected {
		t.Error("expected connected=true")
	}
}

func TestScanResultTypes(t *testing.T) {
	result := ScanResult{
		Networks: []Network{
			{SSID: "Net1", RSSI: -50, Channel: 6},
			{SSID: "Net2", RSSI: -70, Channel: 11},
		},
		Count: 2,
	}

	if result.Count != 2 {
		t.Errorf("expected count=2, got %d", result.Count)
	}
	if len(result.Networks) != 2 {
		t.Errorf("expected 2 networks, got %d", len(result.Networks))
	}
}
