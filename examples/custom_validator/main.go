// Custom validator: call an external moderation API (e.g. OpenAI Moderation) and plug into the pipeline.
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

// ModerationValidator calls a moderation API and returns Block with Code "TOXIC" when flagged.
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

func (m *ModerationValidator) Validate(ctx context.Context, input *guardy.Input) (guardy.Result, error) {
	data := ""
	if input != nil {
		data = input.Data
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/moderate", strings.NewReader(data))
	if err != nil {
		return guardy.Result{}, err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := m.client.Do(req)
	if err != nil {
		return guardy.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return guardy.Result{}, fmt.Errorf("moderation API returned %d", resp.StatusCode)
	}
	var out struct {
		Toxic bool `json:"toxic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return guardy.Result{}, err
	}
	if out.Toxic {
		return guardy.Result{
			Passed: false,
			Action: guardy.Block,
			Code:   "TOXIC",
			Reason: "Content flagged by moderation API",
		}, nil
	}
	return guardy.Result{Passed: true, Action: guardy.Pass}, nil
}

func (m *ModerationValidator) Name() string { return "moderation" }

func main() {
	// Mock moderation API: flags text containing "badword".
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
	pipeline := guardy.NewPipeline(guardy.WithTier1(v))
	ctx := context.Background()

	for _, text := range []string{"Hello world", "This has badword in it"} {
		report, err := pipeline.Run(ctx, &guardy.Input{Data: text})
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}
		if report.FinalAction == guardy.Block {
			fmt.Printf("Blocked: %q -> %s\n", text, report.Results[0].Code)
		} else {
			fmt.Printf("Pass: %q\n", text)
		}
	}
}
