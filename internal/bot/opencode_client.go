package bot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type OpencodeClient struct {
	base  *url.URL
	token string
	http  *http.Client
}

func NewOpencodeClient(baseURL, token string) (*OpencodeClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	return &OpencodeClient{base: u, token: token, http: &http.Client{}}, nil
}

func (c *OpencodeClient) doRequest(method, p string, body any) ([]byte, error) {
	// build URL
	u := *c.base
	u.Path = path.Join(c.base.Path, p)

	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, u.String(), buf)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("opencode error: %d %s", resp.StatusCode, string(b))
	}
	return b, nil
}

func (c *OpencodeClient) ListSessions() ([]map[string]any, error) {
	b, err := c.doRequest("GET", "/session", nil)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *OpencodeClient) CreateSession(title string) (map[string]any, error) {
	body := map[string]any{"title": title}
	b, err := c.doRequest("POST", "/session", body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *OpencodeClient) PromptSession(sessionID, text string) (map[string]any, error) {
	body := map[string]any{"parts": []map[string]any{{"type": "text", "text": text}}}
	p := fmt.Sprintf("/session/%s/message", sessionID)
	b, err := c.doRequest("POST", p, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *OpencodeClient) AbortSession(sessionID string) error {
	p := fmt.Sprintf("/session/%s/abort", sessionID)
	_, err := c.doRequest("POST", p, nil)
	return err
}

// DeleteSession deletes a session by ID.
func (c *OpencodeClient) DeleteSession(sessionID string) error {
	p := fmt.Sprintf("/session/%s", sessionID)
	_, err := c.doRequest("DELETE", p, nil)
	return err
}

// SubscribeEvents connects to the Opencode SSE endpoint (/event) and calls
// handler for each parsed event payload. This runs until the connection
// breaks; caller may run it in a goroutine.
func (c *OpencodeClient) SubscribeEvents(handler func(map[string]any)) error {
	// build URL
	u := *c.base
	u.Path = path.Join(c.base.Path, "/event")

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}

	// parse SSE using a buffered reader, handling multiple "data:" lines per event
	go func() {
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		var dataLines []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return
				}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				// event delimiter — join data lines
				if len(dataLines) > 0 {
					payload := strings.Join(dataLines, "\n")
					var ev map[string]any
					if err := json.Unmarshal([]byte(payload), &ev); err == nil {
						handler(ev)
					} else {
						// try to parse as generic JSON array or other; ignore parse errors silently
					}
				}
				dataLines = dataLines[:0]
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(line[len("data:"):])
				dataLines = append(dataLines, data)
			}
			// ignore other SSE fields (id:, event:, retry:)
		}
	}()
	return nil
}

// GetSessionMessages fetches messages for a session and concatenates text parts,
// filtering out thinking parts to return only the final output.
func (c *OpencodeClient) GetSessionMessages(sessionID string) (string, error) {
	p := fmt.Sprintf("/session/%s/message", sessionID)
	b, err := c.doRequest("GET", p, nil)
	if err != nil {
		return "", err
	}
	// The response is typically an array of { info, parts }
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return "", err
	}
	// Collect the last non-thinking text part and return it as the final output.
	// If no non-thinking part exists, fall back to the most recent thinking part.
	var lastNonThinking string
	var lastThinking string
	for _, item := range arr {
		if parts, ok := item["parts"]; ok {
			if ps, ok := parts.([]any); ok {
				for _, p := range ps {
					if pm, ok := p.(map[string]any); ok {
						// extract text if present
						var text string
						if t, ok := pm["text"]; ok {
							text = fmt.Sprintf("%v", t)
						}

						// determine type (if present)
						if partTypeRaw, ok := pm["type"]; ok {
							if partType, ok := partTypeRaw.(string); ok {
								if strings.EqualFold(partType, "thinking") {
									if text != "" {
										lastThinking = text
									}
									continue
								}
							}
						}

						if text != "" {
							lastNonThinking = text
						}
					}
				}
			}
		}
	}

	if lastNonThinking != "" {
		return lastNonThinking, nil
	}
	return lastThinking, nil
}
