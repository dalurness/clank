package compiler

import (
	"fmt"

	"github.com/dalurness/clank/internal/ast"
)

// builtinOps maps builtin function names to direct opcode sequences.
var builtinOps = map[string][]byte{
	"add":    {OpADD}, "sub": {OpSUB}, "mul": {OpMUL}, "div": {OpDIV}, "mod": {OpMOD},
	"negate": {OpNEG},
	"eq":     {OpEQ}, "neq": {OpNEQ}, "lt": {OpLT}, "gt": {OpGT}, "lte": {OpLTE}, "gte": {OpGTE},
	"not":    {OpNOT},
	"str.cat": {OpSTR_CAT}, "show": {OpTO_STR}, "print": {OpIO_PRINT},
	"len":   {OpLIST_LEN}, "head": {OpLIST_HEAD}, "tail": {OpLIST_TAIL},
	"cons":  {OpLIST_CONS}, "cat": {OpLIST_CAT}, "rev": {OpLIST_REV},
	"split": {OpSTR_SPLIT}, "join": {OpSTR_JOIN}, "trim": {OpSTR_TRIM},
	"tuple.get": {OpTUPLE_GET_DYN}, "get": {OpLIST_IDX},
	"fst": {OpTUPLE_GET, 0}, "snd": {OpTUPLE_GET, 1},
	// Async
	"spawn": {OpTASK_SPAWN}, "await": {OpTASK_AWAIT},
	"task-yield": {OpTASK_YIELD}, "sleep": {OpTASK_SLEEP},
	"is-cancelled": {OpTASK_CANCEL_CHECK},
	"channel": {OpCHAN_NEW}, "send": {OpCHAN_SEND}, "recv": {OpCHAN_RECV},
	"try-recv": {OpCHAN_TRY_RECV}, "close-sender": {OpCHAN_CLOSE}, "close-receiver": {OpCHAN_CLOSE},
	"select-wait": {OpSELECT_WAIT},
	"str.split": {OpSTR_SPLIT}, "str.join": {OpSTR_JOIN}, "str.trim": {OpSTR_TRIM},
	// STM
	"tvar-new": {OpTVAR_NEW}, "tvar-read": {OpTVAR_READ}, "tvar-write": {OpTVAR_WRITE},
	"tvar-take": {OpTVAR_TAKE}, "tvar-put": {OpTVAR_PUT},
	// Iterator
	"iter-new": {OpITER_NEW}, "iter-next": {OpITER_NEXT}, "iter-close": {OpITER_CLOSE},
	// Ref
	"ref-new": {OpREF_NEW}, "ref-read": {OpREF_READ}, "ref-write": {OpREF_WRITE},
	"ref-cas": {OpREF_CAS}, "ref-modify": {OpREF_MODIFY}, "ref-close": {OpREF_CLOSE},
}

// vmBuiltins maps builtin function names to their reserved word IDs (0-259).
var vmBuiltins = map[string]int{
	"map": 1, "filter": 2, "fold": 3, "flat-map": 4, "range": 5, "zip": 6,
	"task-group": 7, "shield": 8, "check-cancel": 9,
	"atomically": 65, "or-else": 66, "retry": 67,
	"cmp$Int": 230, "cmp$Rat": 231, "cmp$Str": 232,
	"show$Record": 240, "eq$Record": 241, "clone$Record": 242,
	"cmp$Record": 243, "default$Record": 244,
	"show$List": 250, "eq$List": 251, "clone$List": 252,
	"show$Tuple": 253, "eq$Tuple": 254, "clone$Tuple": 255,
	"cmp$List": 256, "cmp$Tuple": 257,
	"clone$Ref": 258, "clone$TVar": 259, "ref-swap": 260,
	// Tier 2
	"http.get": 120, "http.post": 121, "http.put": 122, "http.del": 123,
	"http.patch": 124, "http.req": 125, "http.hdr": 126, "http.json": 127, "http.ok?": 128,
	"srv.new": 130, "srv.get": 131, "srv.post": 132, "srv.put": 133, "srv.del": 134,
	"srv.start": 135, "srv.stop": 136, "srv.res": 137, "srv.json": 138, "srv.hdr": 139, "srv.mw": 140,
	"csv.dec": 145, "csv.enc": 146, "csv.decf": 147, "csv.encf": 148,
	"csv.hdr": 149, "csv.rows": 150, "csv.maps": 151, "csv.opts": 152,
	"proc.run": 155, "proc.sh": 156, "proc.ok": 157, "proc.pipe": 158,
	"proc.bg": 159, "proc.wait": 160, "proc.kill": 161, "proc.exit": 162, "proc.pid": 163,
	"dt.now": 170, "dt.unix": 171, "dt.from": 172, "dt.to": 173,
	"dt.parse": 174, "dt.fmt": 175, "dt.add": 176, "dt.sub": 177,
	"dt.tz": 178, "dt.iso": 179, "dt.ms": 180, "dt.sec": 181,
	"dt.min": 182, "dt.hr": 183, "dt.day": 184,
	// Iterator combinators
	"iter.of": 70, "iter.range": 71, "iter.collect": 72,
	"iter.map": 73, "iter.filter": 74, "iter.take": 75,
	"iter.drop": 76, "iter.fold": 77, "iter.count": 78,
	"iter.sum": 79, "iter.any": 80, "iter.all": 81,
	"iter.find": 82, "iter.each": 83, "iter.drain": 84,
	"iter.enumerate": 85, "iter.chain": 86, "iter.zip": 87,
	"iter.take-while": 88, "iter.drop-while": 89,
	"iter.flatmap": 90, "iter.first": 91, "iter.last": 92,
	"iter.join": 93, "iter.repeat": 94, "iter.once": 95,
	"iter.empty": 96, "iter.unfold": 97, "iter.scan": 98,
	"iter.dedup": 99, "iter.chunk": 100, "iter.window": 101,
	"iter.intersperse": 102, "iter.cycle": 103,
	"iter.nth": 104, "iter.min": 105, "iter.max": 106,
	"iter.generate": 107,
	"iter-of": 70, "iter-range": 71, "iter-recv": 108,
	"iter-send": 109, "iter-spawn": 110,
	"collect": 72, "drain": 84, "close-iter": 111, "next": 112,
	// Streaming I/O
	"fs.stream-lines": 190, "http.stream-lines": 191,
	"proc.stream": 192, "io.stdin-lines": 193,
	// Runtime-dispatched for-loop
	"__for_each": 113, "__for_filter": 114, "__for_fold": 115,
	// Filesystem
	"fs.read": 200, "fs.write": 201, "fs.exists": 202,
	"fs.ls": 203, "fs.mkdir": 204, "fs.rm": 205,
	// JSON
	"json.enc": 207, "json.dec": 208, "json.get": 209,
	"json.set": 210, "json.keys": 211, "json.merge": 212,
	// Environment
	"env.get": 214, "env.set": 215, "env.has": 216, "env.all": 217,
	// Regex
	"rx.ok": 220, "rx.find": 221, "rx.replace": 222, "rx.split": 223,
	// Math
	"math.abs": 224, "math.min": 225, "math.max": 226,
	"math.floor": 227, "math.ceil": 228, "math.sqrt": 229,
}

// ── Code Emitter ──

type codeEmitter struct {
	code []byte
}

func (e *codeEmitter) pos() int { return len(e.code) }

func (e *codeEmitter) emit(op byte) { e.code = append(e.code, op) }

func (e *codeEmitter) emitU8(op byte, val int) {
	e.code = append(e.code, op, byte(val&0xFF))
}

func (e *codeEmitter) emitU16(op byte, val int) {
	e.code = append(e.code, op, byte((val>>8)&0xFF), byte(val&0xFF))
}

func (e *codeEmitter) emitU32(op byte, val int) {
	e.code = append(e.code, op,
		byte((val>>24)&0xFF), byte((val>>16)&0xFF),
		byte((val>>8)&0xFF), byte(val&0xFF))
}

func (e *codeEmitter) emitJumpPlaceholder(op byte) int {
	e.code = append(e.code, op)
	patch := len(e.code)
	e.code = append(e.code, 0, 0)
	return patch
}

func (e *codeEmitter) patchJump(patch int) {
	offset := len(e.code) - patch - 2
	e.code[patch] = byte((offset >> 8) & 0xFF)
	e.code[patch+1] = byte(offset & 0xFF)
}

// ── Local Scope ──

type localScope struct {
	slots    map[string]int
	nextSlot int
}

func newLocalScope() *localScope {
	return &localScope{slots: make(map[string]int)}
}

func (s *localScope) allocate(name string) int {
	slot := s.nextSlot
	s.nextSlot++
	s.slots[name] = slot
	return slot
}

func (s *localScope) get(name string) (int, bool) {
	slot, ok := s.slots[name]
	return slot, ok
}

func (s *localScope) count() int { return s.nextSlot }

func (s *localScope) child() *localScope {
	c := &localScope{
		slots:    make(map[string]int, len(s.slots)),
		nextSlot: s.nextSlot,
	}
	for k, v := range s.slots {
		c.slots[k] = v
	}
	return c
}

// ── Compiler ──

type variantInfo struct {
	tag   int
	arity int
}

type deferredLambda struct {
	name     string
	params   []ast.Param
	body     ast.Expr
	captures []string
}

// Compiler compiles a Clank AST program into a BytecodeModule.
type Compiler struct {
	strings      []string
	stringIndex  map[string]int
	rationals    []float64
	rationalIdx  map[float64]int
	words        []BytecodeWord
	wordIDs      map[string]int
	nextWordID   int
	resumeVars   map[string]int
	lambdaBodies []deferredLambda
	variantInfos map[string]variantInfo
	variantNames []string
	nextVarTag   int
	effectOps    map[string]int
	interfaceMethods     map[string]bool
	interfaceMethodParam map[string]int
	dispatchTable        map[string]map[string]int
}

// NewCompiler creates a new compiler instance.
func NewCompiler() *Compiler {
	return &Compiler{
		stringIndex:          make(map[string]int),
		rationalIdx:          make(map[float64]int),
		wordIDs:              make(map[string]int),
		nextWordID:           260,
		resumeVars:           make(map[string]int),
		variantInfos:         make(map[string]variantInfo),
		effectOps:            make(map[string]int),
		interfaceMethods:     make(map[string]bool),
		interfaceMethodParam: make(map[string]int),
		dispatchTable:        make(map[string]map[string]int),
	}
}

func (c *Compiler) internString(s string) int {
	if idx, ok := c.stringIndex[s]; ok {
		return idx
	}
	idx := len(c.strings)
	c.strings = append(c.strings, s)
	c.stringIndex[s] = idx
	return idx
}

func (c *Compiler) internRational(r float64) int {
	if idx, ok := c.rationalIdx[r]; ok {
		return idx
	}
	idx := len(c.rationals)
	c.rationals = append(c.rationals, r)
	c.rationalIdx[r] = idx
	return idx
}

func (c *Compiler) allocWordID(name string) int {
	if id, ok := c.wordIDs[name]; ok {
		return id
	}
	id := c.nextWordID
	c.nextWordID++
	c.wordIDs[name] = id
	return id
}

// Compile compiles a program into a BytecodeModule.
func (c *Compiler) Compile(program *ast.Program) *BytecodeModule {
	// Register VM builtin word IDs
	for name, id := range vmBuiltins {
		c.wordIDs[name] = id
	}

	// Synthesize wrapper words for builtinOps so they can be used as values
	reservedIDs := make(map[int]bool)
	for _, id := range vmBuiltins {
		reservedIDs[id] = true
	}
	nextBuiltinWordID := 10
	for name, ops := range builtinOps {
		if _, exists := c.wordIDs[name]; !exists {
			for reservedIDs[nextBuiltinWordID] {
				nextBuiltinWordID++
			}
			id := nextBuiltinWordID
			nextBuiltinWordID++
			c.wordIDs[name] = id
			code := make([]byte, len(ops)+1)
			copy(code, ops)
			code[len(ops)] = OpRET
			c.words = append(c.words, BytecodeWord{
				Name: name, WordID: id, Code: code, IsPublic: false,
			})
		}
	}

	// Pre-register built-in effect operations
	c.effectOps["raise"] = 1

	// Register built-in Ordering variants
	for _, name := range []string{"None", "Some", "Lt", "Eq_", "Gt"} {
		if _, exists := c.variantInfos[name]; !exists {
			tag := c.nextVarTag
			c.nextVarTag++
			c.variantInfos[name] = variantInfo{tag: tag, arity: 0}
			for len(c.variantNames) <= tag {
				c.variantNames = append(c.variantNames, "")
			}
			c.variantNames[tag] = name
		}
	}

	// Register built-in interface methods
	c.registerBuiltinImpls()

	// First pass: allocate word IDs, register variants, effect ops, etc.
	for _, tl := range program.TopLevels {
		c.firstPass(tl)
	}

	// Finalize builtin impls
	c.finalizeBuiltinImpls()

	// Second pass: compile each definition
	for _, tl := range program.TopLevels {
		c.compileTopLevel(tl)
	}

	// Process deferred lambda bodies
	c.flushLambdaBodies()

	var entryID *int
	if id, ok := c.wordIDs["main"]; ok {
		entryID = &id
	}

	return &BytecodeModule{
		Words:         c.words,
		Strings:       c.strings,
		Rationals:     c.rationals,
		VariantNames:  c.variantNames,
		EntryWordID:   entryID,
		DispatchTable: c.dispatchTable,
	}
}

func (c *Compiler) firstPass(tl ast.TopLevel) {
	switch t := tl.(type) {
	case ast.TopDefinition:
		c.allocWordID(t.Name)
	case ast.TopTypeDecl:
		for _, v := range t.Variants {
			if existing, ok := c.variantInfos[v.Name]; ok {
				// Reuse existing tag, update arity from user definition
				c.variantInfos[v.Name] = variantInfo{tag: existing.tag, arity: len(v.Fields)}
			} else {
				tag := c.nextVarTag
				c.nextVarTag++
				c.variantInfos[v.Name] = variantInfo{tag: tag, arity: len(v.Fields)}
				for len(c.variantNames) <= tag {
					c.variantNames = append(c.variantNames, "")
				}
				c.variantNames[tag] = v.Name
			}
		}
		if len(t.Deriving) > 0 {
			c.registerDerivedImplIDs(t.Variants, t.Deriving)
		}
	case ast.TopEffectDecl:
		for _, op := range t.Ops {
			c.effectOps[op.Name] = len(op.Sig.Params)
		}
	case ast.TopInterfaceDecl:
		for _, m := range t.Methods {
			c.interfaceMethods[m.Name] = true
			c.interfaceMethodParam[m.Name] = len(m.Sig.Params)
		}
	case ast.TopImplBlock:
		typeTag := c.typeExprToTag(t.ForType)
		for _, m := range t.Methods {
			dispatchTag := typeTag
			if t.Interface == "From" && m.Name == "from" && len(t.TypeArgs) > 0 {
				dispatchTag = c.typeExprToTag(t.TypeArgs[0])
			}
			implWordName := m.Name + "$" + dispatchTag
			c.allocWordID(implWordName)
			if _, ok := c.dispatchTable[m.Name]; !ok {
				c.dispatchTable[m.Name] = make(map[string]int)
			}
			c.dispatchTable[m.Name][dispatchTag] = c.wordIDs[implWordName]
		}
		if t.Interface == "From" && len(t.TypeArgs) > 0 {
			sourceTag := c.typeExprToTag(t.TypeArgs[0])
			fromWordName := "from$" + sourceTag
			if fromWordID, ok := c.wordIDs[fromWordName]; ok {
				if _, ok := c.dispatchTable["into"]; !ok {
					c.dispatchTable["into"] = make(map[string]int)
				}
				c.dispatchTable["into"][sourceTag] = fromWordID
			}
		}
	case ast.TopUseDecl:
		for _, imp := range t.Imports {
			if imp.Alias != "" {
				if origID, ok := c.wordIDs[imp.Name]; ok {
					c.wordIDs[imp.Alias] = origID
				}
				if vi, ok := c.variantInfos[imp.Name]; ok {
					c.variantInfos[imp.Alias] = vi
				}
			}
		}
	}
}

func (c *Compiler) compileTopLevel(tl ast.TopLevel) {
	switch t := tl.(type) {
	case ast.TopDefinition:
		wordID := c.wordIDs[t.Name]
		e := &codeEmitter{}
		scope := newLocalScope()

		for _, p := range t.Sig.Params {
			scope.allocate(p.Name)
		}
		for i := len(t.Sig.Params) - 1; i >= 0; i-- {
			slot, _ := scope.get(t.Sig.Params[i].Name)
			e.emitU8(OpLOCAL_SET, slot)
		}

		c.compileExpr(t.Body, e, scope, true)
		e.emit(OpRET)

		c.words = append(c.words, BytecodeWord{
			Name: t.Name, WordID: wordID, Code: e.code,
			LocalCount: scope.count(), IsPublic: t.Pub,
		})

	case ast.TopTypeDecl:
		if len(t.Deriving) > 0 {
			c.compileDerivedImpls(t.Variants, t.Deriving)
		}

	case ast.TopImplBlock:
		typeTag := c.typeExprToTag(t.ForType)
		for _, m := range t.Methods {
			dispatchTag := typeTag
			if t.Interface == "From" && m.Name == "from" && len(t.TypeArgs) > 0 {
				dispatchTag = c.typeExprToTag(t.TypeArgs[0])
			}
			implWordName := m.Name + "$" + dispatchTag
			wordID := c.wordIDs[implWordName]
			e := &codeEmitter{}
			scope := newLocalScope()

			if lam, ok := m.Body.(ast.ExprLambda); ok {
				for _, p := range lam.Params {
					scope.allocate(p.Name)
				}
				for i := len(lam.Params) - 1; i >= 0; i-- {
					slot, _ := scope.get(lam.Params[i].Name)
					e.emitU8(OpLOCAL_SET, slot)
				}
				c.compileExpr(lam.Body, e, scope, true)
			} else {
				paramCount := 1
				if pc, ok := c.interfaceMethodParam[m.Name]; ok {
					paramCount = pc
				}
				for i := 0; i < paramCount; i++ {
					scope.allocate(fmt.Sprintf("__arg%d", i))
				}
				for i := paramCount - 1; i >= 0; i-- {
					slot, _ := scope.get(fmt.Sprintf("__arg%d", i))
					e.emitU8(OpLOCAL_SET, slot)
				}
				for i := 0; i < paramCount; i++ {
					slot, _ := scope.get(fmt.Sprintf("__arg%d", i))
					e.emitU8(OpLOCAL_GET, slot)
				}
				c.compileExpr(m.Body, e, scope, false)
				e.emit(OpTAIL_CALL_DYN)
			}
			e.emit(OpRET)

			c.words = append(c.words, BytecodeWord{
				Name: implWordName, WordID: wordID, Code: e.code,
				LocalCount: scope.count(), IsPublic: false,
			})
		}
	}
}

func (c *Compiler) compileExpr(expr ast.Expr, e *codeEmitter, scope *localScope, tail bool) {
	switch x := expr.(type) {
	case ast.ExprLiteral:
		c.compileLiteral(x.Value, e)

	case ast.ExprVar:
		if slot, ok := scope.get(x.Name); ok {
			e.emitU8(OpLOCAL_GET, slot)
		} else if vi, ok := c.variantInfos[x.Name]; ok && vi.arity == 0 {
			e.emitU8(OpUNION_NEW, vi.tag)
			e.code = append(e.code, 0) // arity = 0
		} else if wordID, ok := c.wordIDs[x.Name]; ok {
			e.emitU16(OpQUOTE, wordID)
		} else {
			strIdx := c.internString(x.Name)
			e.emitU16(OpPUSH_STR, strIdx)
		}

	case ast.ExprLet:
		c.compileExpr(x.Value, e, scope, false)
		slot := scope.allocate(x.Name)
		e.emitU8(OpLOCAL_SET, slot)
		if x.Body != nil {
			c.compileExpr(x.Body, e, scope, tail)
		} else {
			e.emit(OpPUSH_UNIT)
		}

	case ast.ExprIf:
		c.compileExpr(x.Cond, e, scope, false)
		elsePatch := e.emitJumpPlaceholder(OpJMP_UNLESS)
		c.compileExpr(x.Then, e, scope, tail)
		endPatch := e.emitJumpPlaceholder(OpJMP)
		e.patchJump(elsePatch)
		c.compileExpr(x.Else, e, scope, tail)
		e.patchJump(endPatch)

	case ast.ExprApply:
		c.compileApply(x, e, scope, tail)

	case ast.ExprLambda:
		c.compileLambda(x, e, scope)

	case ast.ExprMatch:
		c.compileMatch(x, e, scope, tail)

	case ast.ExprList:
		for _, el := range x.Elements {
			c.compileExpr(el, e, scope, false)
		}
		e.emitU8(OpLIST_NEW, len(x.Elements))

	case ast.ExprTuple:
		for _, el := range x.Elements {
			c.compileExpr(el, e, scope, false)
		}
		e.emitU8(OpTUPLE_NEW, len(x.Elements))

	case ast.ExprRecord:
		if x.Spread != nil {
			c.compileExpr(x.Spread, e, scope, false)
			for _, f := range x.Fields {
				c.compileExpr(f.Value, e, scope, false)
				e.emit(OpSWAP)
				fieldID := c.internString(f.Name)
				e.emitU16(OpRECORD_SET, fieldID)
			}
		} else {
			for _, f := range x.Fields {
				c.compileExpr(f.Value, e, scope, false)
			}
			e.emitU8(OpRECORD_NEW, len(x.Fields))
			for _, f := range x.Fields {
				nameIdx := c.internString(f.Name)
				e.code = append(e.code, byte((nameIdx>>8)&0xFF), byte(nameIdx&0xFF))
			}
		}

	case ast.ExprFieldAccess:
		if varExpr, ok := x.Object.(ast.ExprVar); ok {
			dottedName := varExpr.Name + "." + x.Field
			if _, ok := builtinOps[dottedName]; ok {
				strIdx := c.internString(dottedName)
				e.emitU16(OpPUSH_STR, strIdx)
				return
			}
			if dottedWordID, ok := c.wordIDs[dottedName]; ok {
				e.emitU16(OpQUOTE, dottedWordID)
				return
			}
		}
		c.compileExpr(x.Object, e, scope, false)
		fieldID := c.internString(x.Field)
		e.emitU16(OpRECORD_GET, fieldID)

	case ast.ExprRecordUpdate:
		c.compileExpr(x.Base, e, scope, false)
		for _, f := range x.Fields {
			c.compileExpr(f.Value, e, scope, false)
			e.emit(OpSWAP)
			fieldID := c.internString(f.Name)
			e.emitU16(OpRECORD_SET, fieldID)
		}

	case ast.ExprHandle:
		c.compileHandle(x, e, scope, tail)

	case ast.ExprPerform:
		c.compilePerform(x, e, scope)

	case ast.ExprBorrow:
		c.compileExpr(x.Expr, e, scope, tail)
	case ast.ExprClone:
		c.compileExpr(x.Expr, e, scope, tail)
	case ast.ExprDiscard:
		c.compileExpr(x.Expr, e, scope, false)
		e.emit(OpDROP)
		e.emit(OpPUSH_UNIT)

	case ast.ExprPipeline, ast.ExprInfix, ast.ExprUnary, ast.ExprDo,
		ast.ExprFor, ast.ExprRange, ast.ExprLetPattern:
		panic(fmt.Sprintf("compiler: unexpected sugared node %T — run desugar first", expr))

	default:
		panic(fmt.Sprintf("compiler: unknown node type %T", expr))
	}
}

func (c *Compiler) compileLiteral(lit ast.Literal, e *codeEmitter) {
	switch l := lit.(type) {
	case ast.LitInt:
		v := l.Value
		if v >= 0 && v <= 255 {
			e.emitU8(OpPUSH_INT, int(v))
		} else if v >= 0 && v <= 0xFFFF {
			e.emitU16(OpPUSH_INT16, int(v))
		} else {
			e.emitU32(OpPUSH_INT32, int(v))
		}
	case ast.LitRat:
		idx := c.internRational(l.Value)
		e.emitU32(OpPUSH_RAT, idx)
	case ast.LitBool:
		if l.Value {
			e.emit(OpPUSH_TRUE)
		} else {
			e.emit(OpPUSH_FALSE)
		}
	case ast.LitStr:
		idx := c.internString(l.Value)
		e.emitU16(OpPUSH_STR, idx)
	case ast.LitUnit:
		e.emit(OpPUSH_UNIT)
	}
}

func (c *Compiler) compileApply(expr ast.ExprApply, e *codeEmitter, scope *localScope, tail bool) {
	// Check if calling a resume continuation
	if varExpr, ok := expr.Fn.(ast.ExprVar); ok {
		if kSlot, isResume := c.resumeVars[varExpr.Name]; isResume {
			if len(expr.Args) > 0 {
				c.compileExpr(expr.Args[0], e, scope, false)
			} else {
				e.emit(OpPUSH_UNIT)
			}
			e.emitU8(OpLOCAL_GET, kSlot)
			e.emit(OpRESUME)
			return
		}
	}

	if varExpr, ok := expr.Fn.(ast.ExprVar); ok {
		name := varExpr.Name

		// Effect operation
		if arity, isEffect := c.effectOps[name]; isEffect {
			if _, isLocal := scope.get(name); !isLocal {
				_ = arity
				effectID := c.internString(name)
				for _, arg := range expr.Args {
					c.compileExpr(arg, e, scope, false)
				}
				e.emit(OpEFFECT_PERFORM)
				e.code = append(e.code, byte((effectID>>8)&0xFF), byte(effectID&0xFF))
				e.code = append(e.code, byte(len(expr.Args)&0xFF))
				return
			}
		}

		// Variant constructor
		if vi, ok := c.variantInfos[name]; ok && vi.arity > 0 {
			for _, arg := range expr.Args {
				c.compileExpr(arg, e, scope, false)
			}
			e.emitU8(OpUNION_NEW, vi.tag)
			e.code = append(e.code, byte(vi.arity&0xFF))
			return
		}

		// Interface method dispatch
		if c.interfaceMethods[name] {
			if _, isLocal := scope.get(name); !isLocal {
				for _, arg := range expr.Args {
					c.compileExpr(arg, e, scope, false)
				}
				methodIdx := c.internString(name)
				e.emit(OpDISPATCH)
				e.code = append(e.code, byte((methodIdx>>8)&0xFF), byte(methodIdx&0xFF))
				e.code = append(e.code, byte(len(expr.Args)&0xFF))
				return
			}
		}

		// Builtin direct opcode
		if ops, ok := builtinOps[name]; ok {
			for _, arg := range expr.Args {
				c.compileExpr(arg, e, scope, false)
			}
			e.code = append(e.code, ops...)
			return
		}

		// Known word
		if wordID, ok := c.wordIDs[name]; ok {
			for _, arg := range expr.Args {
				c.compileExpr(arg, e, scope, false)
			}
			if tail {
				e.emitU16(OpTAIL_CALL, wordID)
			} else {
				e.emitU16(OpCALL, wordID)
			}
			return
		}
	}

	// Dotted builtin calls (str.cat, http.get, etc.)
	if fa, ok := expr.Fn.(ast.ExprFieldAccess); ok {
		if varExpr, ok := fa.Object.(ast.ExprVar); ok {
			dottedName := varExpr.Name + "." + fa.Field
			if ops, ok := builtinOps[dottedName]; ok {
				for _, arg := range expr.Args {
					c.compileExpr(arg, e, scope, false)
				}
				e.code = append(e.code, ops...)
				return
			}
			if dottedWordID, ok := c.wordIDs[dottedName]; ok {
				for _, arg := range expr.Args {
					c.compileExpr(arg, e, scope, false)
				}
				if tail {
					e.emitU16(OpTAIL_CALL, dottedWordID)
				} else {
					e.emitU16(OpCALL, dottedWordID)
				}
				return
			}
		}
	}

	// Dynamic call
	for _, arg := range expr.Args {
		c.compileExpr(arg, e, scope, false)
	}
	c.compileExpr(expr.Fn, e, scope, false)
	if tail {
		e.emit(OpTAIL_CALL_DYN)
	} else {
		e.emit(OpCALL_DYN)
	}
}

func (c *Compiler) compileHandle(expr ast.ExprHandle, e *codeEmitter, scope *localScope, tail bool) {
	var returnArm *ast.HandlerArm
	var opArms []ast.HandlerArm
	for i := range expr.Arms {
		if expr.Arms[i].Name == "return" {
			arm := expr.Arms[i]
			returnArm = &arm
		} else {
			opArms = append(opArms, expr.Arms[i])
		}
	}

	handlerPatches := make([]int, 0, len(opArms))
	frameCount := len(opArms)
	if frameCount == 0 && returnArm != nil {
		frameCount = 1
	}

	for gi, arm := range opArms {
		armEffectID := c.internString(arm.Name)
		e.emit(OpHANDLE_PUSH)
		e.code = append(e.code, byte((armEffectID>>8)&0xFF), byte(armEffectID&0xFF))
		patch := len(e.code)
		e.code = append(e.code, 0, 0) // handler_offset placeholder
		e.code = append(e.code, byte(gi&0xFF))
		handlerPatches = append(handlerPatches, patch)
	}
	if len(opArms) == 0 && returnArm != nil {
		e.emit(OpHANDLE_PUSH)
		e.code = append(e.code, 0xFF, 0xFF) // sentinel
		e.code = append(e.code, 0, 0)       // handler_offset unused
		e.code = append(e.code, 0)           // groupIdx
	}

	c.compileExpr(expr.Expr, e, scope, false)

	for gi := 0; gi < frameCount; gi++ {
		e.emit(OpHANDLE_POP)
	}

	if returnArm != nil {
		returnScope := scope.child()
		if len(returnArm.Params) > 0 {
			slot := returnScope.allocate(returnArm.Params[0].Name)
			e.emitU8(OpLOCAL_SET, slot)
		}
		c.compileExpr(returnArm.Body, e, returnScope, tail)
	}

	endPatches := []int{e.emitJumpPlaceholder(OpJMP)}

	for gi, arm := range opArms {
		handlerOff := len(e.code)
		e.code[handlerPatches[gi]] = byte((handlerOff >> 8) & 0xFF)
		e.code[handlerPatches[gi]+1] = byte(handlerOff & 0xFF)

		armScope := scope.child()

		if arm.ResumeName != "" {
			kSlot := armScope.allocate(arm.ResumeName)
			e.emitU8(OpLOCAL_SET, kSlot)
			c.resumeVars[arm.ResumeName] = kSlot
		} else {
			e.emit(OpDROP)
		}

		opArity := len(arm.Params)
		if a, ok := c.effectOps[arm.Name]; ok {
			opArity = a
		}

		for i := len(arm.Params) - 1; i >= 0; i-- {
			slot := armScope.allocate(arm.Params[i].Name)
			if i >= opArity {
				e.emit(OpPUSH_UNIT)
			}
			e.emitU8(OpLOCAL_SET, slot)
		}

		c.compileExpr(arm.Body, e, armScope, false)
		endPatches = append(endPatches, e.emitJumpPlaceholder(OpJMP))

		if arm.ResumeName != "" {
			delete(c.resumeVars, arm.ResumeName)
		}
	}

	for _, p := range endPatches {
		e.patchJump(p)
	}
}

func (c *Compiler) compilePerform(expr ast.ExprPerform, e *codeEmitter, scope *localScope) {
	if apply, ok := expr.Expr.(ast.ExprApply); ok {
		if varExpr, ok := apply.Fn.(ast.ExprVar); ok {
			opName := varExpr.Name
			effectID := c.internString(opName)
			for _, arg := range apply.Args {
				c.compileExpr(arg, e, scope, false)
			}
			e.emit(OpEFFECT_PERFORM)
			e.code = append(e.code, byte((effectID>>8)&0xFF), byte(effectID&0xFF))
			e.code = append(e.code, byte(len(apply.Args)&0xFF))
			return
		}
	}
	c.compileExpr(expr.Expr, e, scope, false)
	e.emit(OpEFFECT_PERFORM)
	e.code = append(e.code, 0, 0)
	e.code = append(e.code, 0)
}

func (c *Compiler) compileLambda(expr ast.ExprLambda, e *codeEmitter, scope *localScope) {
	paramNames := make(map[string]bool, len(expr.Params))
	for _, p := range expr.Params {
		paramNames[p.Name] = true
	}
	var freeVars []string
	c.findFreeVars(expr.Body, paramNames, scope, &freeVars)

	lambdaName := fmt.Sprintf("__lambda_%d", c.nextWordID)
	lambdaWordID := c.allocWordID(lambdaName)

	c.lambdaBodies = append(c.lambdaBodies, deferredLambda{
		name: lambdaName, params: expr.Params,
		body: expr.Body, captures: freeVars,
	})

	if len(freeVars) == 0 {
		e.emitU16(OpQUOTE, lambdaWordID)
	} else {
		for _, v := range freeVars {
			if slot, ok := scope.get(v); ok {
				e.emitU8(OpLOCAL_GET, slot)
			}
		}
		e.code = append(e.code, OpCLOSURE,
			byte((lambdaWordID>>8)&0xFF), byte(lambdaWordID&0xFF),
			byte(len(freeVars)&0xFF))
	}
}

func (c *Compiler) findFreeVars(expr ast.Expr, bound map[string]bool, scope *localScope, free *[]string) {
	seen := make(map[string]bool)
	var collect func(ast.Expr, map[string]bool)
	collect = func(ex ast.Expr, localBound map[string]bool) {
		switch x := ex.(type) {
		case ast.ExprVar:
			if !localBound[x.Name] {
				if _, ok := scope.get(x.Name); ok && !seen[x.Name] {
					seen[x.Name] = true
					*free = append(*free, x.Name)
				}
			}
		case ast.ExprLiteral:
			// nop
		case ast.ExprLet:
			collect(x.Value, localBound)
			next := copySet(localBound)
			next[x.Name] = true
			if x.Body != nil {
				collect(x.Body, next)
			}
		case ast.ExprIf:
			collect(x.Cond, localBound)
			collect(x.Then, localBound)
			collect(x.Else, localBound)
		case ast.ExprApply:
			collect(x.Fn, localBound)
			for _, a := range x.Args {
				collect(a, localBound)
			}
		case ast.ExprLambda:
			inner := copySet(localBound)
			for _, p := range x.Params {
				inner[p.Name] = true
			}
			collect(x.Body, inner)
		case ast.ExprMatch:
			collect(x.Subject, localBound)
			for _, arm := range x.Arms {
				armBound := copySet(localBound)
				collectPatternVarNames(arm.Pattern, armBound)
				collect(arm.Body, armBound)
			}
		case ast.ExprList:
			for _, el := range x.Elements {
				collect(el, localBound)
			}
		case ast.ExprTuple:
			for _, el := range x.Elements {
				collect(el, localBound)
			}
		case ast.ExprRecord:
			for _, f := range x.Fields {
				collect(f.Value, localBound)
			}
			if x.Spread != nil {
				collect(x.Spread, localBound)
			}
		case ast.ExprFieldAccess:
			collect(x.Object, localBound)
		case ast.ExprHandle:
			collect(x.Expr, localBound)
			for _, arm := range x.Arms {
				armBound := copySet(localBound)
				for _, p := range arm.Params {
					armBound[p.Name] = true
				}
				if arm.ResumeName != "" {
					armBound[arm.ResumeName] = true
				}
				collect(arm.Body, armBound)
			}
		case ast.ExprPerform:
			collect(x.Expr, localBound)
		case ast.ExprRecordUpdate:
			collect(x.Base, localBound)
			for _, f := range x.Fields {
				collect(f.Value, localBound)
			}
		case ast.ExprBorrow:
			collect(x.Expr, localBound)
		case ast.ExprClone:
			collect(x.Expr, localBound)
		case ast.ExprDiscard:
			collect(x.Expr, localBound)
		}
	}
	collect(expr, bound)
}

func collectPatternVarNames(pat ast.Pattern, bound map[string]bool) {
	switch p := pat.(type) {
	case ast.PatVar:
		bound[p.Name] = true
	case ast.PatVariant:
		for _, a := range p.Args {
			collectPatternVarNames(a, bound)
		}
	case ast.PatTuple:
		for _, el := range p.Elements {
			collectPatternVarNames(el, bound)
		}
	case ast.PatRecord:
		for _, pf := range p.Fields {
			if pf.Pattern != nil {
				collectPatternVarNames(pf.Pattern, bound)
			} else {
				bound[pf.Name] = true
			}
		}
		if p.Rest != "" && p.Rest != "_" {
			bound[p.Rest] = true
		}
	}
}

func (c *Compiler) flushLambdaBodies() {
	for len(c.lambdaBodies) > 0 {
		pending := c.lambdaBodies
		c.lambdaBodies = nil

		for _, lam := range pending {
			wordID := c.wordIDs[lam.name]
			e := &codeEmitter{}
			bodyScope := newLocalScope()

			for _, p := range lam.params {
				bodyScope.allocate(p.Name)
			}
			for _, cap := range lam.captures {
				bodyScope.allocate(cap)
			}

			// Pop captures first (on top), then args
			for i := len(lam.captures) - 1; i >= 0; i-- {
				slot, _ := bodyScope.get(lam.captures[i])
				e.emitU8(OpLOCAL_SET, slot)
			}
			for i := len(lam.params) - 1; i >= 0; i-- {
				slot, _ := bodyScope.get(lam.params[i].Name)
				e.emitU8(OpLOCAL_SET, slot)
			}

			c.compileExpr(lam.body, e, bodyScope, true)
			e.emit(OpRET)

			c.words = append(c.words, BytecodeWord{
				Name: lam.name, WordID: wordID, Code: e.code,
				LocalCount: bodyScope.count(), IsPublic: false,
			})
		}
	}
}

func (c *Compiler) compileMatch(expr ast.ExprMatch, e *codeEmitter, scope *localScope, tail bool) {
	c.compileExpr(expr.Subject, e, scope, false)
	subjectSlot := scope.allocate("__match_subject")
	e.emitU8(OpLOCAL_SET, subjectSlot)

	var endPatches []int

	for i, arm := range expr.Arms {
		isLast := i == len(expr.Arms)-1
		armScope := scope.child()

		var nextArmPatch int
		hasNextPatch := false
		if !isLast {
			nextArmPatch = c.compilePatternTest(arm.Pattern, subjectSlot, e, armScope)
			hasNextPatch = true
		} else {
			c.compilePatternBind(arm.Pattern, subjectSlot, e, armScope)
		}

		c.compileExpr(arm.Body, e, armScope, tail)

		if !isLast {
			endPatches = append(endPatches, e.emitJumpPlaceholder(OpJMP))
		}
		if hasNextPatch {
			e.patchJump(nextArmPatch)
		}
	}

	for _, p := range endPatches {
		e.patchJump(p)
	}
}

func (c *Compiler) compilePatternTest(pat ast.Pattern, subjectSlot int, e *codeEmitter, scope *localScope) int {
	switch p := pat.(type) {
	case ast.PatWildcard, ast.PatVar:
		c.compilePatternBind(pat, subjectSlot, e, scope)
		e.emit(OpPUSH_TRUE)
		return e.emitJumpPlaceholder(OpJMP_UNLESS)

	case ast.PatLiteral:
		e.emitU8(OpLOCAL_GET, subjectSlot)
		c.compileLiteral(p.Value, e)
		e.emit(OpEQ)
		return e.emitJumpPlaceholder(OpJMP_UNLESS)

	case ast.PatVariant:
		e.emitU8(OpLOCAL_GET, subjectSlot)
		e.emit(OpVARIANT_TAG)
		vi, _ := c.variantInfos[p.Name]
		e.emitU8(OpPUSH_INT, vi.tag)
		e.emit(OpEQ)
		failPatch := e.emitJumpPlaceholder(OpJMP_UNLESS)

		for i, argPat := range p.Args {
			switch ap := argPat.(type) {
			case ast.PatVar:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpVARIANT_FIELD, i)
				slot := scope.allocate(ap.Name)
				e.emitU8(OpLOCAL_SET, slot)
			case ast.PatWildcard:
				// skip
			default:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpVARIANT_FIELD, i)
				tempSlot := scope.allocate(fmt.Sprintf("__variant_arg_%d_%d", subjectSlot, i))
				e.emitU8(OpLOCAL_SET, tempSlot)
				c.compilePatternBind(argPat, tempSlot, e, scope)
			}
		}
		return failPatch

	case ast.PatTuple:
		for i, elPat := range p.Elements {
			switch ep := elPat.(type) {
			case ast.PatVar:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpTUPLE_GET, i)
				slot := scope.allocate(ep.Name)
				e.emitU8(OpLOCAL_SET, slot)
			case ast.PatWildcard:
				// skip
			default:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpTUPLE_GET, i)
				tempSlot := scope.allocate(fmt.Sprintf("__tuple_el_%d_%d", subjectSlot, i))
				e.emitU8(OpLOCAL_SET, tempSlot)
				c.compilePatternBind(elPat, tempSlot, e, scope)
			}
		}
		e.emit(OpPUSH_TRUE)
		return e.emitJumpPlaceholder(OpJMP_UNLESS)

	case ast.PatRecord:
		for _, pf := range p.Fields {
			if pf.Pattern != nil {
				switch fp := pf.Pattern.(type) {
				case ast.PatVar:
					e.emitU8(OpLOCAL_GET, subjectSlot)
					fieldID := c.internString(pf.Name)
					e.emitU16(OpRECORD_GET, fieldID)
					slot := scope.allocate(fp.Name)
					e.emitU8(OpLOCAL_SET, slot)
				case ast.PatWildcard:
					// skip
				default:
					e.emitU8(OpLOCAL_GET, subjectSlot)
					fieldID := c.internString(pf.Name)
					e.emitU16(OpRECORD_GET, fieldID)
					tempSlot := scope.allocate(fmt.Sprintf("__rec_field_%d_%s", subjectSlot, pf.Name))
					e.emitU8(OpLOCAL_SET, tempSlot)
					c.compilePatternBind(pf.Pattern, tempSlot, e, scope)
				}
			} else {
				e.emitU8(OpLOCAL_GET, subjectSlot)
				fieldID := c.internString(pf.Name)
				e.emitU16(OpRECORD_GET, fieldID)
				slot := scope.allocate(pf.Name)
				e.emitU8(OpLOCAL_SET, slot)
			}
		}
		if p.Rest != "" && p.Rest != "_" {
			e.emitU8(OpLOCAL_GET, subjectSlot)
			e.emitU8(OpRECORD_REST, len(p.Fields))
			for _, pf := range p.Fields {
				nameIdx := c.internString(pf.Name)
				e.code = append(e.code, byte((nameIdx>>8)&0xFF), byte(nameIdx&0xFF))
			}
			slot := scope.allocate(p.Rest)
			e.emitU8(OpLOCAL_SET, slot)
		}
		e.emit(OpPUSH_TRUE)
		return e.emitJumpPlaceholder(OpJMP_UNLESS)
	}
	e.emit(OpPUSH_TRUE)
	return e.emitJumpPlaceholder(OpJMP_UNLESS)
}

func (c *Compiler) compilePatternBind(pat ast.Pattern, subjectSlot int, e *codeEmitter, scope *localScope) {
	switch p := pat.(type) {
	case ast.PatVar:
		e.emitU8(OpLOCAL_GET, subjectSlot)
		slot := scope.allocate(p.Name)
		e.emitU8(OpLOCAL_SET, slot)
	case ast.PatWildcard:
		// nop
	case ast.PatVariant:
		for i, argPat := range p.Args {
			switch ap := argPat.(type) {
			case ast.PatVar:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpVARIANT_FIELD, i)
				slot := scope.allocate(ap.Name)
				e.emitU8(OpLOCAL_SET, slot)
			case ast.PatWildcard:
				// skip
			default:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpVARIANT_FIELD, i)
				tempSlot := scope.allocate(fmt.Sprintf("__variant_bind_%d_%d", subjectSlot, i))
				e.emitU8(OpLOCAL_SET, tempSlot)
				c.compilePatternBind(argPat, tempSlot, e, scope)
			}
		}
	case ast.PatTuple:
		for i, elPat := range p.Elements {
			switch ep := elPat.(type) {
			case ast.PatVar:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpTUPLE_GET, i)
				slot := scope.allocate(ep.Name)
				e.emitU8(OpLOCAL_SET, slot)
			case ast.PatWildcard:
				// skip
			default:
				e.emitU8(OpLOCAL_GET, subjectSlot)
				e.emitU8(OpTUPLE_GET, i)
				tempSlot := scope.allocate(fmt.Sprintf("__tuple_bind_%d_%d", subjectSlot, i))
				e.emitU8(OpLOCAL_SET, tempSlot)
				c.compilePatternBind(elPat, tempSlot, e, scope)
			}
		}
	case ast.PatRecord:
		for _, pf := range p.Fields {
			if pf.Pattern != nil {
				switch fp := pf.Pattern.(type) {
				case ast.PatVar:
					e.emitU8(OpLOCAL_GET, subjectSlot)
					fieldID := c.internString(pf.Name)
					e.emitU16(OpRECORD_GET, fieldID)
					slot := scope.allocate(fp.Name)
					e.emitU8(OpLOCAL_SET, slot)
				case ast.PatWildcard:
					// skip
				default:
					e.emitU8(OpLOCAL_GET, subjectSlot)
					fieldID := c.internString(pf.Name)
					e.emitU16(OpRECORD_GET, fieldID)
					tempSlot := scope.allocate(fmt.Sprintf("__rec_bind_%d_%s", subjectSlot, pf.Name))
					e.emitU8(OpLOCAL_SET, tempSlot)
					c.compilePatternBind(pf.Pattern, tempSlot, e, scope)
				}
			} else {
				e.emitU8(OpLOCAL_GET, subjectSlot)
				fieldID := c.internString(pf.Name)
				e.emitU16(OpRECORD_GET, fieldID)
				slot := scope.allocate(pf.Name)
				e.emitU8(OpLOCAL_SET, slot)
			}
		}
		if p.Rest != "" && p.Rest != "_" {
			e.emitU8(OpLOCAL_GET, subjectSlot)
			e.emitU8(OpRECORD_REST, len(p.Fields))
			for _, pf := range p.Fields {
				nameIdx := c.internString(pf.Name)
				e.code = append(e.code, byte((nameIdx>>8)&0xFF), byte(nameIdx&0xFF))
			}
			slot := scope.allocate(p.Rest)
			e.emitU8(OpLOCAL_SET, slot)
		}
	case ast.PatLiteral:
		// nop (bind phase, no value to extract)
	}
}

func (c *Compiler) typeExprToTag(te ast.TypeExpr) string {
	switch t := te.(type) {
	case ast.TypeName:
		return t.Name
	case ast.TypeList:
		return "List"
	case ast.TypeTuple:
		return "Tuple"
	case ast.TypeRecord:
		return "Record"
	case ast.TypeGeneric:
		return t.Name
	default:
		return "?"
	}
}

func (c *Compiler) registerBuiltinImpls() {
	c.interfaceMethods["cmp"] = true
	c.interfaceMethodParam["cmp"] = 2

	for _, prim := range []string{"Int", "Rat", "Str"} {
		implName := "cmp$" + prim
		if _, ok := c.dispatchTable["cmp"]; !ok {
			c.dispatchTable["cmp"] = make(map[string]int)
		}
		c.dispatchTable["cmp"][prim] = c.wordIDs[implName]
	}

	c.interfaceMethods["default"] = true
	c.interfaceMethodParam["default"] = 1
	c.interfaceMethods["from"] = true
	c.interfaceMethodParam["from"] = 1
	c.interfaceMethods["into"] = true
	c.interfaceMethodParam["into"] = 1

	c.registerPrimitiveDefaultImpls()
}

func (c *Compiler) registerPrimitiveDefaultImpls() {
	emitDefault := func(typeTag string, emitValue func(e *codeEmitter)) {
		implName := "default$" + typeTag
		wordID := c.allocWordID(implName)
		e := &codeEmitter{}
		e.emit(OpDROP)
		emitValue(e)
		e.emit(OpRET)
		c.words = append(c.words, BytecodeWord{Name: implName, WordID: wordID, Code: e.code, IsPublic: false})
		if _, ok := c.dispatchTable["default"]; !ok {
			c.dispatchTable["default"] = make(map[string]int)
		}
		c.dispatchTable["default"][typeTag] = wordID
	}
	emitDefault("Int", func(e *codeEmitter) { e.emitU8(OpPUSH_INT, 0) })
	emitDefault("Rat", func(e *codeEmitter) { e.emitU32(OpPUSH_RAT, c.internRational(0.0)) })
	emitDefault("Bool", func(e *codeEmitter) { e.emit(OpPUSH_FALSE) })
	emitDefault("Str", func(e *codeEmitter) { e.emitU16(OpPUSH_STR, c.internString("")) })
	emitDefault("Unit", func(e *codeEmitter) { e.emit(OpPUSH_UNIT) })
}

func (c *Compiler) finalizeBuiltinImpls() {
	makeImplWord := func(name, typeTag string, opcodes []byte) {
		implName := name + "$" + typeTag
		if _, ok := c.wordIDs[implName]; ok {
			return
		}
		wordID := c.allocWordID(implName)
		code := make([]byte, len(opcodes)+1)
		copy(code, opcodes)
		code[len(opcodes)] = OpRET
		c.words = append(c.words, BytecodeWord{
			Name: implName, WordID: wordID, Code: code, IsPublic: false,
		})
		if _, ok := c.dispatchTable[name]; !ok {
			c.dispatchTable[name] = make(map[string]int)
		}
		dt := c.dispatchTable[name]
		if _, ok := dt[typeTag]; !ok {
			dt[typeTag] = wordID
		}
	}

	if c.interfaceMethods["show"] {
		for _, prim := range []string{"Int", "Rat", "Bool", "Str", "Unit"} {
			makeImplWord("show", prim, []byte{OpTO_STR})
		}
	}
	if c.interfaceMethods["eq"] {
		for _, prim := range []string{"Int", "Rat", "Bool", "Str"} {
			makeImplWord("eq", prim, []byte{OpEQ})
		}
	}
	if c.interfaceMethods["clone"] {
		for _, prim := range []string{"Int", "Rat", "Bool", "Str", "Unit"} {
			makeImplWord("clone", prim, nil)
		}
	}

	registerRecordBuiltin := func(methodName, builtinName string) {
		if !c.interfaceMethods[methodName] {
			return
		}
		wordID, ok := c.wordIDs[builtinName]
		if !ok {
			return
		}
		if _, ok := c.dispatchTable[methodName]; !ok {
			c.dispatchTable[methodName] = make(map[string]int)
		}
		dt := c.dispatchTable[methodName]
		if _, ok := dt["Record"]; !ok {
			dt["Record"] = wordID
		}
	}
	registerRecordBuiltin("show", "show$Record")
	registerRecordBuiltin("eq", "eq$Record")
	registerRecordBuiltin("clone", "clone$Record")
	registerRecordBuiltin("cmp", "cmp$Record")
	registerRecordBuiltin("default", "default$Record")

	registerCompositeBuiltin := func(methodName, typeTag, builtinName string) {
		if !c.interfaceMethods[methodName] {
			return
		}
		wordID, ok := c.wordIDs[builtinName]
		if !ok {
			return
		}
		if _, ok := c.dispatchTable[methodName]; !ok {
			c.dispatchTable[methodName] = make(map[string]int)
		}
		dt := c.dispatchTable[methodName]
		if _, ok := dt[typeTag]; !ok {
			dt[typeTag] = wordID
		}
	}
	for _, entry := range []struct{ method, typeTag, builtin string }{
		{"show", "List", "show$List"}, {"eq", "List", "eq$List"}, {"clone", "List", "clone$List"},
		{"cmp", "List", "cmp$List"},
		{"show", "Tuple", "show$Tuple"}, {"eq", "Tuple", "eq$Tuple"}, {"clone", "Tuple", "clone$Tuple"},
		{"cmp", "Tuple", "cmp$Tuple"},
		{"clone", "Ref", "clone$Ref"}, {"clone", "TVar", "clone$TVar"},
	} {
		registerCompositeBuiltin(entry.method, entry.typeTag, entry.builtin)
	}
}

func (c *Compiler) registerDerivedImplIDs(variants []ast.Variant, deriving []string) {
	registerImpl := func(methodName, variantName string) {
		implName := methodName + "$" + variantName
		if _, ok := c.wordIDs[implName]; ok {
			return
		}
		c.allocWordID(implName)
		if _, ok := c.dispatchTable[methodName]; !ok {
			c.dispatchTable[methodName] = make(map[string]int)
		}
		c.dispatchTable[methodName][variantName] = c.wordIDs[implName]
		c.interfaceMethods[methodName] = true
	}

	for _, iface := range deriving {
		switch iface {
		case "Show":
			for _, v := range variants {
				registerImpl("show", v.Name)
			}
		case "Eq":
			for _, v := range variants {
				registerImpl("eq", v.Name)
			}
		case "Clone":
			for _, v := range variants {
				registerImpl("clone", v.Name)
			}
		case "Ord":
			for _, v := range variants {
				registerImpl("cmp", v.Name)
			}
		case "Default":
			for _, v := range variants {
				if len(v.Fields) == 0 {
					registerImpl("default", v.Name)
					break
				}
			}
		}
	}
}

func (c *Compiler) compileDerivedImpls(variants []ast.Variant, deriving []string) {
	for _, iface := range deriving {
		switch iface {
		case "Show":
			for _, v := range variants {
				c.compileDerivedShow(v)
			}
		case "Eq":
			for _, v := range variants {
				c.compileDerivedEq(v)
			}
		case "Clone":
			for _, v := range variants {
				c.compileDerivedClone(v)
			}
		case "Ord":
			c.compileDerivedOrd(variants)
		case "Default":
			for _, v := range variants {
				if len(v.Fields) == 0 {
					c.compileDerivedDefault(v)
					break
				}
			}
		}
	}
}

func (c *Compiler) compileDerivedShow(v ast.Variant) {
	implName := "show$" + v.Name
	wordID := c.wordIDs[implName]
	e := &codeEmitter{}
	scope := newLocalScope()
	selfSlot := scope.allocate("self")
	e.emitU8(OpLOCAL_SET, selfSlot)

	if len(v.Fields) == 0 {
		e.emitU16(OpPUSH_STR, c.internString(v.Name))
	} else {
		e.emitU16(OpPUSH_STR, c.internString(v.Name+"("))
		for i := 0; i < len(v.Fields); i++ {
			if i > 0 {
				e.emitU16(OpPUSH_STR, c.internString(", "))
				e.emit(OpSTR_CAT)
			}
			e.emitU8(OpLOCAL_GET, selfSlot)
			e.emitU8(OpVARIANT_FIELD, i)
			showIdx := c.internString("show")
			e.emit(OpDISPATCH)
			e.code = append(e.code, byte((showIdx>>8)&0xFF), byte(showIdx&0xFF))
			e.code = append(e.code, 1)
			e.emit(OpSTR_CAT)
		}
		e.emitU16(OpPUSH_STR, c.internString(")"))
		e.emit(OpSTR_CAT)
	}
	e.emit(OpRET)
	c.words = append(c.words, BytecodeWord{
		Name: implName, WordID: wordID, Code: e.code,
		LocalCount: scope.count(), IsPublic: false,
	})
}

func (c *Compiler) compileDerivedEq(v ast.Variant) {
	implName := "eq$" + v.Name
	wordID := c.wordIDs[implName]
	e := &codeEmitter{}
	scope := newLocalScope()
	aSlot := scope.allocate("a")
	bSlot := scope.allocate("b")
	e.emitU8(OpLOCAL_SET, bSlot)
	e.emitU8(OpLOCAL_SET, aSlot)

	e.emitU8(OpLOCAL_GET, aSlot)
	e.emit(OpVARIANT_TAG)
	e.emitU8(OpLOCAL_GET, bSlot)
	e.emit(OpVARIANT_TAG)
	e.emit(OpEQ)
	tagMismatch := e.emitJumpPlaceholder(OpJMP_UNLESS)

	var falseJumps []int

	if len(v.Fields) == 0 {
		e.emit(OpPUSH_TRUE)
	} else {
		for i := 0; i < len(v.Fields); i++ {
			e.emitU8(OpLOCAL_GET, aSlot)
			e.emitU8(OpVARIANT_FIELD, i)
			e.emitU8(OpLOCAL_GET, bSlot)
			e.emitU8(OpVARIANT_FIELD, i)
			eqIdx := c.internString("eq")
			e.emit(OpDISPATCH)
			e.code = append(e.code, byte((eqIdx>>8)&0xFF), byte(eqIdx&0xFF))
			e.code = append(e.code, 2)
			if i < len(v.Fields)-1 {
				falseJumps = append(falseJumps, e.emitJumpPlaceholder(OpJMP_UNLESS))
			}
		}
	}
	endPatch := e.emitJumpPlaceholder(OpJMP)

	e.patchJump(tagMismatch)
	for _, fj := range falseJumps {
		e.patchJump(fj)
	}
	e.emit(OpPUSH_FALSE)

	e.patchJump(endPatch)
	e.emit(OpRET)
	c.words = append(c.words, BytecodeWord{
		Name: implName, WordID: wordID, Code: e.code,
		LocalCount: scope.count(), IsPublic: false,
	})
}

func (c *Compiler) compileDerivedClone(v ast.Variant) {
	implName := "clone$" + v.Name
	wordID := c.wordIDs[implName]
	e := &codeEmitter{}
	scope := newLocalScope()
	selfSlot := scope.allocate("self")
	e.emitU8(OpLOCAL_SET, selfSlot)

	if len(v.Fields) == 0 {
		e.emitU8(OpLOCAL_GET, selfSlot)
	} else {
		vi := c.variantInfos[v.Name]
		for i := 0; i < len(v.Fields); i++ {
			e.emitU8(OpLOCAL_GET, selfSlot)
			e.emitU8(OpVARIANT_FIELD, i)
			cloneIdx := c.internString("clone")
			e.emit(OpDISPATCH)
			e.code = append(e.code, byte((cloneIdx>>8)&0xFF), byte(cloneIdx&0xFF))
			e.code = append(e.code, 1)
		}
		e.emitU8(OpUNION_NEW, vi.tag)
		e.code = append(e.code, byte(len(v.Fields)&0xFF))
	}
	e.emit(OpRET)
	c.words = append(c.words, BytecodeWord{
		Name: implName, WordID: wordID, Code: e.code,
		LocalCount: scope.count(), IsPublic: false,
	})
}

func (c *Compiler) compileDerivedOrd(variants []ast.Variant) {
	for _, v := range variants {
		implName := "cmp$" + v.Name
		wordID := c.wordIDs[implName]
		e := &codeEmitter{}
		scope := newLocalScope()
		aSlot := scope.allocate("a")
		bSlot := scope.allocate("b")
		e.emitU8(OpLOCAL_SET, bSlot)
		e.emitU8(OpLOCAL_SET, aSlot)

		aTagSlot := scope.allocate("atag")
		bTagSlot := scope.allocate("btag")

		e.emitU8(OpLOCAL_GET, aSlot)
		e.emit(OpVARIANT_TAG)
		e.emitU8(OpLOCAL_SET, aTagSlot)

		e.emitU8(OpLOCAL_GET, bSlot)
		e.emit(OpVARIANT_TAG)
		e.emitU8(OpLOCAL_SET, bTagSlot)

		var doneJumps []int

		e.emitU8(OpLOCAL_GET, aTagSlot)
		e.emitU8(OpLOCAL_GET, bTagSlot)
		e.emit(OpLT)
		notLt := e.emitJumpPlaceholder(OpJMP_UNLESS)
		e.emitU8(OpUNION_NEW, c.variantInfos["Lt"].tag)
		e.code = append(e.code, 0)
		doneJumps = append(doneJumps, e.emitJumpPlaceholder(OpJMP))
		e.patchJump(notLt)

		e.emitU8(OpLOCAL_GET, aTagSlot)
		e.emitU8(OpLOCAL_GET, bTagSlot)
		e.emit(OpGT)
		notGt := e.emitJumpPlaceholder(OpJMP_UNLESS)
		e.emitU8(OpUNION_NEW, c.variantInfos["Gt"].tag)
		e.code = append(e.code, 0)
		doneJumps = append(doneJumps, e.emitJumpPlaceholder(OpJMP))
		e.patchJump(notGt)

		if len(v.Fields) == 0 {
			e.emitU8(OpUNION_NEW, c.variantInfos["Eq_"].tag)
			e.code = append(e.code, 0)
		} else {
			resultSlot := scope.allocate("result")
			for i := 0; i < len(v.Fields); i++ {
				e.emitU8(OpLOCAL_GET, aSlot)
				e.emitU8(OpVARIANT_FIELD, i)
				e.emitU8(OpLOCAL_GET, bSlot)
				e.emitU8(OpVARIANT_FIELD, i)
				cmpIdx := c.internString("cmp")
				e.emit(OpDISPATCH)
				e.code = append(e.code, byte((cmpIdx>>8)&0xFF), byte(cmpIdx&0xFF))
				e.code = append(e.code, 2)
				if i < len(v.Fields)-1 {
					e.emitU8(OpLOCAL_SET, resultSlot)
					e.emitU8(OpLOCAL_GET, resultSlot)
					e.emit(OpVARIANT_TAG)
					e.emitU8(OpPUSH_INT, c.variantInfos["Eq_"].tag)
					e.emit(OpEQ)
					isEq := e.emitJumpPlaceholder(OpJMP_IF)
					e.emitU8(OpLOCAL_GET, resultSlot)
					doneJumps = append(doneJumps, e.emitJumpPlaceholder(OpJMP))
					e.patchJump(isEq)
				}
			}
		}

		for _, dj := range doneJumps {
			e.patchJump(dj)
		}
		e.emit(OpRET)
		c.words = append(c.words, BytecodeWord{
			Name: implName, WordID: wordID, Code: e.code,
			LocalCount: scope.count(), IsPublic: false,
		})
	}
}

func (c *Compiler) compileDerivedDefault(v ast.Variant) {
	implName := "default$" + v.Name
	wordID := c.wordIDs[implName]
	e := &codeEmitter{}
	e.emit(OpDROP)
	vi := c.variantInfos[v.Name]
	e.emitU8(OpUNION_NEW, vi.tag)
	e.code = append(e.code, 0)
	e.emit(OpRET)
	c.words = append(c.words, BytecodeWord{
		Name: implName, WordID: wordID, Code: e.code, IsPublic: false,
	})
}

func copySet(s map[string]bool) map[string]bool {
	c := make(map[string]bool, len(s))
	for k, v := range s {
		c[k] = v
	}
	return c
}

// CompileProgram is the public API: compiles a program to bytecode.
func CompileProgram(program *ast.Program) *BytecodeModule {
	c := NewCompiler()
	return c.Compile(program)
}
