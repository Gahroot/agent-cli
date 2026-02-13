package wifi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/unstablemind/pocket/pkg/output"
)

// Network represents a WiFi network
type Network struct {
	SSID     string `json:"ssid"`
	BSSID    string `json:"bssid,omitempty"`
	RSSI     int    `json:"rssi,omitempty"`
	Channel  int    `json:"channel,omitempty"`
	Security string `json:"security,omitempty"`
}

// ScanResult holds WiFi scan results
type ScanResult struct {
	Networks []Network `json:"networks"`
	Count    int       `json:"count"`
}

// ConnectionInfo holds current WiFi connection details
type ConnectionInfo struct {
	SSID      string `json:"ssid"`
	BSSID     string `json:"bssid,omitempty"`
	RSSI      int    `json:"rssi,omitempty"`
	Noise     int    `json:"noise,omitempty"`
	Channel   int    `json:"channel,omitempty"`
	TxRate    string `json:"tx_rate,omitempty"`
	Security  string `json:"security,omitempty"`
	Connected bool   `json:"connected"`
}

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "wifi",
		Aliases: []string{"wf"},
		Short:   "WiFi network analysis commands",
	}

	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newCurrentCmd())

	return cmd
}

func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Scan nearby WiFi networks with signal strength",
		RunE: func(cmd *cobra.Command, args []string) error {
			return scanNetworks()
		},
	}
}

func newCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show current WiFi connection details",
		RunE: func(cmd *cobra.Command, args []string) error {
			return currentConnection()
		},
	}
}

func scanNetworks() error {
	switch runtime.GOOS {
	case "darwin":
		return scanDarwin()
	case "linux":
		return scanLinux()
	default:
		return output.PrintError("platform_unsupported",
			fmt.Sprintf("WiFi scan not supported on %s", runtime.GOOS),
			map[string]string{"supported": "macOS, Linux"})
	}
}

func currentConnection() error {
	switch runtime.GOOS {
	case "darwin":
		return currentDarwin()
	case "linux":
		return currentLinux()
	default:
		return output.PrintError("platform_unsupported",
			fmt.Sprintf("WiFi info not supported on %s", runtime.GOOS),
			map[string]string{"supported": "macOS, Linux"})
	}
}

// systemProfilerAirPort represents the JSON structure from system_profiler SPAirPortDataType -json
type systemProfilerAirPort struct {
	SPAirPortDataType []struct {
		Interfaces []spAirPortInterface `json:"spairport_airport_interfaces"`
	} `json:"SPAirPortDataType"`
}

type spAirPortInterface struct {
	Name           string             `json:"_name"`
	Status         string             `json:"spairport_status_information"`
	CurrentNetwork *spAirPortNetwork  `json:"spairport_current_network_information"`
	OtherNetworks  []spAirPortNetwork `json:"spairport_airport_other_local_wireless_networks"`
	MACAddress     string             `json:"spairport_wireless_mac_address"`
}

type spAirPortNetwork struct {
	Name         string `json:"_name"`
	Channel      string `json:"spairport_network_channel"`
	SecurityMode string `json:"spairport_security_mode"`
	SignalNoise  string `json:"spairport_signal_noise"`
	PhyMode      string `json:"spairport_network_phymode"`
	Rate         int    `json:"spairport_network_rate"`
	MCS          int    `json:"spairport_network_mcs"`
}

// macOS implementation using system_profiler (airport CLI was removed in macOS 14 Sonoma)
func scanDarwin() error {
	out, err := exec.Command("system_profiler", "SPAirPortDataType", "-json").CombinedOutput()
	if err != nil {
		return output.PrintError("wifi_scan_error",
			fmt.Sprintf("system_profiler failed: %v", err),
			map[string]string{"suggestion": "WiFi may be disabled"})
	}

	networks := parseSystemProfilerScan(out)

	return output.Print(ScanResult{
		Networks: networks,
		Count:    len(networks),
	})
}

func currentDarwin() error {
	out, err := exec.Command("system_profiler", "SPAirPortDataType", "-json").CombinedOutput()
	if err != nil {
		return output.PrintError("wifi_info_error",
			fmt.Sprintf("system_profiler failed: %v", err),
			map[string]string{"suggestion": "WiFi may be disabled"})
	}

	info := parseSystemProfilerCurrent(out)
	return output.Print(info)
}

// parseSystemProfilerScan extracts nearby networks from system_profiler JSON output
func parseSystemProfilerScan(data []byte) []Network {
	iface := findWiFiInterface(data)
	if iface == nil {
		return nil
	}

	var networks []Network
	for _, net := range iface.OtherNetworks {
		n := Network{
			SSID:     net.Name,
			Channel:  parseChannelNumber(net.Channel),
			Security: cleanSecurityMode(net.SecurityMode),
		}
		rssi, _ := parseSignalNoise(net.SignalNoise)
		if rssi != 0 {
			n.RSSI = rssi
		}
		if n.SSID != "" {
			networks = append(networks, n)
		}
	}

	return networks
}

// parseSystemProfilerCurrent extracts the current connection from system_profiler JSON output
func parseSystemProfilerCurrent(data []byte) ConnectionInfo {
	info := ConnectionInfo{}

	iface := findWiFiInterface(data)
	if iface == nil {
		return info
	}

	if iface.Status != "spairport_status_connected" || iface.CurrentNetwork == nil {
		return info
	}

	cur := iface.CurrentNetwork
	info.SSID = cur.Name
	info.Connected = cur.Name != ""
	info.Channel = parseChannelNumber(cur.Channel)
	info.Security = cleanSecurityMode(cur.SecurityMode)

	rssi, noise := parseSignalNoise(cur.SignalNoise)
	if rssi != 0 {
		info.RSSI = rssi
	}
	if noise != 0 {
		info.Noise = noise
	}

	if cur.Rate > 0 {
		info.TxRate = strconv.Itoa(cur.Rate) + " Mbps"
	}

	return info
}

// findWiFiInterface locates the primary WiFi interface (en0) from system_profiler JSON
func findWiFiInterface(data []byte) *spAirPortInterface {
	var sp systemProfilerAirPort
	if err := json.Unmarshal(data, &sp); err != nil {
		return nil
	}

	if len(sp.SPAirPortDataType) == 0 {
		return nil
	}

	for i, iface := range sp.SPAirPortDataType[0].Interfaces {
		if iface.Name == "en0" {
			return &sp.SPAirPortDataType[0].Interfaces[i]
		}
	}

	// Fall back to first interface if en0 not found
	if len(sp.SPAirPortDataType[0].Interfaces) > 0 {
		return &sp.SPAirPortDataType[0].Interfaces[0]
	}

	return nil
}

// parseChannelNumber extracts the numeric channel from strings like "40 (5GHz, 80MHz)"
func parseChannelNumber(ch string) int {
	if ch == "" {
		return 0
	}
	// Take the first space-delimited token
	parts := strings.Fields(ch)
	if len(parts) == 0 {
		return 0
	}
	v, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return v
}

// cleanSecurityMode converts system_profiler security mode strings to human-readable form
// e.g., "spairport_security_mode_wpa2_personal" -> "wpa2-personal"
func cleanSecurityMode(mode string) string {
	if mode == "" {
		return ""
	}
	// Strip the "spairport_security_mode_" prefix
	const prefix = "spairport_security_mode_"
	if strings.HasPrefix(mode, prefix) {
		mode = mode[len(prefix):]
	}
	// Replace underscores with hyphens for readability
	return strings.ReplaceAll(mode, "_", "-")
}

// parseSignalNoise extracts RSSI and noise from strings like "-48 dBm / -92 dBm"
func parseSignalNoise(sn string) (rssi int, noise int) {
	if sn == "" {
		return 0, 0
	}
	// Match pattern like "-48 dBm / -92 dBm"
	re := regexp.MustCompile(`(-?\d+)\s*dBm\s*/\s*(-?\d+)\s*dBm`)
	m := re.FindStringSubmatch(sn)
	if len(m) == 3 {
		rssi, _ = strconv.Atoi(m[1])
		noise, _ = strconv.Atoi(m[2])
	}
	return rssi, noise
}

// Linux implementation using nmcli
func scanLinux() error {
	out, err := exec.Command("nmcli", "-t", "-f", "SSID,BSSID,SIGNAL,CHAN,SECURITY", "dev", "wifi", "list").CombinedOutput()
	if err != nil {
		return output.PrintError("wifi_scan_error",
			fmt.Sprintf("nmcli scan failed: %v", err),
			map[string]string{"suggestion": "Ensure NetworkManager is installed and WiFi is enabled"})
	}

	var networks []Network
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ":", 5)
		if len(fields) < 5 {
			continue
		}

		n := Network{
			SSID:     fields[0],
			BSSID:    fields[1],
			Security: fields[4],
		}
		if sig, err := strconv.Atoi(fields[2]); err == nil {
			// nmcli reports signal strength as percentage, convert to approximate dBm
			n.RSSI = sig - 100
		}
		if ch, err := strconv.Atoi(fields[3]); err == nil {
			n.Channel = ch
		}
		networks = append(networks, n)
	}

	return output.Print(ScanResult{
		Networks: networks,
		Count:    len(networks),
	})
}

func currentLinux() error {
	out, err := exec.Command("nmcli", "-t", "-f", "GENERAL.CONNECTION,WIFI.SSID,WIFI.BSSID,WIFI.CHAN,WIFI.RATE,WIFI.SIGNAL,WIFI.SECURITY", "dev", "show", "wlan0").CombinedOutput()
	if err != nil {
		// Try common alternative interface names
		out, err = exec.Command("nmcli", "-t", "-f", "active,ssid,bssid,signal,chan,security", "dev", "wifi").CombinedOutput()
		if err != nil {
			return output.PrintError("wifi_info_error",
				fmt.Sprintf("nmcli failed: %v", err), nil)
		}
	}

	info := ConnectionInfo{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "WIFI.SSID", "GENERAL.CONNECTION":
			if val != "" && val != "--" {
				info.SSID = val
				info.Connected = true
			}
		case "WIFI.BSSID":
			info.BSSID = val
		case "WIFI.SIGNAL":
			if v, err := strconv.Atoi(val); err == nil {
				info.RSSI = v - 100
			}
		case "WIFI.CHAN":
			if v, err := strconv.Atoi(val); err == nil {
				info.Channel = v
			}
		case "WIFI.RATE":
			info.TxRate = val
		case "WIFI.SECURITY":
			info.Security = val
		}
	}

	return output.Print(info)
}
