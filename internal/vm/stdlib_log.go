package vm

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ── std.log — Structured logging ──

var logLevelNames = []string{"trace", "debug", "info", "warn", "error"}

func (vm *VM) logWrite(level int, levelName string) error {
	msg, err := vm.popStr()
	if err != nil {
		return err
	}
	root := vm.root()
	if level < root.logLevel {
		vm.push(ValUnit())
		return nil
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	if root.logJSON {
		entry := map[string]interface{}{
			"ts":    ts,
			"level": levelName,
			"msg":   msg,
		}
		if len(root.logContext) > 0 {
			ctx := make(map[string]string)
			for k, v := range root.logContext {
				ctx[k] = v
			}
			entry["ctx"] = ctx
		}
		data, _ := json.Marshal(entry)
		fmt.Fprintln(os.Stderr, string(data))
	} else {
		ctxStr := ""
		if len(root.logContext) > 0 {
			for k, v := range root.logContext {
				ctxStr += fmt.Sprintf(" %s=%s", k, v)
			}
		}
		fmt.Fprintf(os.Stderr, "[%s] %s %s%s\n",
			fmt.Sprintf("%-5s", levelName), ts, msg, ctxStr)
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinLogTrace() error { return vm.logWrite(0, "trace") }
func (vm *VM) builtinLogDebug() error { return vm.logWrite(1, "debug") }
func (vm *VM) builtinLogInfo() error  { return vm.logWrite(2, "info") }
func (vm *VM) builtinLogWarn() error  { return vm.logWrite(3, "warn") }
func (vm *VM) builtinLogError() error { return vm.logWrite(4, "error") }

func (vm *VM) builtinLogLevel() error {
	levelStr, err := vm.popStr()
	if err != nil {
		return err
	}
	root := vm.root()
	for i, name := range logLevelNames {
		if name == levelStr {
			root.logLevel = i
			vm.push(ValUnit())
			return nil
		}
	}
	return vm.trap("E002", fmt.Sprintf("log.level: unknown level %q (expected trace/debug/info/warn/error)", levelStr))
}

func (vm *VM) builtinLogCtx() error {
	val, err := vm.popStr()
	if err != nil {
		return err
	}
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	root := vm.root()
	root.logContext[key] = val
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinLogJSON() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	root := vm.root()
	if v.Tag == TagBOOL {
		root.logJSON = v.BoolVal
	}
	vm.push(ValUnit())
	return nil
}
