package timezone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/unstablemind/pocket/pkg/output"
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	// baseURL is used only for IP-based timezone lookups via timeapi.io
	baseURL = "https://timeapi.io/api"
)

// TimeInfo is LLM-friendly timezone information
type TimeInfo struct {
	Timezone     string `json:"timezone"`
	DateTime     string `json:"datetime"`
	UTCOffset    string `json:"utc_offset"`
	DayOfWeek    int    `json:"day_of_week"`
	WeekNumber   int    `json:"week_number"`
	DST          bool   `json:"dst"`
	Abbreviation string `json:"abbreviation"`
	UnixTime     int64  `json:"unixtime"`
}

// NewCmd returns the timezone command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "timezone",
		Aliases: []string{"tz", "time"},
		Short:   "Timezone commands",
	}

	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newIPCmd())
	cmd.AddCommand(newListCmd())

	return cmd
}

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [timezone]",
		Short: "Get time for a timezone (e.g., America/New_York)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tz := args[0]
			return getTimezoneLocal(tz)
		},
	}

	return cmd
}

func newIPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ip [ip-address]",
		Short: "Get timezone by IP address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ip := args[0]
			return fetchTimezoneByIP(ip)
		},
	}

	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all available timezones",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listTimezones()
		},
	}

	return cmd
}

// getTimezoneLocal uses Go's built-in time package to get timezone info
// without requiring any external API.
func getTimezoneLocal(tz string) error {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return output.PrintError("not_found", fmt.Sprintf("Timezone not found: %s", tz), nil)
	}

	now := time.Now().In(loc)
	_, offset := now.Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	if minutes < 0 {
		minutes = -minutes
	}

	var utcOffset string
	if hours >= 0 {
		utcOffset = fmt.Sprintf("+%02d:%02d", hours, minutes)
	} else {
		utcOffset = fmt.Sprintf("-%02d:%02d", -hours, minutes)
	}

	zone, _ := now.Zone()
	_, isoWeek := now.ISOWeek()

	result := TimeInfo{
		Timezone:     tz,
		DateTime:     now.Format(time.RFC3339),
		UTCOffset:    utcOffset,
		DayOfWeek:    int(now.Weekday()),
		WeekNumber:   isoWeek,
		DST:          now.IsDST(),
		Abbreviation: zone,
		UnixTime:     now.Unix(),
	}

	return output.Print(result)
}

// fetchTimezoneByIP uses timeapi.io to look up timezone by IP address.
func fetchTimezoneByIP(ip string) error {
	reqURL := fmt.Sprintf("%s/time/current/ip?ipAddress=%s", baseURL, url.QueryEscape(ip))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, http.NoBody)
	if err != nil {
		return output.PrintError("fetch_failed", err.Error(), nil)
	}

	req.Header.Set("User-Agent", "Pocket-CLI/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return output.PrintError("fetch_failed", err.Error(), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return output.PrintError("not_found", "Timezone not found for IP", nil)
	}

	if resp.StatusCode >= 400 {
		return output.PrintError("fetch_failed", fmt.Sprintf("HTTP %d", resp.StatusCode), nil)
	}

	// timeapi.io response structure
	var data struct {
		Year             int    `json:"year"`
		Month            int    `json:"month"`
		Day              int    `json:"day"`
		Hour             int    `json:"hour"`
		Minute           int    `json:"minute"`
		Seconds          int    `json:"seconds"`
		DateTime         string `json:"dateTime"`
		TimeZone         string `json:"timeZone"`
		DayOfWeek        string `json:"dayOfWeek"`
		DSTActive        bool   `json:"dstActive"`
		CurrentUTCOffset struct {
			Seconds int `json:"seconds"`
		} `json:"currentUtcOffset"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return output.PrintError("parse_failed", err.Error(), nil)
	}

	// Calculate UTC offset string from seconds
	offsetSec := data.CurrentUTCOffset.Seconds
	hours := offsetSec / 3600
	minutes := (offsetSec % 3600) / 60
	if minutes < 0 {
		minutes = -minutes
	}

	var utcOffset string
	if hours >= 0 {
		utcOffset = fmt.Sprintf("+%02d:%02d", hours, minutes)
	} else {
		utcOffset = fmt.Sprintf("-%02d:%02d", -hours, minutes)
	}

	// Map day of week string to int
	dayOfWeekMap := map[string]int{
		"Sunday": 0, "Monday": 1, "Tuesday": 2, "Wednesday": 3,
		"Thursday": 4, "Friday": 5, "Saturday": 6,
	}
	dayOfWeek := dayOfWeekMap[data.DayOfWeek]

	// Parse the datetime to get week number and abbreviation
	loc, _ := time.LoadLocation(data.TimeZone)
	var abbrev string
	var weekNumber int
	if loc != nil {
		now := time.Now().In(loc)
		abbrev, _ = now.Zone()
		_, weekNumber = now.ISOWeek()
	}

	// Build datetime in RFC3339 format
	dateTime := data.DateTime
	if dateTime != "" && !strings.Contains(dateTime, "T") {
		dateTime = strings.Replace(dateTime, " ", "T", 1)
	}

	result := TimeInfo{
		Timezone:     data.TimeZone,
		DateTime:     dateTime,
		UTCOffset:    utcOffset,
		DayOfWeek:    dayOfWeek,
		WeekNumber:   weekNumber,
		DST:          data.DSTActive,
		Abbreviation: abbrev,
		UnixTime:     time.Now().Unix(),
	}

	return output.Print(result)
}

// knownTimezones is the list of IANA timezone names from Go's time package.
var knownTimezones = []string{
	"Africa/Abidjan", "Africa/Accra", "Africa/Addis_Ababa", "Africa/Algiers",
	"Africa/Asmara", "Africa/Bamako", "Africa/Bangui", "Africa/Banjul",
	"Africa/Bissau", "Africa/Blantyre", "Africa/Brazzaville", "Africa/Bujumbura",
	"Africa/Cairo", "Africa/Casablanca", "Africa/Ceuta", "Africa/Conakry",
	"Africa/Dakar", "Africa/Dar_es_Salaam", "Africa/Djibouti", "Africa/Douala",
	"Africa/El_Aaiun", "Africa/Freetown", "Africa/Gaborone", "Africa/Harare",
	"Africa/Johannesburg", "Africa/Juba", "Africa/Kampala", "Africa/Khartoum",
	"Africa/Kigali", "Africa/Kinshasa", "Africa/Lagos", "Africa/Libreville",
	"Africa/Lome", "Africa/Luanda", "Africa/Lubumbashi", "Africa/Lusaka",
	"Africa/Malabo", "Africa/Maputo", "Africa/Maseru", "Africa/Mbabane",
	"Africa/Mogadishu", "Africa/Monrovia", "Africa/Nairobi", "Africa/Ndjamena",
	"Africa/Niamey", "Africa/Nouakchott", "Africa/Ouagadougou", "Africa/Porto-Novo",
	"Africa/Sao_Tome", "Africa/Tripoli", "Africa/Tunis", "Africa/Windhoek",
	"America/Adak", "America/Anchorage", "America/Anguilla", "America/Antigua",
	"America/Araguaina", "America/Argentina/Buenos_Aires", "America/Argentina/Catamarca",
	"America/Argentina/Cordoba", "America/Argentina/Jujuy", "America/Argentina/La_Rioja",
	"America/Argentina/Mendoza", "America/Argentina/Rio_Gallegos", "America/Argentina/Salta",
	"America/Argentina/San_Juan", "America/Argentina/San_Luis", "America/Argentina/Tucuman",
	"America/Argentina/Ushuaia", "America/Aruba", "America/Asuncion", "America/Atikokan",
	"America/Bahia", "America/Bahia_Banderas", "America/Barbados", "America/Belem",
	"America/Belize", "America/Blanc-Sablon", "America/Boa_Vista", "America/Bogota",
	"America/Boise", "America/Cambridge_Bay", "America/Campo_Grande", "America/Cancun",
	"America/Caracas", "America/Cayenne", "America/Cayman", "America/Chicago",
	"America/Chihuahua", "America/Costa_Rica", "America/Creston", "America/Cuiaba",
	"America/Curacao", "America/Danmarkshavn", "America/Dawson", "America/Dawson_Creek",
	"America/Denver", "America/Detroit", "America/Dominica", "America/Edmonton",
	"America/Eirunepe", "America/El_Salvador", "America/Fort_Nelson", "America/Fortaleza",
	"America/Glace_Bay", "America/Goose_Bay", "America/Grand_Turk", "America/Grenada",
	"America/Guadeloupe", "America/Guatemala", "America/Guayaquil", "America/Guyana",
	"America/Halifax", "America/Havana", "America/Hermosillo", "America/Indiana/Indianapolis",
	"America/Indiana/Knox", "America/Indiana/Marengo", "America/Indiana/Petersburg",
	"America/Indiana/Tell_City", "America/Indiana/Vevay", "America/Indiana/Vincennes",
	"America/Indiana/Winamac", "America/Inuvik", "America/Iqaluit", "America/Jamaica",
	"America/Juneau", "America/Kentucky/Louisville", "America/Kentucky/Monticello",
	"America/Kralendijk", "America/La_Paz", "America/Lima", "America/Los_Angeles",
	"America/Lower_Princes", "America/Maceio", "America/Managua", "America/Manaus",
	"America/Marigot", "America/Martinique", "America/Matamoros", "America/Mazatlan",
	"America/Menominee", "America/Merida", "America/Metlakatla", "America/Mexico_City",
	"America/Miquelon", "America/Moncton", "America/Monterrey", "America/Montevideo",
	"America/Montserrat", "America/Nassau", "America/New_York", "America/Nipigon",
	"America/Nome", "America/Noronha", "America/North_Dakota/Beulah",
	"America/North_Dakota/Center", "America/North_Dakota/New_Salem", "America/Nuuk",
	"America/Ojinaga", "America/Panama", "America/Pangnirtung", "America/Paramaribo",
	"America/Phoenix", "America/Port-au-Prince", "America/Port_of_Spain",
	"America/Porto_Velho", "America/Puerto_Rico", "America/Punta_Arenas",
	"America/Rainy_River", "America/Rankin_Inlet", "America/Recife", "America/Regina",
	"America/Resolute", "America/Rio_Branco", "America/Santarem", "America/Santiago",
	"America/Santo_Domingo", "America/Sao_Paulo", "America/Scoresbysund",
	"America/Sitka", "America/St_Barthelemy", "America/St_Johns", "America/St_Kitts",
	"America/St_Lucia", "America/St_Thomas", "America/St_Vincent",
	"America/Swift_Current", "America/Tegucigalpa", "America/Thule",
	"America/Thunder_Bay", "America/Tijuana", "America/Toronto", "America/Tortola",
	"America/Vancouver", "America/Whitehorse", "America/Winnipeg", "America/Yakutat",
	"America/Yellowknife",
	"Antarctica/Casey", "Antarctica/Davis", "Antarctica/DumontDUrville",
	"Antarctica/Macquarie", "Antarctica/Mawson", "Antarctica/McMurdo",
	"Antarctica/Palmer", "Antarctica/Rothera", "Antarctica/Syowa", "Antarctica/Troll",
	"Antarctica/Vostok",
	"Arctic/Longyearbyen",
	"Asia/Aden", "Asia/Almaty", "Asia/Amman", "Asia/Anadyr", "Asia/Aqtau",
	"Asia/Aqtobe", "Asia/Ashgabat", "Asia/Atyrau", "Asia/Baghdad", "Asia/Bahrain",
	"Asia/Baku", "Asia/Bangkok", "Asia/Barnaul", "Asia/Beirut", "Asia/Bishkek",
	"Asia/Brunei", "Asia/Chita", "Asia/Choibalsan", "Asia/Colombo", "Asia/Damascus",
	"Asia/Dhaka", "Asia/Dili", "Asia/Dubai", "Asia/Dushanbe", "Asia/Famagusta",
	"Asia/Gaza", "Asia/Hebron", "Asia/Ho_Chi_Minh", "Asia/Hong_Kong", "Asia/Hovd",
	"Asia/Irkutsk", "Asia/Istanbul", "Asia/Jakarta", "Asia/Jayapura", "Asia/Jerusalem",
	"Asia/Kabul", "Asia/Kamchatka", "Asia/Karachi", "Asia/Kathmandu", "Asia/Khandyga",
	"Asia/Kolkata", "Asia/Krasnoyarsk", "Asia/Kuala_Lumpur", "Asia/Kuching",
	"Asia/Kuwait", "Asia/Macau", "Asia/Magadan", "Asia/Makassar", "Asia/Manila",
	"Asia/Muscat", "Asia/Nicosia", "Asia/Novokuznetsk", "Asia/Novosibirsk",
	"Asia/Omsk", "Asia/Oral", "Asia/Phnom_Penh", "Asia/Pontianak", "Asia/Pyongyang",
	"Asia/Qatar", "Asia/Qostanay", "Asia/Qyzylorda", "Asia/Riyadh", "Asia/Sakhalin",
	"Asia/Samarkand", "Asia/Seoul", "Asia/Shanghai", "Asia/Singapore",
	"Asia/Srednekolymsk", "Asia/Taipei", "Asia/Tashkent", "Asia/Tbilisi",
	"Asia/Tehran", "Asia/Thimphu", "Asia/Tokyo", "Asia/Tomsk", "Asia/Ulaanbaatar",
	"Asia/Urumqi", "Asia/Ust-Nera", "Asia/Vientiane", "Asia/Vladivostok",
	"Asia/Yakutsk", "Asia/Yangon", "Asia/Yekaterinburg", "Asia/Yerevan",
	"Atlantic/Azores", "Atlantic/Bermuda", "Atlantic/Canary", "Atlantic/Cape_Verde",
	"Atlantic/Faroe", "Atlantic/Madeira", "Atlantic/Reykjavik", "Atlantic/South_Georgia",
	"Atlantic/St_Helena", "Atlantic/Stanley",
	"Australia/Adelaide", "Australia/Brisbane", "Australia/Broken_Hill",
	"Australia/Darwin", "Australia/Eucla", "Australia/Hobart", "Australia/Lindeman",
	"Australia/Lord_Howe", "Australia/Melbourne", "Australia/Perth", "Australia/Sydney",
	"Europe/Amsterdam", "Europe/Andorra", "Europe/Astrakhan", "Europe/Athens",
	"Europe/Belgrade", "Europe/Berlin", "Europe/Bratislava", "Europe/Brussels",
	"Europe/Bucharest", "Europe/Budapest", "Europe/Busingen", "Europe/Chisinau",
	"Europe/Copenhagen", "Europe/Dublin", "Europe/Gibraltar", "Europe/Guernsey",
	"Europe/Helsinki", "Europe/Isle_of_Man", "Europe/Istanbul", "Europe/Jersey",
	"Europe/Kaliningrad", "Europe/Kiev", "Europe/Kirov", "Europe/Lisbon",
	"Europe/Ljubljana", "Europe/London", "Europe/Luxembourg", "Europe/Madrid",
	"Europe/Malta", "Europe/Mariehamn", "Europe/Minsk", "Europe/Monaco",
	"Europe/Moscow", "Europe/Nicosia", "Europe/Oslo", "Europe/Paris",
	"Europe/Podgorica", "Europe/Prague", "Europe/Riga", "Europe/Rome",
	"Europe/Samara", "Europe/San_Marino", "Europe/Sarajevo", "Europe/Saratov",
	"Europe/Simferopol", "Europe/Skopje", "Europe/Sofia", "Europe/Stockholm",
	"Europe/Tallinn", "Europe/Tirane", "Europe/Ulyanovsk", "Europe/Uzhgorod",
	"Europe/Vaduz", "Europe/Vatican", "Europe/Vienna", "Europe/Vilnius",
	"Europe/Volgograd", "Europe/Warsaw", "Europe/Zagreb", "Europe/Zaporozhye",
	"Europe/Zurich",
	"Indian/Antananarivo", "Indian/Chagos", "Indian/Christmas", "Indian/Cocos",
	"Indian/Comoro", "Indian/Kerguelen", "Indian/Mahe", "Indian/Maldives",
	"Indian/Mauritius", "Indian/Mayotte", "Indian/Reunion",
	"Pacific/Apia", "Pacific/Auckland", "Pacific/Bougainville", "Pacific/Chatham",
	"Pacific/Chuuk", "Pacific/Easter", "Pacific/Efate", "Pacific/Enderbury",
	"Pacific/Fakaofo", "Pacific/Fiji", "Pacific/Funafuti", "Pacific/Galapagos",
	"Pacific/Gambier", "Pacific/Guadalcanal", "Pacific/Guam", "Pacific/Honolulu",
	"Pacific/Kiritimati", "Pacific/Kosrae", "Pacific/Kwajalein", "Pacific/Majuro",
	"Pacific/Marquesas", "Pacific/Midway", "Pacific/Nauru", "Pacific/Niue",
	"Pacific/Norfolk", "Pacific/Noumea", "Pacific/Pago_Pago", "Pacific/Palau",
	"Pacific/Pitcairn", "Pacific/Pohnpei", "Pacific/Port_Moresby",
	"Pacific/Rarotonga", "Pacific/Saipan", "Pacific/Tahiti", "Pacific/Tarawa",
	"Pacific/Tongatapu", "Pacific/Wake", "Pacific/Wallis",
	"UTC",
}

func listTimezones() error {
	// Group by region for LLM-friendly output
	regions := make(map[string][]string)
	for _, tz := range knownTimezones {
		parts := strings.SplitN(tz, "/", 2)
		region := parts[0]
		regions[region] = append(regions[region], tz)
	}

	// Sort regions for consistent output
	for region := range regions {
		sort.Strings(regions[region])
	}

	type TimezoneList struct {
		Total   int                 `json:"total"`
		Regions map[string][]string `json:"regions"`
	}

	return output.Print(TimezoneList{
		Total:   len(knownTimezones),
		Regions: regions,
	})
}
