package followupboss

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/unstablemind/pocket/internal/common/config"
	"github.com/unstablemind/pocket/pkg/output"
)

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "followupboss",
		Aliases: []string{"fub"},
		Short:   "Follow Up Boss CRM commands",
		Long:    "Follow Up Boss CRM integration for contacts, leads, tasks, and events.",
	}

	cmd.AddCommand(newContactsCmd())
	cmd.AddCommand(newContactCmd())
	cmd.AddCommand(newLeadsCmd())
	cmd.AddCommand(newTasksCmd())
	cmd.AddCommand(newEventsCmd())

	return cmd
}

type fubClient struct {
	apiKey     string
	baseURL    string
	systemKey  string
	systemName string
	httpClient *http.Client
}

func newFUBClient() (*fubClient, error) {
	apiKey, err := config.MustGet("fub_api_key")
	if err != nil {
		return nil, err
	}

	systemKey, err := config.MustGet("fub_system_key")
	if err != nil {
		return nil, err
	}

	systemName, err := config.MustGet("fub_system_name")
	if err != nil {
		return nil, err
	}

	baseURL, _ := config.Get("fub_base_url")
	if baseURL == "" {
		baseURL = "https://api.followupboss.com/v1"
	}

	return &fubClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		systemKey:  systemKey,
		systemName: systemName,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *fubClient) doRequest(method, endpoint string, body interface{}) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	reqURL := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-System-Key", c.systemKey)
	req.Header.Set("X-System-Name", c.systemName)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil {
			if errResp.Message != "" {
				return nil, fmt.Errorf("FUB API error: %s", errResp.Message)
			}
			if errResp.Error != "" {
				return nil, fmt.Errorf("FUB API error: %s", errResp.Error)
			}
		}
		return nil, fmt.Errorf("FUB API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// Contact represents a Follow Up Boss contact
type Contact struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Phone     string   `json:"phone"`
	Status    string   `json:"status"`
	Source    string   `json:"source"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// Lead represents a Follow Up Boss lead/opportunity
type Lead struct {
	ID         string `json:"id"`
	ContactID  string `json:"contact_id"`
	Status     string `json:"status"`
	Stage      string `json:"stage"`
	Price      int64  `json:"price"`
	Address    string `json:"address,omitempty"`
	AssignedTo string `json:"assigned_to"`
	CreatedAt  string `json:"created_at"`
}

// Task represents a Follow Up Boss task
type Task struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	DueDate    string `json:"due_date"`
	Completed  bool   `json:"completed"`
	AssignedTo string `json:"assigned_to"`
	Priority   string `json:"priority"`
}

// Event represents a Follow Up Boss event/appointment
type Event struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	Location  string   `json:"location,omitempty"`
	Attendees []string `json:"attendees,omitempty"`
}

func newContactsCmd() *cobra.Command {
	var limit int
	var status string
	var search string

	cmd := &cobra.Command{
		Use:   "contacts",
		Short: "List contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newFUBClient()
			if err != nil {
				return err
			}

			endpoint := "/contacts"
			queryParams := ""
			if limit > 0 {
				if queryParams != "" {
					queryParams += "&"
				}
				queryParams += "limit=" + fmt.Sprint(limit)
			}
			if status != "" {
				if queryParams != "" {
					queryParams += "&"
				}
				queryParams += "status=" + status
			}
			if search != "" {
				if queryParams != "" {
					queryParams += "&"
				}
				queryParams += "q=" + search
			}
			if queryParams != "" {
				endpoint += "?" + queryParams
			}

			body, err := client.doRequest("GET", endpoint, nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var result struct {
				Contacts []Contact `json:"contacts"`
				Total    int       `json:"total"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"count":    len(result.Contacts),
				"total":    result.Total,
				"contacts": result.Contacts,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of results")
	cmd.Flags().StringVarP(&status, "status", "s", "", "Filter by status")
	cmd.Flags().StringVarP(&search, "search", "q", "", "Search query")

	return cmd
}

func newContactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contact [id]",
		Short: "Get contact details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newFUBClient()
			if err != nil {
				return err
			}

			body, err := client.doRequest("GET", "/contacts/"+args[0], nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var contact Contact
			if err := json.Unmarshal(body, &contact); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(contact)
		},
	}

	return cmd
}

func newLeadsCmd() *cobra.Command {
	var limit int
	var status string

	cmd := &cobra.Command{
		Use:   "leads",
		Short: "List leads/opportunities",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newFUBClient()
			if err != nil {
				return err
			}

			endpoint := "/opportunities"
			queryParams := ""
			if limit > 0 {
				queryParams = "limit=" + fmt.Sprint(limit)
			}
			if status != "" {
				if queryParams != "" {
					queryParams += "&"
				}
				queryParams += "status=" + status
			}
			if queryParams != "" {
				endpoint += "?" + queryParams
			}

			body, err := client.doRequest("GET", endpoint, nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var result struct {
				Opportunities []Lead `json:"opportunities"`
				Total         int    `json:"total"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"count": len(result.Opportunities),
				"total": result.Total,
				"leads": result.Opportunities,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of results")
	cmd.Flags().StringVarP(&status, "status", "s", "", "Filter by status")

	return cmd
}

func newTasksCmd() *cobra.Command {
	var limit int
	var completed string

	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "List tasks/reminders",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newFUBClient()
			if err != nil {
				return err
			}

			endpoint := "/tasks"
			queryParams := ""
			if limit > 0 {
				queryParams = "limit=" + fmt.Sprint(limit)
			}
			if completed != "" {
				if queryParams != "" {
					queryParams += "&"
				}
				queryParams += "completed=" + completed
			}
			if queryParams != "" {
				endpoint += "?" + queryParams
			}

			body, err := client.doRequest("GET", endpoint, nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var result struct {
				Tasks []Task `json:"tasks"`
				Total int    `json:"total"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"count": len(result.Tasks),
				"total": result.Total,
				"tasks": result.Tasks,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of results")
	cmd.Flags().StringVarP(&completed, "completed", "c", "", "Filter by completed (true/false)")

	return cmd
}

func newEventsCmd() *cobra.Command {
	var limit int
	var startDate string
	var endDate string

	cmd := &cobra.Command{
		Use:   "events",
		Short: "List events/appointments",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newFUBClient()
			if err != nil {
				return err
			}

			endpoint := "/events"
			queryParams := ""
			if limit > 0 {
				queryParams = "limit=" + fmt.Sprint(limit)
			}
			if startDate != "" {
				if queryParams != "" {
					queryParams += "&"
				}
				queryParams += "start_date=" + startDate
			}
			if endDate != "" {
				if queryParams != "" {
					queryParams += "&"
				}
				queryParams += "end_date=" + endDate
			}
			if queryParams != "" {
				endpoint += "?" + queryParams
			}

			body, err := client.doRequest("GET", endpoint, nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var result struct {
				Events []Event `json:"events"`
				Total  int     `json:"total"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"count":  len(result.Events),
				"total":  result.Total,
				"events": result.Events,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of results")
	cmd.Flags().StringVarP(&startDate, "start", "s", "", "Start date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&endDate, "end", "e", "", "End date (YYYY-MM-DD)")

	return cmd
}
