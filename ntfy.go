package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// sentTag marks our "leave now" pushes so later runs can see one went out today.
const sentTag = "kita-leave"

type Message struct {
	Title    string
	Body     string
	Priority string // min, low, default, high, urgent
	Tags     []string
	Click    string
}

func publish(ctx context.Context, c *http.Client, cfg Config, m Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.NtfyServer+"/"+cfg.NtfyTopic, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", m.Title)
	req.Header.Set("Priority", m.Priority)
	req.Header.Set("Tags", strings.Join(m.Tags, ","))
	if m.Click != "" {
		req.Header.Set("Click", m.Click)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ntfy publish: HTTP %d", resp.StatusCode)
	}
	return nil
}

// alreadySent asks ntfy's message cache (12h on ntfy.sh) whether a push with
// sentTag went out since `since`. ntfy is the only state this job keeps.
func alreadySent(ctx context.Context, c *http.Client, cfg Config, since time.Time) (bool, error) {
	u := fmt.Sprintf("%s/%s/json?poll=1&since=%d", cfg.NtfyServer, cfg.NtfyTopic, since.Unix())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ntfy poll: HTTP %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var ev struct {
			Event string   `json:"event"`
			Tags  []string `json:"tags"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Event == "message" && slices.Contains(ev.Tags, sentTag) {
			return true, nil
		}
	}
	return false, sc.Err()
}
