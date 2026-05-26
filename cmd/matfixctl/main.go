package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ilamparithi-in/matfix/internal/version"
	"github.com/spf13/cobra"
)

// # Global state

var socketPath string

// # HTTP client

// newClient returns an http.Client whose transport dials the admin UNIX socket.
// The request URL host is ignored; all connections go to socketPath.
func newClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 30 * time.Second,
	}
}

// adminBase is the fake base URL used for UNIX socket HTTP requests.
// The host value is arbitrary; the transport ignores it.
const adminBase = "http://matfix-admin"

// # Response types

type keyCreatedResp struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

type keyPermissions struct {
	Accounts     []string `json:"accounts,omitempty"`
	Routes       []string `json:"routes,omitempty"`
	Rooms        []string `json:"rooms,omitempty"`
	EventTypes   []string `json:"event_types,omitempty"`
	RateLimitRPS int      `json:"rate_limit_rps,omitempty"`
}

type keyEntry struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Permissions *keyPermissions `json:"permissions,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	RevokedAt   *int64          `json:"revoked_at,omitempty"`
}

type listKeysResp struct {
	Keys []keyEntry `json:"keys"`
}

type accountEntry struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type accountsResp struct {
	Accounts []accountEntry `json:"accounts"`
}

type queueResp struct {
	Queued     int `json:"queued"`
	Sending    int `json:"sending"`
	Failed     int `json:"failed"`
	DeadLetter int `json:"dead_letter"`
}

type subscriptionsResp struct {
	Asks     int `json:"asks"`
	Receives int `json:"receives"`
}

// # Helpers

// fmtTime converts a Unix millisecond timestamp to a UTC RFC3339 string.
func fmtTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// checkError returns a descriptive error if resp carries a non-2xx status.
func checkError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e.Error != "" {
		return fmt.Errorf("%s (%s)", e.Error, resp.Status)
	}
	return fmt.Errorf("request failed: %s", resp.Status)
}

// doJSON sends method to url with a JSON-encoded body and returns the response.
func doJSON(method, url string, body any) (*http.Response, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return newClient().Do(req)
}

// doGet sends a GET request to url and returns the response.
func doGet(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return newClient().Do(req)
}

// doPost sends a POST request with no body.
func doPost(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	return newClient().Do(req)
}

// doDelete sends a DELETE request.
func doDelete(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}
	return newClient().Do(req)
}

// nilIfEmpty returns nil when s is empty, enabling JSON omitempty on slices.
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// # Entry point

func main() {
	root := &cobra.Command{
		Use:           "matfixctl",
		Short:         "Admin CLI for the matfix relay daemon",
		Long:          "matfixctl communicates with the matfix daemon over a UNIX domain socket.",
		Version:       version.Full(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	root.PersistentFlags().StringVar(&socketPath, "socket", "/run/matfix/admin.sock",
		"path to the matfix admin UNIX socket")

	root.AddCommand(keysCmd(), accountsCmd(), queueCmd(), subscriptionsCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// # keys

func keysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage API keys",
	}
	cmd.AddCommand(keysCreateCmd(), keysListCmd(), keysRevokeCmd(), keysRotateCmd())
	return cmd
}

func keysCreateCmd() *cobra.Command {
	var (
		name       string
		accounts   []string
		routes     []string
		rooms      []string
		eventTypes []string
		rateLimit  int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			var perms *keyPermissions
			if len(accounts)+len(routes)+len(rooms)+len(eventTypes)+rateLimit > 0 {
				perms = &keyPermissions{
					Accounts:     nilIfEmpty(accounts),
					Routes:       nilIfEmpty(routes),
					Rooms:        nilIfEmpty(rooms),
					EventTypes:   nilIfEmpty(eventTypes),
					RateLimitRPS: rateLimit,
				}
			}

			body := struct {
				Name        string          `json:"name"`
				Permissions *keyPermissions `json:"permissions,omitempty"`
			}{Name: name, Permissions: perms}

			resp, err := doJSON(http.MethodPost, adminBase+"/keys", body)
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer resp.Body.Close()
			if err := checkError(resp); err != nil {
				return err
			}

			var result keyCreatedResp
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}

			fmt.Printf("Key ID:     %s\n", result.ID)
			fmt.Printf("Name:       %s\n", result.Name)
			fmt.Printf("Key:        %s\n", result.Key)
			fmt.Printf("            (save this - it will not be shown again)\n")
			fmt.Printf("Created At: %s\n", fmtTime(result.CreatedAt))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "key name (required)")
	cmd.Flags().StringSliceVar(&accounts, "accounts", nil,
		"account IDs this key may act on (default: any)")
	cmd.Flags().StringSliceVar(&routes, "routes", nil,
		"routes this key may access: send,receive,ask,admin (default: any)")
	cmd.Flags().StringSliceVar(&rooms, "rooms", nil,
		"room IDs this key may target (default: any)")
	cmd.Flags().StringSliceVar(&eventTypes, "event-types", nil,
		"inbound event types this key may subscribe to (default: any)")
	cmd.Flags().IntVar(&rateLimit, "rate-limit", 0,
		"per-key rate limit in requests/sec (0 = server default)")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func keysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := doGet(adminBase + "/keys")
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer resp.Body.Close()
			if err := checkError(resp); err != nil {
				return err
			}

			var result listKeysResp
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}

			if len(result.Keys) == 0 {
				fmt.Println("No API keys.")
				return nil
			}

			fmt.Printf("%-36s  %-20s  %-20s  %-20s  %s\n",
				"ID", "Name", "Created At", "Revoked At", "Routes")
			fmt.Println(strings.Repeat("-", 115))
			for _, k := range result.Keys {
				revoked := "-"
				if k.RevokedAt != nil {
					revoked = fmtTime(*k.RevokedAt)
				}
				routes := "any"
				if k.Permissions != nil && len(k.Permissions.Routes) > 0 {
					routes = strings.Join(k.Permissions.Routes, ",")
				}
				fmt.Printf("%-36s  %-20s  %-20s  %-20s  %s\n",
					k.ID, k.Name, fmtTime(k.CreatedAt), revoked, routes)
			}
			return nil
		},
	}
}

func keysRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := doDelete(adminBase + "/keys/" + args[0])
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer resp.Body.Close()
			if err := checkError(resp); err != nil {
				return err
			}
			fmt.Printf("Key %s revoked.\n", args[0])
			return nil
		},
	}
}

func keysRotateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate <id>",
		Short: "Revoke a key and issue a replacement with the same name and permissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := doPost(adminBase + "/keys/" + args[0] + "/rotate")
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer resp.Body.Close()
			if err := checkError(resp); err != nil {
				return err
			}

			var result keyCreatedResp
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}

			fmt.Printf("Old key %s revoked.\n\n", args[0])
			fmt.Printf("New Key ID: %s\n", result.ID)
			fmt.Printf("Name:       %s\n", result.Name)
			fmt.Printf("Key:        %s\n", result.Key)
			fmt.Printf("            (save this - it will not be shown again)\n")
			fmt.Printf("Created At: %s\n", fmtTime(result.CreatedAt))
			return nil
		},
	}
}

// # accounts

func accountsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accounts",
		Short: "Show account availability status",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := doGet(adminBase + "/accounts")
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer resp.Body.Close()
			if err := checkError(resp); err != nil {
				return err
			}

			var result accountsResp
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}

			if len(result.Accounts) == 0 {
				fmt.Println("No accounts configured.")
				return nil
			}

			fmt.Printf("%-40s  %-12s  %s\n", "ID", "Status", "Error")
			fmt.Println(strings.Repeat("-", 80))
			for _, a := range result.Accounts {
				status := "available"
				if !a.Available {
					status = "failed"
				}
				fmt.Printf("%-40s  %-12s  %s\n", a.ID, status, a.Error)
			}
			return nil
		},
	}
}

// # queue

func queueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "Show outbound queue depth",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := doGet(adminBase + "/queue")
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer resp.Body.Close()
			if err := checkError(resp); err != nil {
				return err
			}

			var result queueResp
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}

			fmt.Printf("Queued:      %d\n", result.Queued)
			fmt.Printf("Sending:     %d\n", result.Sending)
			fmt.Printf("Failed:      %d\n", result.Failed)
			fmt.Printf("Dead Letter: %d\n", result.DeadLetter)
			return nil
		},
	}
}

// # subscriptions

func subscriptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "subscriptions",
		Short: "Show active ask and receive subscriptions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := doGet(adminBase + "/subscriptions")
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer resp.Body.Close()
			if err := checkError(resp); err != nil {
				return err
			}

			var result subscriptionsResp
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}

			fmt.Printf("Asks:     %d\n", result.Asks)
			fmt.Printf("Receives: %d\n", result.Receives)
			return nil
		},
	}
}
