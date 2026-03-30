package eval

import (
	"fmt"
	"math"
	"strings"

	"github.com/dalurness/clank/internal/token"
)

// applyFn is set by the evaluator to allow builtins to call user functions.
var applyFn func(fn Value, args []Value, loc token.Loc) Value

// SetApplyFn registers the function application callback for higher-order builtins.
func SetApplyFn(fn func(Value, []Value, token.Loc) Value) {
	applyFn = fn
}

func expectNum(v Value, loc token.Loc) float64 {
	switch val := v.(type) {
	case ValInt:
		return float64(val.Val)
	case ValRat:
		return val.Val
	}
	panic(runtimeError("E200", fmt.Sprintf("expected number, got %s", v.valueTag()), loc))
}

func expectBool(v Value, loc token.Loc) bool {
	if val, ok := v.(ValBool); ok {
		return val.Val
	}
	panic(runtimeError("E200", fmt.Sprintf("expected Bool, got %s", v.valueTag()), loc))
}

func expectStr(v Value, loc token.Loc) string {
	if val, ok := v.(ValStr); ok {
		return val.Val
	}
	panic(runtimeError("E200", fmt.Sprintf("expected Str, got %s", v.valueTag()), loc))
}

func expectList(v Value, loc token.Loc) []Value {
	if val, ok := v.(ValList); ok {
		return val.Elements
	}
	panic(runtimeError("E200", fmt.Sprintf("expected List, got %s", v.valueTag()), loc))
}

func expectInt(v Value, loc token.Loc) int64 {
	if val, ok := v.(ValInt); ok {
		return val.Val
	}
	panic(runtimeError("E200", fmt.Sprintf("expected Int, got %s", v.valueTag()), loc))
}

func isRat(a, b Value) bool {
	_, aRat := a.(ValRat)
	_, bRat := b.(ValRat)
	return aRat || bRat
}

func numResult(rat bool, val float64) Value {
	if rat {
		return ValRat{Val: val}
	}
	return ValInt{Val: int64(val)}
}

// Builtins returns the standard builtin functions.
func Builtins() map[string]func([]Value, token.Loc) Value {
	return map[string]func([]Value, token.Loc) Value{
		// Arithmetic
		"add": func(args []Value, loc token.Loc) Value {
			a, b := expectNum(args[0], loc), expectNum(args[1], loc)
			return numResult(isRat(args[0], args[1]), a+b)
		},
		"sub": func(args []Value, loc token.Loc) Value {
			a, b := expectNum(args[0], loc), expectNum(args[1], loc)
			return numResult(isRat(args[0], args[1]), a-b)
		},
		"mul": func(args []Value, loc token.Loc) Value {
			a, b := expectNum(args[0], loc), expectNum(args[1], loc)
			return numResult(isRat(args[0], args[1]), a*b)
		},
		"div": func(args []Value, loc token.Loc) Value {
			a, b := expectNum(args[0], loc), expectNum(args[1], loc)
			if b == 0 {
				panic(runtimeError("E201", "division by zero", loc))
			}
			if isRat(args[0], args[1]) {
				return ValRat{Val: a / b}
			}
			return ValInt{Val: int64(math.Trunc(a / b))}
		},
		"mod": func(args []Value, loc token.Loc) Value {
			a, b := expectNum(args[0], loc), expectNum(args[1], loc)
			if b == 0 {
				panic(runtimeError("E201", "modulo by zero", loc))
			}
			return ValInt{Val: int64(a) % int64(b)}
		},

		// Comparison
		"eq":  func(args []Value, _ token.Loc) Value { return ValBool{Val: ValEqual(args[0], args[1])} },
		"neq": func(args []Value, _ token.Loc) Value { return ValBool{Val: !ValEqual(args[0], args[1])} },
		"lt": func(args []Value, loc token.Loc) Value {
			return ValBool{Val: expectNum(args[0], loc) < expectNum(args[1], loc)}
		},
		"gt": func(args []Value, loc token.Loc) Value {
			return ValBool{Val: expectNum(args[0], loc) > expectNum(args[1], loc)}
		},
		"lte": func(args []Value, loc token.Loc) Value {
			return ValBool{Val: expectNum(args[0], loc) <= expectNum(args[1], loc)}
		},
		"gte": func(args []Value, loc token.Loc) Value {
			return ValBool{Val: expectNum(args[0], loc) >= expectNum(args[1], loc)}
		},

		// Logic
		"and": func(args []Value, loc token.Loc) Value {
			return ValBool{Val: expectBool(args[0], loc) && expectBool(args[1], loc)}
		},
		"or": func(args []Value, loc token.Loc) Value {
			return ValBool{Val: expectBool(args[0], loc) || expectBool(args[1], loc)}
		},
		"not": func(args []Value, loc token.Loc) Value {
			return ValBool{Val: !expectBool(args[0], loc)}
		},
		"negate": func(args []Value, loc token.Loc) Value {
			n := expectNum(args[0], loc)
			if _, ok := args[0].(ValRat); ok {
				return ValRat{Val: -n}
			}
			return ValInt{Val: -int64(n)}
		},

		// Strings
		"str.cat": func(args []Value, loc token.Loc) Value {
			return ValStr{Val: expectStr(args[0], loc) + expectStr(args[1], loc)}
		},

		// I/O and display
		"show": func(args []Value, _ token.Loc) Value {
			return ValStr{Val: ShowValue(args[0])}
		},
		"print": func(args []Value, loc token.Loc) Value {
			s := expectStr(args[0], loc)
			fmt.Println(s)
			return ValUnit{}
		},

		// List operations
		"len": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			return ValInt{Val: int64(len(list))}
		},
		"head": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			if len(list) == 0 {
				panic(runtimeError("E208", "head of empty list", loc))
			}
			return list[0]
		},
		"tail": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			if len(list) == 0 {
				panic(runtimeError("E208", "tail of empty list", loc))
			}
			newElems := make([]Value, len(list)-1)
			copy(newElems, list[1:])
			return ValList{Elements: newElems}
		},
		"cons": func(args []Value, loc token.Loc) Value {
			list := expectList(args[1], loc)
			newElems := make([]Value, 0, len(list)+1)
			newElems = append(newElems, args[0])
			newElems = append(newElems, list...)
			return ValList{Elements: newElems}
		},
		"cat": func(args []Value, loc token.Loc) Value {
			a := expectList(args[0], loc)
			b := expectList(args[1], loc)
			newElems := make([]Value, 0, len(a)+len(b))
			newElems = append(newElems, a...)
			newElems = append(newElems, b...)
			return ValList{Elements: newElems}
		},
		"rev": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			newElems := make([]Value, len(list))
			for i, v := range list {
				newElems[len(list)-1-i] = v
			}
			return ValList{Elements: newElems}
		},
		"get": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			idx := expectInt(args[1], loc)
			if idx < 0 || int(idx) >= len(list) {
				panic(runtimeError("E209", fmt.Sprintf("index %d out of bounds (length %d)", idx, len(list)), loc))
			}
			return list[idx]
		},
		"map": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			fn := args[1]
			results := make([]Value, len(list))
			for i, el := range list {
				results[i] = applyFn(fn, []Value{el}, loc)
			}
			return ValList{Elements: results}
		},
		"filter": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			fn := args[1]
			var results []Value
			for _, el := range list {
				result := applyFn(fn, []Value{el}, loc)
				if bv, ok := result.(ValBool); ok && bv.Val {
					results = append(results, el)
				}
			}
			if results == nil {
				results = []Value{}
			}
			return ValList{Elements: results}
		},
		"fold": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			acc := args[1]
			fn := args[2]
			for _, el := range list {
				acc = applyFn(fn, []Value{acc, el}, loc)
			}
			return acc
		},
		"flat-map": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			fn := args[1]
			var results []Value
			for _, el := range list {
				inner := applyFn(fn, []Value{el}, loc)
				innerList := expectList(inner, loc)
				results = append(results, innerList...)
			}
			if results == nil {
				results = []Value{}
			}
			return ValList{Elements: results}
		},

		// Tuple access
		"tuple.get": func(args []Value, loc token.Loc) Value {
			tup, ok := args[0].(ValTuple)
			if !ok {
				panic(runtimeError("E200", fmt.Sprintf("expected Tuple, got %s", args[0].valueTag()), loc))
			}
			idx := expectInt(args[1], loc)
			if idx < 0 || int(idx) >= len(tup.Elements) {
				panic(runtimeError("E209", fmt.Sprintf("tuple index %d out of bounds (size %d)", idx, len(tup.Elements)), loc))
			}
			return tup.Elements[idx]
		},

		// Range
		"range": func(args []Value, loc token.Loc) Value {
			start := expectInt(args[0], loc)
			end := expectInt(args[1], loc)
			var elements []Value
			for i := start; i <= end; i++ {
				elements = append(elements, ValInt{Val: i})
			}
			if elements == nil {
				elements = []Value{}
			}
			return ValList{Elements: elements}
		},

		// Zip and tuple accessors
		"zip": func(args []Value, loc token.Loc) Value {
			xs := expectList(args[0], loc)
			ys := expectList(args[1], loc)
			n := len(xs)
			if len(ys) < n {
				n = len(ys)
			}
			elements := make([]Value, n)
			for i := 0; i < n; i++ {
				elements[i] = ValTuple{Elements: []Value{xs[i], ys[i]}}
			}
			return ValList{Elements: elements}
		},
		"fst": func(args []Value, loc token.Loc) Value {
			tup, ok := args[0].(ValTuple)
			if !ok {
				panic(runtimeError("E200", fmt.Sprintf("expected Tuple, got %s", args[0].valueTag()), loc))
			}
			if len(tup.Elements) < 1 {
				panic(runtimeError("E209", "tuple is empty", loc))
			}
			return tup.Elements[0]
		},
		"snd": func(args []Value, loc token.Loc) Value {
			tup, ok := args[0].(ValTuple)
			if !ok {
				panic(runtimeError("E200", fmt.Sprintf("expected Tuple, got %s", args[0].valueTag()), loc))
			}
			if len(tup.Elements) < 2 {
				panic(runtimeError("E209", "tuple has fewer than 2 elements", loc))
			}
			return tup.Elements[1]
		},

		// String operations
		"split": func(args []Value, loc token.Loc) Value {
			s := expectStr(args[0], loc)
			sep := expectStr(args[1], loc)
			parts := strings.Split(s, sep)
			elements := make([]Value, len(parts))
			for i, p := range parts {
				elements[i] = ValStr{Val: p}
			}
			return ValList{Elements: elements}
		},
		"join": func(args []Value, loc token.Loc) Value {
			list := expectList(args[0], loc)
			sep := expectStr(args[1], loc)
			strs := make([]string, len(list))
			for i, el := range list {
				strs[i] = expectStr(el, loc)
			}
			return ValStr{Val: strings.Join(strs, sep)}
		},
		"trim": func(args []Value, loc token.Loc) Value {
			return ValStr{Val: strings.TrimSpace(expectStr(args[0], loc))}
		},

		// Runtime-dispatched for-loop builtins
		"__for_each": func(args []Value, loc token.Loc) Value {
			collection := args[0]
			fn := args[1]
			if list, ok := collection.(ValList); ok {
				results := make([]Value, len(list.Elements))
				for i, el := range list.Elements {
					results[i] = applyFn(fn, []Value{el}, loc)
				}
				return ValList{Elements: results}
			}
			panic(runtimeError("E204", "__for_each: expected List", loc))
		},
		"__for_filter": func(args []Value, loc token.Loc) Value {
			collection := args[0]
			fn := args[1]
			if list, ok := collection.(ValList); ok {
				var results []Value
				for _, el := range list.Elements {
					result := applyFn(fn, []Value{el}, loc)
					if bv, ok := result.(ValBool); ok && bv.Val {
						results = append(results, el)
					}
				}
				if results == nil {
					results = []Value{}
				}
				return ValList{Elements: results}
			}
			panic(runtimeError("E204", "__for_filter: expected List", loc))
		},
		"__for_fold": func(args []Value, loc token.Loc) Value {
			collection := args[0]
			init := args[1]
			fn := args[2]
			if list, ok := collection.(ValList); ok {
				acc := init
				for _, el := range list.Elements {
					acc = applyFn(fn, []Value{acc, el}, loc)
				}
				return acc
			}
			panic(runtimeError("E204", "__for_fold: expected List", loc))
		},
	}
}
