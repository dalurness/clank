package vm

import (
	"fmt"
	"time"
)

// ── std.dt — Date/Time operations ──

func timeToRecord(vm *VM, t time.Time) Value {
	fields := map[string]Value{
		"year":  ValInt(t.Year()),
		"month": ValInt(int(t.Month())),
		"day":   ValInt(t.Day()),
		"hour":  ValInt(t.Hour()),
		"min":   ValInt(t.Minute()),
		"sec":   ValInt(t.Second()),
		"tz":    ValStr(t.Location().String()),
	}
	order := []string{"year", "month", "day", "hour", "min", "sec", "tz"}
	return ValRecord(fields, order)
}

func recordToTime(vm *VM, v Value) (time.Time, error) {
	if v.Tag != TagHEAP || v.Heap.Kind != KindRecord {
		return time.Time{}, vm.trap("E002", "dt: expected datetime record")
	}
	f := v.Heap.Fields
	year := numVal(f["year"])
	month := numVal(f["month"])
	day := numVal(f["day"])
	hour := numVal(f["hour"])
	min := numVal(f["min"])
	sec := numVal(f["sec"])
	tzStr := "UTC"
	if tz, ok := f["tz"]; ok && tz.Tag == TagSTR {
		tzStr = tz.StrVal
	}
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		loc = time.UTC
	}
	return time.Date(year, time.Month(month), day, hour, min, sec, 0, loc), nil
}

func (vm *VM) builtinDtNow() error {
	vm.push(timeToRecord(vm, time.Now().UTC()))
	return nil
}

func (vm *VM) builtinDtUnix() error {
	vm.push(ValInt(int(time.Now().Unix())))
	return nil
}

func (vm *VM) builtinDtFrom() error {
	ts, _ := vm.pop()
	t := time.Unix(int64(numVal(ts)), 0).UTC()
	vm.push(timeToRecord(vm, t))
	return nil
}

func (vm *VM) builtinDtTo() error {
	rec, _ := vm.pop()
	t, err := recordToTime(vm, rec)
	if err != nil {
		return err
	}
	vm.push(ValInt(int(t.Unix())))
	return nil
}

func (vm *VM) builtinDtParse() error {
	fmtStr, err := vm.popStr()
	if err != nil {
		return err
	}
	valStr, err := vm.popStr()
	if err != nil {
		return err
	}
	layout := clankFormatToGo(fmtStr)
	t, parseErr := time.Parse(layout, valStr)
	if parseErr != nil {
		return vm.trap("E900", fmt.Sprintf("dt.parse: %v", parseErr))
	}
	vm.push(timeToRecord(vm, t))
	return nil
}

func (vm *VM) builtinDtFmt() error {
	fmtStr, err := vm.popStr()
	if err != nil {
		return err
	}
	rec, _ := vm.pop()
	t, recErr := recordToTime(vm, rec)
	if recErr != nil {
		return recErr
	}
	layout := clankFormatToGo(fmtStr)
	vm.push(ValStr(t.Format(layout)))
	return nil
}

func (vm *VM) builtinDtAdd() error {
	msVal, _ := vm.pop()
	rec, _ := vm.pop()
	t, err := recordToTime(vm, rec)
	if err != nil {
		return err
	}
	ms := numVal(msVal)
	t = t.Add(time.Duration(ms) * time.Millisecond)
	vm.push(timeToRecord(vm, t))
	return nil
}

func (vm *VM) builtinDtSub() error {
	rec2, _ := vm.pop()
	rec1, _ := vm.pop()
	t1, err := recordToTime(vm, rec1)
	if err != nil {
		return err
	}
	t2, err := recordToTime(vm, rec2)
	if err != nil {
		return err
	}
	vm.push(ValInt(int(t1.Sub(t2).Milliseconds())))
	return nil
}

func (vm *VM) builtinDtTz() error {
	tzStr, err := vm.popStr()
	if err != nil {
		return err
	}
	rec, _ := vm.pop()
	t, recErr := recordToTime(vm, rec)
	if recErr != nil {
		return recErr
	}
	loc, locErr := time.LoadLocation(tzStr)
	if locErr != nil {
		return vm.trap("E900", fmt.Sprintf("dt.tz: unknown timezone %q", tzStr))
	}
	vm.push(timeToRecord(vm, t.In(loc)))
	return nil
}

func (vm *VM) builtinDtIso() error {
	rec, _ := vm.pop()
	t, err := recordToTime(vm, rec)
	if err != nil {
		return err
	}
	vm.push(ValStr(t.Format(time.RFC3339)))
	return nil
}

func (vm *VM) builtinDtMs() error {
	n, _ := vm.pop()
	vm.push(ValInt(numVal(n)))
	return nil
}

func (vm *VM) builtinDtSec() error {
	n, _ := vm.pop()
	vm.push(ValInt(numVal(n) * 1000))
	return nil
}

func (vm *VM) builtinDtMin() error {
	n, _ := vm.pop()
	vm.push(ValInt(numVal(n) * 60000))
	return nil
}

func (vm *VM) builtinDtHr() error {
	n, _ := vm.pop()
	vm.push(ValInt(numVal(n) * 3600000))
	return nil
}

func (vm *VM) builtinDtDay() error {
	n, _ := vm.pop()
	vm.push(ValInt(numVal(n) * 86400000))
	return nil
}

// clankFormatToGo translates common format tokens to Go reference time.
// If the format doesn't contain known tokens, it's passed through as-is
// (allowing users to use Go format directly).
func clankFormatToGo(f string) string {
	// Common patterns agents will use
	replacements := map[string]string{
		"YYYY": "2006",
		"YY":   "06",
		"MM":   "01",
		"DD":   "02",
		"HH":   "15",
		"mm":   "04",
		"ss":   "05",
		"ISO":  time.RFC3339,
	}
	result := f
	for k, v := range replacements {
		if result == k {
			return v
		}
	}
	// Apply token replacements in order (longest first to avoid conflicts)
	for _, pair := range []struct{ from, to string }{
		{"YYYY", "2006"}, {"YY", "06"}, {"MM", "01"}, {"DD", "02"},
		{"HH", "15"}, {"mm", "04"}, {"ss", "05"},
	} {
		for i := 0; i < len(result)-len(pair.from)+1; {
			if result[i:i+len(pair.from)] == pair.from {
				result = result[:i] + pair.to + result[i+len(pair.from):]
				i += len(pair.to)
			} else {
				i++
			}
		}
	}
	return result
}
