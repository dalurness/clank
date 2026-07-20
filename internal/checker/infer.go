package checker

import (
	"sort"

	"github.com/dalurness/clank/internal/ast"
)

// InferredInfo is the checker's answer for a single definition: the
// resolved type of its body (rendered with readable typevar names) and
// the effects the body performs. Effects include those from builtin calls
// (io from print, io+exn from fs.read, async from spawn, ...) as well as
// user-declared operations, propagated transitively through calls and
// effect-polymorphic higher-order functions.
type InferredInfo struct {
	Type    string
	Effects []string
}

// inferCapture threads a capture request through typeCheckImpl. When the
// second pass reaches the named definition, its body type and performed
// effects are recorded; rendering happens after the full check so all
// substitutions are final. The return-type check is skipped for the
// captured definition — eval wraps expressions as
// `main : () -> <> auto = <expr>` and the placeholder `auto` return
// would otherwise fail unification against every real body type.
type inferCapture struct {
	defName  string
	found    bool
	bodyType Type
	effects  map[string]bool
	typeStr  string
}

// InferDefinition type-checks the program like TypeCheckWithResolvers and
// additionally reports the inferred body type and performed effects of
// the named definition. Returns nil info if no definition with that name
// exists. The returned errors are the full check's diagnostics (warnings
// included) and should still be inspected — the info is only trustworthy
// when there are no hard errors.
func InferDefinition(program *ast.Program, typeResolver ModuleTypeResolver, aliasResolver ModuleEffectAliasResolver, defName string) (*InferredInfo, []TypeError) {
	capture := &inferCapture{defName: defName}
	errors := typeCheckImpl(program, typeResolver, aliasResolver, capture)
	if !capture.found {
		return nil, errors
	}
	effects := make([]string, 0, len(capture.effects))
	for e := range capture.effects {
		effects = append(effects, e)
	}
	sort.Strings(effects)
	return &InferredInfo{Type: capture.typeStr, Effects: effects}, errors
}
