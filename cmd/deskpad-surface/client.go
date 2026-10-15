package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"
)

// uiState mirrors the daemon's GET /api/ui/state response and the data of each /api/ui/events message. Keys are
// "data:image/png;base64,..." URLs, or null for an empty key.
type uiState struct {
	CurrentScreen struct {
		Name string `json:"name"`
	} `json:"currentScreen"`
	Grid struct {
		Rows    int `json:"rows"`
		Columns int `json:"columns"`
	} `json:"grid"`
	Keys []*string `json:"keys"`
}

const pngDataURLPrefix = "data:image/png;base64,"

// decodeKeyImage decodes a key image data URL as sent by the daemon.
func decodeKeyImage(dataURL string) (image.Image, error) {
	if !strings.HasPrefix(dataURL, pngDataURLPrefix) {
		return nil, fmt.Errorf("unsupported key image encoding")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, pngDataURLPrefix))
	if err != nil {
		return nil, fmt.Errorf("decode key image: %w", err)
	}

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode key image: %w", err)
	}

	return img, nil
}

// fetchState reads the current surface state once.
func fetchState(ctx context.Context, client *http.Client, server string) (uiState, error) {
	var state uiState

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/api/ui/state", nil)
	if err != nil {
		return state, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return state, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return state, fmt.Errorf("state request returned %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&state); err != nil {
		return state, fmt.Errorf("decode state: %w", err)
	}

	return state, nil
}

// streamStates connects to the daemon's server-sent events and calls onState for every snapshot until the
// connection ends, ctx is done, or nothing (not even a heartbeat) is received for idle. It always returns a non-nil
// error describing why it stopped.
func streamStates(ctx context.Context, client *http.Client, server string, idle time.Duration, onState func(uiState)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/api/ui/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("events request returned %s", resp.Status)
	}

	// The daemon sends a heartbeat comment every 30s, so silence means the connection is dead even if the TCP
	// connection hasn't noticed. Cancelling the request unblocks the read below.
	watchdog := time.AfterFunc(idle, cancel)
	defer watchdog.Stop()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)

	var data []string
	for scanner.Scan() {
		watchdog.Reset(idle)

		line := scanner.Text()
		switch {
		case line == "":
			if len(data) == 0 {
				continue
			}

			var state uiState
			if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &state); err != nil {
				return fmt.Errorf("decode event: %w", err)
			}
			data = data[:0]
			onState(state)

		case strings.HasPrefix(line, ":"):
			// comment, i.e. a heartbeat

		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}

	if ctx.Err() != nil {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("no data for %s: %w", idle, err)
		}
		return fmt.Errorf("stream stopped: %w", ctx.Err())
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

// pressKey reports a key press to the daemon. kind is "short" or "long".
func pressKey(ctx context.Context, client *http.Client, server, token string, key int, kind string) error {
	body, err := json.Marshal(map[string]string{"type": kind})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/ui/keys/%d/press", server, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("press returned %s", resp.Status)
	}

	return nil
}
