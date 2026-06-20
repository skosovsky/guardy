// Custom validator: call an external moderation API and plug into the pipeline.
// This example uses a mock HTTP server that returns a simple "toxic" flag.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/skosovsky/guardy"
)

// ModerationValidator calls a moderation API and returns Block when flagged.
type ModerationValidator struct {
	client  *http.Client
	baseURL string
}

func NewModerationValidator(baseURL string) *ModerationValidator {
	return &ModerationValidator{
		client:  http.DefaultClient,
		baseURL: strings.TrimSuffix(baseURL, "/"),
	}
}

func (m *ModerationValidator) Name() string { return "moderation" }

func (m *ModerationValidator) Validate(ctx context.Context, text string) (string, *guardy.Report, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/moderate", strings.NewReader(text))
	if err != nil {
		return text, nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	// #nosec G704 -- baseURL is trusted (e.g. from config or httptest in examples)
	resp, err := m.client.Do(req)
	if err != nil {
		return text, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return text, nil, fmt.Errorf("moderation API returned %d", resp.StatusCode)
	}
	var out struct {
		Toxic bool `json:"toxic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return text, nil, err
	}
	if out.Toxic {
		return text, &guardy.Report{
			Action:    guardy.ActionBlock,
			Validator: m.Name(),
			Reason:    "Content flagged by moderation API",
		}, nil
	}
	return text, &guardy.Report{Action: guardy.ActionPass, Validator: m.Name()}, nil
}

func main() {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/moderate" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		toxic := strings.Contains(strings.ToLower(string(body)), "badword")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"toxic": toxic})
	}))
	defer mock.Close()

	v := NewModerationValidator(mock.URL)
	pipeline := guardy.NewPipeline(guardy.WithFastPath(v))
	ctx := context.Background()

	for _, text := range []string{"Hello world", "This has badword in it"} {
		result, err := pipeline.Run(ctx, nil, text)
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}
		decision := result.PolicyDecision()
		if decision.IsTerminal() {
			fmt.Printf("Blocked: %q -> %s\n", text, decision.Validator)
		} else {
			fmt.Printf("Pass: %q\n", text)
		}
	}
}
