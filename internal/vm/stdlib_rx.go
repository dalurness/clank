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

// rx.groups(s, pattern) — submatches of the first match: [whole, g1, g2, ...].
// Returns [] when the pattern does not match. Unmatched optional groups are "".
func (vm *VM) builtinRxGroups() error {
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
		return vm.trap("E905", fmt.Sprintf("rx.groups: invalid pattern: %v", compErr))
	}
	m := re.FindStringSubmatch(s)
	items := make([]Value, len(m))
	for i, g := range m {
		items[i] = ValStr(g)
	}
	vm.push(ValList(items))
	return nil
}

// rx.groups-all(s, pattern) — like rx.groups for every match: [[whole, g1, ...], ...].
func (vm *VM) builtinRxGroupsAll() error {
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
		return vm.trap("E905", fmt.Sprintf("rx.groups-all: invalid pattern: %v", compErr))
	}
	all := re.FindAllStringSubmatch(s, -1)
	items := make([]Value, len(all))
	for i, m := range all {
		groups := make([]Value, len(m))
		for j, g := range m {
			groups[j] = ValStr(g)
		}
		items[i] = ValList(groups)
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
