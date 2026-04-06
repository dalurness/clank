package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── HTTP client (http.*) ──

func (vm *VM) builtinHttpRequest(method string) error {
	var bodyStr string
	if method == "POST" || method == "PUT" {
		b, err := vm.popStr()
		if err != nil {
			return err
		}
		bodyStr = b
	}
	url, err := vm.popStr()
	if err != nil {
		return err
	}

	var body io.Reader
	if bodyStr != "" {
		body = strings.NewReader(bodyStr)
	}
	req, reqErr := http.NewRequestWithContext(vm.ctx, method, url, body)
	if reqErr != nil {
		return vm.trap("E904", fmt.Sprintf("http.%s: %v", strings.ToLower(method), reqErr))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	timeoutMs := 30000
	root := vm.root()
	root.mu.Lock()
	if root.httpTimeout > 0 {
		timeoutMs = root.httpTimeout
	}
	root.mu.Unlock()
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	resp, doErr := client.Do(req)
	if doErr != nil {
		if errors.Is(doErr, context.Canceled) {
			return vm.trap("E011", "task cancelled")
		}
		return vm.trap("E904", fmt.Sprintf("http.%s: %v", strings.ToLower(method), doErr))
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return vm.trap("E904", fmt.Sprintf("http.%s: reading response: %v", strings.ToLower(method), readErr))
	}

	// Build header list as [(Str, Str)]
	var headers []Value
	for k, vals := range resp.Header {
		for _, v := range vals {
			headers = append(headers, ValTuple([]Value{ValStr(k), ValStr(v)}))
		}
	}

	fields := map[string]Value{
		"status":  ValInt(resp.StatusCode),
		"body":    ValStr(string(respBody)),
		"headers": ValList(headers),
	}
	vm.push(ValRecord(fields, []string{"status", "body", "headers"}))
	return nil
}

// ── Sleep (async-aware) ──

func (vm *VM) builtinSleep() error {
	ms, err := vm.pop()
	if err != nil {
		return err
	}
	duration := 0
	if ms.Tag == TagINT {
		duration = ms.IntVal
	} else if ms.Tag == TagRAT {
		duration = int(ms.RatVal)
	}
	if duration > 0 {
		timer := time.NewTimer(time.Duration(duration) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			// normal completion
		case <-vm.ctx.Done():
			return vm.trap("E011", "task cancelled")
		}
	}
	vm.push(ValUnit())
	return nil
}
