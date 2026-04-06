package vm

import (
	"fmt"
	"regexp"
)

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
