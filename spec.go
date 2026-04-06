// Package clank provides the embedded language specification.
package clank

import _ "embed"

//go:embed SPEC.md
var Spec string
