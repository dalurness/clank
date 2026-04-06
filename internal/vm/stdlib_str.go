package vm

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ── String operations (str.*) ──

func (vm *VM) builtinStrGet() error {
	idxVal, _ := vm.pop()
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	idx := numVal(idxVal)
	runes := []rune(s)
	if idx < 0 || idx >= len(runes) {
		return vm.trap("E004", fmt.Sprintf("str.get: index %d out of bounds (len %d)", idx, len(runes)))
	}
	vm.push(ValStr(string(runes[idx])))
	return nil
}

func (vm *VM) builtinStrSlc() error {
	endVal, _ := vm.pop()
	startVal, _ := vm.pop()
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	runes := []rune(s)
	start := numVal(startVal)
	end := numVal(endVal)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start > end {
		start = end
	}
	vm.push(ValStr(string(runes[start:end])))
	return nil
}

func (vm *VM) builtinStrHas() error {
	substr, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	vm.push(ValBool(strings.Contains(s, substr)))
	return nil
}

func (vm *VM) builtinStrIdx() error {
	substr, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	// Return character index, not byte index
	byteIdx := strings.Index(s, substr)
	if byteIdx < 0 {
		vm.push(ValInt(-1))
	} else {
		vm.push(ValInt(utf8.RuneCountInString(s[:byteIdx])))
	}
	return nil
}

func (vm *VM) builtinStrRIdx() error {
	substr, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	byteIdx := strings.LastIndex(s, substr)
	if byteIdx < 0 {
		vm.push(ValInt(-1))
	} else {
		vm.push(ValInt(utf8.RuneCountInString(s[:byteIdx])))
	}
	return nil
}

func (vm *VM) builtinStrPfx() error {
	prefix, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	vm.push(ValBool(strings.HasPrefix(s, prefix)))
	return nil
}

func (vm *VM) builtinStrSfx() error {
	suffix, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	vm.push(ValBool(strings.HasSuffix(s, suffix)))
	return nil
}

func (vm *VM) builtinStrUp() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	vm.push(ValStr(strings.ToUpper(s)))
	return nil
}

func (vm *VM) builtinStrLo() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	vm.push(ValStr(strings.ToLower(s)))
	return nil
}

func (vm *VM) builtinStrRep() error {
	replacement, err := vm.popStr()
	if err != nil {
		return err
	}
	old, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	vm.push(ValStr(strings.ReplaceAll(s, old, replacement)))
	return nil
}

func (vm *VM) builtinStrRep1() error {
	replacement, err := vm.popStr()
	if err != nil {
		return err
	}
	old, err := vm.popStr()
	if err != nil {
		return err
	}
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	vm.push(ValStr(strings.Replace(s, old, replacement, 1)))
	return nil
}

func (vm *VM) builtinStrPad() error {
	padChar, err := vm.popStr()
	if err != nil {
		return err
	}
	widthVal, _ := vm.pop()
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	width := numVal(widthVal)
	pad := " "
	if len(padChar) > 0 {
		pad = string([]rune(padChar)[0:1])
	}
	for utf8.RuneCountInString(s) < width {
		s = s + pad
	}
	vm.push(ValStr(s))
	return nil
}

func (vm *VM) builtinStrLPad() error {
	padChar, err := vm.popStr()
	if err != nil {
		return err
	}
	widthVal, _ := vm.pop()
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	width := numVal(widthVal)
	pad := " "
	if len(padChar) > 0 {
		pad = string([]rune(padChar)[0:1])
	}
	for utf8.RuneCountInString(s) < width {
		s = pad + s
	}
	vm.push(ValStr(s))
	return nil
}

func (vm *VM) builtinStrRev() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	vm.push(ValStr(string(runes)))
	return nil
}

func (vm *VM) builtinStrLines() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	lines := strings.Split(s, "\n")
	items := make([]Value, len(lines))
	for i, l := range lines {
		items[i] = ValStr(l)
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinStrWords() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	words := strings.Fields(s)
	items := make([]Value, len(words))
	for i, w := range words {
		items[i] = ValStr(w)
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinStrChars() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	runes := []rune(s)
	items := make([]Value, len(runes))
	for i, r := range runes {
		items[i] = ValStr(string(r))
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinStrInt() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	someTag, _ := vm.findVariantTag("Some")
	noneTag, _ := vm.findVariantTag("None")
	n, parseErr := strconv.Atoi(s)
	if parseErr != nil {
		vm.push(ValUnion(noneTag, nil))
	} else {
		vm.push(ValUnion(someTag, []Value{ValInt(n)}))
	}
	return nil
}

func (vm *VM) builtinStrRat() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	someTag, _ := vm.findVariantTag("Some")
	noneTag, _ := vm.findVariantTag("None")
	f, parseErr := strconv.ParseFloat(s, 64)
	if parseErr != nil {
		vm.push(ValUnion(noneTag, nil))
	} else {
		vm.push(ValUnion(someTag, []Value{ValRat(f)}))
	}
	return nil
}
