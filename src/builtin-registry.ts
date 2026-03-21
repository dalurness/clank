// Shared builtin registry — single source of truth for checker, runtime, and docs.
// Each entry defines the name, type signature, and description of a builtin.

import type { Type } from "./types.js";
import { tInt, tBool, tStr, tUnit, tFn, tList } from "./types.js";

const tAny: Type = { tag: "t-generic", name: "?", args: [] };
const tAnyList = tList(tAny);

export type BuiltinEntry = {
  name: string;
  type: Type;
  description: string;
};

export const BUILTIN_REGISTRY: BuiltinEntry[] = [
  // Arithmetic
  { name: "add",    type: tFn(tInt, tFn(tInt, tInt)), description: "Add two numbers" },
  { name: "sub",    type: tFn(tInt, tFn(tInt, tInt)), description: "Subtract second from first" },
  { name: "mul",    type: tFn(tInt, tFn(tInt, tInt)), description: "Multiply two numbers" },
  { name: "div",    type: tFn(tInt, tFn(tInt, tInt)), description: "Integer division" },
  { name: "mod",    type: tFn(tInt, tFn(tInt, tInt)), description: "Modulo (remainder)" },

  // Comparison
  { name: "eq",     type: tFn(tAny, tFn(tAny, tBool)), description: "Structural equality" },
  { name: "neq",    type: tFn(tAny, tFn(tAny, tBool)), description: "Structural inequality" },
  { name: "lt",     type: tFn(tInt, tFn(tInt, tBool)), description: "Less than" },
  { name: "gt",     type: tFn(tInt, tFn(tInt, tBool)), description: "Greater than" },
  { name: "lte",    type: tFn(tInt, tFn(tInt, tBool)), description: "Less than or equal" },
  { name: "gte",    type: tFn(tInt, tFn(tInt, tBool)), description: "Greater than or equal" },

  // Logic
  { name: "not",    type: tFn(tBool, tBool), description: "Boolean negation" },
  { name: "negate", type: tFn(tInt, tInt), description: "Numeric negation" },
  { name: "and",    type: tFn(tBool, tFn(tBool, tBool)), description: "Boolean AND" },
  { name: "or",     type: tFn(tBool, tFn(tBool, tBool)), description: "Boolean OR" },

  // Strings
  { name: "str.cat", type: tFn(tStr, tFn(tStr, tStr)), description: "Concatenate two strings" },
  { name: "show",   type: tFn(tAny, tStr), description: "Convert any value to its string representation" },
  { name: "print",  type: tFn(tStr, tUnit), description: "Print a string to stdout" },

  // List operations
  { name: "len",    type: tFn(tAnyList, tInt), description: "Length of a list" },
  { name: "head",   type: tFn(tAnyList, tAny), description: "First element of a list (errors on empty)" },
  { name: "tail",   type: tFn(tAnyList, tAnyList), description: "All elements except the first (errors on empty)" },
  { name: "cons",   type: tFn(tAny, tFn(tAnyList, tAnyList)), description: "Prepend an element to a list" },
  { name: "cat",    type: tFn(tAnyList, tFn(tAnyList, tAnyList)), description: "Concatenate two lists" },
  { name: "rev",    type: tFn(tAnyList, tAnyList), description: "Reverse a list" },
  { name: "get",    type: tFn(tAnyList, tFn(tInt, tAny)), description: "Get element at index (errors on out of bounds)" },
  { name: "map",    type: tFn(tAnyList, tFn(tFn(tAny, tAny), tAnyList)), description: "Apply a function to each element" },
  { name: "filter", type: tFn(tAnyList, tFn(tFn(tAny, tBool), tAnyList)), description: "Keep elements where predicate returns true" },
  { name: "fold",   type: tFn(tAnyList, tFn(tAny, tFn(tFn(tAny, tFn(tAny, tAny)), tAny))), description: "Left fold with accumulator" },
  { name: "flat-map", type: tFn(tAnyList, tFn(tFn(tAny, tAnyList), tAnyList)), description: "Map each element to a list, then flatten" },
  { name: "range",  type: tFn(tInt, tFn(tInt, tAnyList)), description: "Generate list of integers from start to end (inclusive)" },
  { name: "zip",    type: tFn(tAnyList, tFn(tAnyList, tAnyList)), description: "Zip two lists into list of tuples" },
  { name: "fst",    type: tFn(tAny, tAny), description: "First element of a tuple" },
  { name: "snd",    type: tFn(tAny, tAny), description: "Second element of a tuple" },
  { name: "tuple.get", type: tFn(tAny, tFn(tInt, tAny)), description: "Get tuple element by index" },
  { name: "split",  type: tFn(tStr, tFn(tStr, tAnyList)), description: "Split string by separator" },
  { name: "join",   type: tFn(tAnyList, tFn(tStr, tStr)), description: "Join list of strings with separator" },
  { name: "trim",   type: tFn(tStr, tStr), description: "Trim whitespace from both ends of a string" },

  // Built-in effects
  { name: "raise",  type: tFn(tAny, tAny), description: "Raise an exception" },
  { name: "exn",    type: tAny, description: "Exception effect marker" },
  { name: "io",     type: tAny, description: "IO effect marker" },
];
