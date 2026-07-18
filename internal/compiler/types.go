package compiler

import "github.com/dalurness/clank/internal/token"

// LineEntry maps a bytecode offset to the source location of the
// expression that produced the code starting there. Entries are in
// ascending PC order; a run extends until the next entry.
type LineEntry struct {
	PC  int
	Loc token.Loc
}

// BytecodeWord is a single compiled word (function) in the bytecode module.
type BytecodeWord struct {
	Name       string
	WordID     int
	Code       []byte
	LocalCount int
	// NumParams is the number of declared parameters, used by the VM to
	// adapt dynamic calls (partial and over-application). -1 = unknown:
	// the VM then assumes the caller pushed exactly what the word pops.
	NumParams int
	IsPublic  bool
	Lines     []LineEntry // pc → source location, for runtime error reporting
}

// LocAt returns the source location of the code at bytecode offset pc,
// or a zero Loc when the word has no line information.
func (w *BytecodeWord) LocAt(pc int) token.Loc {
	var loc token.Loc
	for _, e := range w.Lines {
		if e.PC > pc {
			break
		}
		loc = e.Loc
	}
	return loc
}

// BytecodeModule is the in-memory representation of a compiled program.
type BytecodeModule struct {
	Words         []BytecodeWord
	Strings       []string
	Rationals     []float64
	VariantNames  []string            // maps variant tag → name
	EntryWordID   *int                // nil if no main
	DispatchTable map[string]map[string]int // method → typeTag → wordID
}
