package dotloop

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

const apiBaseURL = "https://api.dotloop.com/public/v2"

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dotloop",
		Short: "DotLoop transaction management commands",
		Long:  "DotLoop integration for loops (transactions), profiles, tasks, and documents.",
	}

	cmd.AddCommand(newLoopsCmd())
	cmd.AddCommand(newLoopCmd())
	cmd.AddCommand(newProfilesCmd())
	cmd.AddCommand(newTasksCmd())
	cmd.AddCommand(newDocumentsCmd())

	return cmd
}

type dotloopClient struct {
	token      string
	companyID  string
	httpClient *http.Client
}

func newDotloopClient() (*dotloopClient, error) {
	token, err := config.MustGet("dotloop_token")
	if err != nil {
		return nil, err
	}

	companyID, _ := config.Get("dotloop_company_id")

	return &dotloopClient{
		token:      token,
		companyID:  companyID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *dotloopClient) doRequest(method, endpoint string, body interface{}) ([]byte, error) {
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

	reqURL := apiBaseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
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
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if json.Unmarshal(respBody, &errResp) == nil {
			if errResp.Message != "" {
				return nil, fmt.Errorf("DotLoop API error: %s", errResp.Message)
			}
			if errResp.Error != "" {
				return nil, fmt.Errorf("DotLoop API error: %s", errResp.Error)
			}
			if len(errResp.Errors) > 0 && errResp.Errors[0].Message != "" {
				return nil, fmt.Errorf("DotLoop API error: %s", errResp.Errors[0].Message)
			}
		}
		return nil, fmt.Errorf("DotLoop API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// Loop represents a DotLoop transaction
type Loop struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	ViewCount   int    `json:"view_count"`
	CreatedBy   string `json:"created_by"`
	CreatedDate string `json:"created_date"`
	UpdatedDate string `json:"updated_date"`
}

// Profile represents a DotLoop profile
type Profile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

// Task represents a DotLoop task
type Task struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	AssignedTo string `json:"assigned_to"`
	DueDate    string `json:"due_date,omitempty"`
}

// Document represents a DotLoop document
type Document struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	CreatedDate string `json:"created_date"`
}

func newLoopsCmd() *cobra.Command {
	var limit int
	var status string

	cmd := &cobra.Command{
		Use:   "loops",
		Short: "List loops (transactions)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newDotloopClient()
			if err != nil {
				return err
			}

			endpoint := "/loops"
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
				Loops []Loop `json:"data"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"count": len(result.Loops),
				"loops": result.Loops,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of results")
	cmd.Flags().StringVarP(&status, "status", "s", "", "Filter by status")

	return cmd
}

func newLoopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loop [id]",
		Short: "Get loop details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newDotloopClient()
			if err != nil {
				return err
			}

			body, err := client.doRequest("GET", "/loops/"+args[0], nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var result struct {
				Loop Loop `json:"data"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(result.Loop)
		},
	}

	return cmd
}

func newProfilesCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newDotloopClient()
			if err != nil {
				return err
			}

			endpoint := "/profiles"
			if limit > 0 {
				endpoint += "?limit=" + fmt.Sprint(limit)
			}

			body, err := client.doRequest("GET", endpoint, nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var result struct {
				Profiles []Profile `json:"data"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"count":    len(result.Profiles),
				"profiles": result.Profiles,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of results")

	return cmd
}

func newTasksCmd() *cobra.Command {
	var limit int
	var status string

	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "List tasks across loops",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newDotloopClient()
			if err != nil {
				return err
			}

			endpoint := "/tasks"
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
				Tasks []Task `json:"data"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"count": len(result.Tasks),
				"tasks": result.Tasks,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of results")
	cmd.Flags().StringVarP(&status, "status", "s", "", "Filter by status")

	return cmd
}

func newDocumentsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "documents [loop-id]",
		Short: "List documents in a loop",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newDotloopClient()
			if err != nil {
				return err
			}

			endpoint := "/loops/" + args[0] + "/documents"
			if limit > 0 {
				endpoint += "?limit=" + fmt.Sprint(limit)
			}

			body, err := client.doRequest("GET", endpoint, nil)
			if err != nil {
				return output.PrintError("request_failed", err.Error(), nil)
			}

			var result struct {
				Documents []Document `json:"data"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return output.PrintError("parse_error", err.Error(), nil)
			}

			return output.Print(map[string]any{
				"loop_id":   args[0],
				"count":     len(result.Documents),
				"documents": result.Documents,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 50, "Number of results")

	return cmd
}
