package vm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// ── Streaming I/O ──
// These return lazy iterators that yield lines one at a time,
// enabling memory-efficient processing of large files and streams.

// fs.stream-lines(path) -> Iterator[Str]
func (vm *VM) builtinFsStreamLines() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	f, openErr := os.Open(path)
	if openErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.stream-lines: %v", openErr))
	}
	scanner := bufio.NewScanner(f)
	done := false
	vm.push(vm.newIter(func() *Value {
		if done {
			return nil
		}
		if scanner.Scan() {
			v := ValStr(scanner.Text())
			return &v
		}
		done = true
		f.Close()
		return nil
	}))
	return nil
}

// http.stream-lines(url) -> Iterator[Str]
func (vm *VM) builtinHttpStreamLines() error {
	url, err := vm.popStr()
	if err != nil {
		return err
	}
	req, reqErr := http.NewRequestWithContext(vm.ctx, "GET", url, nil)
	if reqErr != nil {
		return vm.trap("E904", fmt.Sprintf("http.stream-lines: %v", reqErr))
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
		return vm.trap("E904", fmt.Sprintf("http.stream-lines: %v", doErr))
	}
	scanner := bufio.NewScanner(resp.Body)
	done := false
	vm.push(vm.newIter(func() *Value {
		if done {
			return nil
		}
		if scanner.Scan() {
			v := ValStr(scanner.Text())
			return &v
		}
		done = true
		resp.Body.Close()
		return nil
	}))
	return nil
}

// proc.stream(cmd) -> Iterator[Str]
// Runs a shell command and streams stdout lines as an iterator.
func (vm *VM) builtinProcStream() error {
	cmdStr, err := vm.popStr()
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(vm.ctx, "cmd", "/C", cmdStr)
	} else {
		cmd = exec.CommandContext(vm.ctx, "sh", "-c", cmdStr)
	}
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return vm.trap("E903", fmt.Sprintf("proc.stream: %v", pipeErr))
	}
	if startErr := cmd.Start(); startErr != nil {
		return vm.trap("E903", fmt.Sprintf("proc.stream: %v", startErr))
	}
	scanner := bufio.NewScanner(stdout)
	done := false
	vm.push(vm.newIter(func() *Value {
		if done {
			return nil
		}
		if scanner.Scan() {
			v := ValStr(scanner.Text())
			return &v
		}
		done = true
		cmd.Wait()
		return nil
	}))
	return nil
}

// io.stdin-lines() -> Iterator[Str]
// Reads lines from stdin as a lazy iterator.
func (vm *VM) builtinStdinLines() error {
	scanner := bufio.NewScanner(os.Stdin)
	root := vm.root()
	// Allow test override
	var reader io.Reader
	if root.testStdin != nil {
		reader = root.testStdin
		scanner = bufio.NewScanner(reader)
	}
	done := false
	vm.push(vm.newIter(func() *Value {
		if done {
			return nil
		}
		if scanner.Scan() {
			v := ValStr(scanner.Text())
			return &v
		}
		done = true
		return nil
	}))
	return nil
}

