package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ── Process execution (proc.*) ──

func (vm *VM) builtinProcRun() error {
	argList, err := vm.popList()
	if err != nil {
		return err
	}
	cmdName, err := vm.popStr()
	if err != nil {
		return err
	}
	args := make([]string, len(argList))
	for i, a := range argList {
		args[i] = ValueToString(a)
	}
	cmd := exec.CommandContext(vm.ctx, cmdName, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			return vm.trap("E011", "task cancelled")
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return vm.trap("E903", fmt.Sprintf("proc.run: %v", runErr))
		}
	}
	fields := map[string]Value{
		"stdout": ValStr(stdout.String()),
		"stderr": ValStr(stderr.String()),
		"code":   ValInt(exitCode),
	}
	vm.push(ValRecord(fields, []string{"stdout", "stderr", "code"}))
	return nil
}

func (vm *VM) builtinProcSh() error {
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
	out, runErr := cmd.Output()
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			return vm.trap("E011", "task cancelled")
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return vm.trap("E903", fmt.Sprintf("proc.sh: command failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr)))
		}
		return vm.trap("E903", fmt.Sprintf("proc.sh: %v", runErr))
	}
	vm.push(ValStr(string(out)))
	return nil
}

func (vm *VM) builtinProcExit() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	code := 0
	if v.Tag == TagINT {
		code = v.IntVal
	}
	os.Exit(code)
	return nil // unreachable
}
