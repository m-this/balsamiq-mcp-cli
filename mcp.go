package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
)

const protocolVersion = "2025-06-18"

type mcpClient struct {
	url       string
	token     string
	sessionID string
	nextID    atomic.Int64
	http      *http.Client
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     *int64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func dial() (*mcpClient, error) {
	token, err := accessToken()
	if err != nil {
		return nil, err
	}
	c := &mcpClient{url: mcpURL(), token: token, http: http.DefaultClient}
	var initResult json.RawMessage
	initResult, err = c.roundTrip("initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "bais-cli", "version": "0.1.0"},
	}, true)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	_ = initResult
	if err := c.notify("notifications/initialized"); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *mcpClient) call(method string, params any) (json.RawMessage, error) {
	return c.roundTrip(method, params, false)
}

func (c *mcpClient) notify(method string) error {
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method})
	resp, err := c.post(body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *mcpClient) roundTrip(method string, params any, isInit bool) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	resp, err := c.post(body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if isInit {
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.sessionID = sid
		}
	}
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(buf.String()))
	}

	var rr rpcResponse
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		raw, err := readSSEResponse(resp, id)
		if err != nil {
			return nil, err
		}
		rr = *raw
	} else {
		if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
			return nil, err
		}
	}
	if rr.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", rr.Error.Code, rr.Error.Message)
	}
	return rr.Result, nil
}

func (c *mcpClient) post(body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	return c.http.Do(req)
}

func readSSEResponse(resp *http.Response, wantID int64) (*rpcResponse, error) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var data strings.Builder
	flush := func() (*rpcResponse, bool) {
		if data.Len() == 0 {
			return nil, false
		}
		var rr rpcResponse
		payload := data.String()
		data.Reset()
		if err := json.Unmarshal([]byte(payload), &rr); err != nil {
			return nil, false
		}
		if rr.ID != nil && *rr.ID == wantID {
			return &rr, true
		}
		return nil, false
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "":
			if rr, ok := flush(); ok {
				return rr, nil
			}
		}
	}
	if rr, ok := flush(); ok {
		return rr, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no response for request id %d in event stream", wantID)
}
