package vm

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// ── Filesystem (fs.*) ──

func (vm *VM) builtinFsRead() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.read: %v", readErr))
	}
	vm.push(ValStr(string(data)))
	return nil
}

func (vm *VM) builtinFsWrite() error {
	content, err := vm.popStr()
	if err != nil {
		return err
	}
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	if writeErr := os.WriteFile(path, []byte(content), 0644); writeErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.write: %v", writeErr))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinFsExists() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	_, statErr := os.Stat(path)
	vm.push(ValBool(statErr == nil))
	return nil
}

func (vm *VM) builtinFsLs() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	entries, readErr := os.ReadDir(path)
	if readErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.ls: %v", readErr))
	}
	items := make([]Value, len(entries))
	for i, e := range entries {
		items[i] = ValStr(e.Name())
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinFsMkdir() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(path, 0755); mkErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.mkdir: %v", mkErr))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinFsRm() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	if rmErr := os.RemoveAll(path); rmErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.rm: %v", rmErr))
	}
	vm.push(ValUnit())
	return nil
}

// ── JSON (json.*) ──

func (vm *VM) builtinJsonEnc() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	goVal := valueToGo(v)
	data, jsonErr := json.Marshal(goVal)
	if jsonErr != nil {
		return vm.trap("E901", fmt.Sprintf("json.enc: %v", jsonErr))
	}
	vm.push(ValStr(string(data)))
	return nil
}

func (vm *VM) builtinJsonDec() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	var raw interface{}
	if jsonErr := json.Unmarshal([]byte(s), &raw); jsonErr != nil {
		return vm.trap("E901", fmt.Sprintf("json.dec: %v", jsonErr))
	}
	vm.push(goToValue(raw))
	return nil
}

func (vm *VM) builtinJsonGet() error {
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	obj, err := vm.pop()
	if err != nil {
		return err
	}
	if obj.Tag == TagHEAP && obj.Heap.Kind == KindRecord {
		if val, ok := obj.Heap.Fields[key]; ok {
			someTag, _ := vm.findVariantTag("Some")
			vm.push(ValUnion(someTag, []Value{val}))
		} else {
			noneTag, _ := vm.findVariantTag("None")
			vm.push(ValUnion(noneTag, nil))
		}
		return nil
	}
	noneTag, _ := vm.findVariantTag("None")
	vm.push(ValUnion(noneTag, nil))
	return nil
}

func (vm *VM) builtinJsonSet() error {
	val, err := vm.pop()
	if err != nil {
		return err
	}
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	obj, err := vm.pop()
	if err != nil {
		return err
	}
	if obj.Tag != TagHEAP || obj.Heap.Kind != KindRecord {
		return vm.trap("E901", "json.set: expected record")
	}
	newFields := make(map[string]Value)
	newOrder := make([]string, len(obj.Heap.FieldOrder))
	copy(newOrder, obj.Heap.FieldOrder)
	for k, v := range obj.Heap.Fields {
		newFields[k] = v
	}
	if _, exists := newFields[key]; !exists {
		newOrder = append(newOrder, key)
	}
	newFields[key] = val
	vm.push(ValRecord(newFields, newOrder))
	return nil
}

func (vm *VM) builtinJsonKeys() error {
	obj, err := vm.pop()
	if err != nil {
		return err
	}
	if obj.Tag != TagHEAP || obj.Heap.Kind != KindRecord {
		return vm.trap("E901", "json.keys: expected record")
	}
	order := obj.Heap.FieldOrder
	if len(order) == 0 {
		for k := range obj.Heap.Fields {
			order = append(order, k)
		}
		sort.Strings(order)
	}
	items := make([]Value, len(order))
	for i, k := range order {
		items[i] = ValStr(k)
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinJsonMerge() error {
	b, err := vm.pop()
	if err != nil {
		return err
	}
	a, err := vm.pop()
	if err != nil {
		return err
	}
	if a.Tag != TagHEAP || a.Heap.Kind != KindRecord || b.Tag != TagHEAP || b.Heap.Kind != KindRecord {
		return vm.trap("E901", "json.merge: expected two records")
	}
	newFields := make(map[string]Value)
	var newOrder []string
	for _, k := range a.Heap.FieldOrder {
		newFields[k] = a.Heap.Fields[k]
		newOrder = append(newOrder, k)
	}
	for _, k := range b.Heap.FieldOrder {
		if _, exists := newFields[k]; !exists {
			newOrder = append(newOrder, k)
		}
		newFields[k] = b.Heap.Fields[k]
	}
	vm.push(ValRecord(newFields, newOrder))
	return nil
}

// ── Environment (env.*) ──

func (vm *VM) builtinEnvGet() error {
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	val, ok := os.LookupEnv(key)
	if ok {
		someTag, _ := vm.findVariantTag("Some")
		vm.push(ValUnion(someTag, []Value{ValStr(val)}))
	} else {
		noneTag, _ := vm.findVariantTag("None")
		vm.push(ValUnion(noneTag, nil))
	}
	return nil
}

func (vm *VM) builtinEnvSet() error {
	val, err := vm.popStr()
	if err != nil {
		return err
	}
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	if setErr := os.Setenv(key, val); setErr != nil {
		return vm.trap("E902", fmt.Sprintf("env.set: %v", setErr))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinEnvHas() error {
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	_, ok := os.LookupEnv(key)
	vm.push(ValBool(ok))
	return nil
}

func (vm *VM) builtinEnvAll() error {
	environ := os.Environ()
	items := make([]Value, 0, len(environ))
	for _, e := range environ {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			items = append(items, ValTuple([]Value{ValStr(parts[0]), ValStr(parts[1])}))
		}
	}
	vm.push(ValList(items))
	return nil
}

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
	cmd := exec.Command(cmdName, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
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
		cmd = exec.Command("cmd", "/C", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}
	out, runErr := cmd.Output()
	if runErr != nil {
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

// ── HTTP (http.*) ──

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
	req, reqErr := http.NewRequest(method, url, body)
	if reqErr != nil {
		return vm.trap("E904", fmt.Sprintf("http.%s: %v", strings.ToLower(method), reqErr))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, doErr := client.Do(req)
	if doErr != nil {
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

// ── Sleep fix ──

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
		time.Sleep(time.Duration(duration) * time.Millisecond)
	}
	vm.push(ValUnit())
	return nil
}

// ── Helpers for JSON conversion ──

func valueToGo(v Value) interface{} {
	switch v.Tag {
	case TagINT:
		return v.IntVal
	case TagRAT:
		return v.RatVal
	case TagBOOL:
		return v.BoolVal
	case TagSTR:
		return v.StrVal
	case TagUNIT:
		return nil
	case TagHEAP:
		switch v.Heap.Kind {
		case KindList:
			items := make([]interface{}, len(v.Heap.Items))
			for i, el := range v.Heap.Items {
				items[i] = valueToGo(el)
			}
			return items
		case KindTuple:
			items := make([]interface{}, len(v.Heap.Items))
			for i, el := range v.Heap.Items {
				items[i] = valueToGo(el)
			}
			return items
		case KindRecord:
			obj := make(map[string]interface{})
			for k, val := range v.Heap.Fields {
				obj[k] = valueToGo(val)
			}
			return obj
		case KindUnion:
			if len(activeVariantNames) > v.Heap.VariantTag {
				name := activeVariantNames[v.Heap.VariantTag]
				if name == "None" && len(v.Heap.UFields) == 0 {
					return nil
				}
				if name == "Some" && len(v.Heap.UFields) == 1 {
					return valueToGo(v.Heap.UFields[0])
				}
			}
			return ValueToString(v)
		}
	}
	return nil
}

func goToValue(v interface{}) Value {
	if v == nil {
		return ValUnit()
	}
	switch val := v.(type) {
	case float64:
		if val == float64(int(val)) && val >= -9.2e18 && val <= 9.2e18 {
			return ValInt(int(val))
		}
		return ValRat(val)
	case bool:
		return ValBool(val)
	case string:
		return ValStr(val)
	case []interface{}:
		items := make([]Value, len(val))
		for i, el := range val {
			items[i] = goToValue(el)
		}
		return ValList(items)
	case map[string]interface{}:
		fields := make(map[string]Value)
		var order []string
		for k, v := range val {
			order = append(order, k)
			fields[k] = goToValue(v)
		}
		sort.Strings(order)
		return ValRecord(fields, order)
	}
	return ValStr(fmt.Sprintf("%v", v))
}

// ── Regex (rx.*) ──

func (vm *VM) builtinRxTest() error {
	pattern, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	re, compErr := regexp.Compile(pattern)
	if compErr != nil {
		return vm.trap("E905", fmt.Sprintf("rx.test: invalid pattern: %v", compErr))
	}
	vm.push(ValBool(re.MatchString(s)))
	return nil
}

func (vm *VM) builtinRxFind() error {
	pattern, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	re, compErr := regexp.Compile(pattern)
	if compErr != nil {
		return vm.trap("E905", fmt.Sprintf("rx.find: invalid pattern: %v", compErr))
	}
	matches := re.FindAllString(s, -1)
	items := make([]Value, len(matches))
	for i, m := range matches {
		items[i] = ValStr(m)
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinRxReplace() error {
	replacement, err := vm.popStr()
	if err != nil {
		return err
	}
	pattern, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	re, compErr := regexp.Compile(pattern)
	if compErr != nil {
		return vm.trap("E905", fmt.Sprintf("rx.replace: invalid pattern: %v", compErr))
	}
	vm.push(ValStr(re.ReplaceAllString(s, replacement)))
	return nil
}

func (vm *VM) builtinRxSplit() error {
	pattern, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	re, compErr := regexp.Compile(pattern)
	if compErr != nil {
		return vm.trap("E905", fmt.Sprintf("rx.split: invalid pattern: %v", compErr))
	}
	parts := re.Split(s, -1)
	items := make([]Value, len(parts))
	for i, p := range parts {
		items[i] = ValStr(p)
	}
	vm.push(ValList(items))
	return nil
}

// ── Math (math.*) ──

func (vm *VM) builtinMathAbs() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	switch v.Tag {
	case TagINT:
		if v.IntVal < 0 {
			vm.push(ValInt(-v.IntVal))
		} else {
			vm.push(v)
		}
	case TagRAT:
		vm.push(ValRat(math.Abs(v.RatVal)))
	default:
		return vm.trap("E002", "math.abs: expected numeric type")
	}
	return nil
}

func (vm *VM) builtinMathMin() error {
	b, err := vm.pop()
	if err != nil {
		return err
	}
	a, err := vm.pop()
	if err != nil {
		return err
	}
	av, aok := NumericValue(a)
	bv, bok := NumericValue(b)
	if !aok || !bok {
		return vm.trap("E002", "math.min: expected numeric type")
	}
	if av <= bv {
		vm.push(a)
	} else {
		vm.push(b)
	}
	return nil
}

func (vm *VM) builtinMathMax() error {
	b, err := vm.pop()
	if err != nil {
		return err
	}
	a, err := vm.pop()
	if err != nil {
		return err
	}
	av, aok := NumericValue(a)
	bv, bok := NumericValue(b)
	if !aok || !bok {
		return vm.trap("E002", "math.max: expected numeric type")
	}
	if av >= bv {
		vm.push(a)
	} else {
		vm.push(b)
	}
	return nil
}

func (vm *VM) builtinMathFloor() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	fv, ok := NumericValue(v)
	if !ok {
		return vm.trap("E002", "math.floor: expected numeric type")
	}
	vm.push(ValInt(int(math.Floor(fv))))
	return nil
}

func (vm *VM) builtinMathCeil() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	fv, ok := NumericValue(v)
	if !ok {
		return vm.trap("E002", "math.ceil: expected numeric type")
	}
	vm.push(ValInt(int(math.Ceil(fv))))
	return nil
}

func (vm *VM) builtinMathSqrt() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	fv, ok := NumericValue(v)
	if !ok {
		return vm.trap("E002", "math.sqrt: expected numeric type")
	}
	if fv < 0 {
		return vm.trap("E003", "math.sqrt: negative argument")
	}
	vm.push(ValRat(math.Sqrt(fv)))
	return nil
}

// ── Helper: absolute path ──

func absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
