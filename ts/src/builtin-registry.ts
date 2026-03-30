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

  // Iterator / Streaming I/O
  { name: "iter-new",       type: tFn(tAny, tFn(tAny, tAny)), description: "Create iterator from generator closure + cleanup function" },
  { name: "iter-next",      type: tFn(tAny, tAny), description: "Advance iterator, return next value or raise IterDone" },
  { name: "iter-close",     type: tFn(tAny, tUnit), description: "Close iterator, run cleanup" },
  { name: "next",           type: tFn(tAny, tAny), description: "Advance iterator (alias for iter-next)" },
  { name: "close-iter",     type: tFn(tAny, tUnit), description: "Close iterator (alias for iter-close)" },
  { name: "collect",        type: tFn(tAny, tAnyList), description: "Collect iterator into list" },
  { name: "drain",          type: tFn(tAny, tUnit), description: "Consume iterator, discard values" },
  { name: "iter-of",        type: tFn(tAnyList, tAny), description: "Convert list to iterator" },
  { name: "iter-range",     type: tFn(tInt, tFn(tInt, tAny)), description: "Lazy range iterator [start, end)" },
  { name: "iter.of",        type: tFn(tAnyList, tAny), description: "Convert list to iterator" },
  { name: "iter.range",     type: tFn(tInt, tFn(tInt, tAny)), description: "Lazy range iterator [start, end)" },
  { name: "iter.collect",   type: tFn(tAny, tAnyList), description: "Collect iterator into list" },
  { name: "iter.map",       type: tFn(tAny, tFn(tFn(tAny, tAny), tAny)), description: "Lazy map over iterator" },
  { name: "iter.filter",    type: tFn(tAny, tFn(tFn(tAny, tBool), tAny)), description: "Lazy filter over iterator" },
  { name: "iter.take",      type: tFn(tAny, tFn(tInt, tAny)), description: "Take first n elements" },
  { name: "iter.drop",      type: tFn(tAny, tFn(tInt, tAny)), description: "Drop first n elements" },
  { name: "iter.fold",      type: tFn(tAny, tFn(tAny, tFn(tFn(tAny, tFn(tAny, tAny)), tAny))), description: "Left fold over iterator" },
  { name: "iter.count",     type: tFn(tAny, tInt), description: "Count elements in iterator" },
  { name: "iter.sum",       type: tFn(tAny, tInt), description: "Sum elements in iterator" },
  { name: "iter.any",       type: tFn(tAny, tFn(tFn(tAny, tBool), tBool)), description: "Any element matches predicate" },
  { name: "iter.all",       type: tFn(tAny, tFn(tFn(tAny, tBool), tBool)), description: "All elements match predicate" },
  { name: "iter.find",      type: tFn(tAny, tFn(tFn(tAny, tBool), tAny)), description: "Find first matching element" },
  { name: "iter.first",     type: tFn(tAny, tAny), description: "First element or None" },
  { name: "iter.last",      type: tFn(tAny, tAny), description: "Last element or None" },
  { name: "iter.each",      type: tFn(tAny, tFn(tFn(tAny, tUnit), tUnit)), description: "Execute function for each element" },
  { name: "iter.drain",     type: tFn(tAny, tUnit), description: "Consume iterator, discard values" },
  { name: "iter.enumerate",  type: tFn(tAny, tAny), description: "Pair each element with 0-based index" },
  { name: "iter.chain",     type: tFn(tAny, tFn(tAny, tAny)), description: "Concatenate two iterators" },
  { name: "iter.zip",       type: tFn(tAny, tFn(tAny, tAny)), description: "Zip two iterators" },
  { name: "iter.take-while", type: tFn(tAny, tFn(tFn(tAny, tBool), tAny)), description: "Take while predicate holds" },
  { name: "iter.drop-while", type: tFn(tAny, tFn(tFn(tAny, tBool), tAny)), description: "Drop while predicate holds" },
  { name: "iter.flatmap",   type: tFn(tAny, tFn(tFn(tAny, tAny), tAny)), description: "Map then flatten iterators" },
  { name: "iter.join",      type: tFn(tAny, tFn(tStr, tStr)), description: "Join iterator of strings with separator" },
  { name: "iter.repeat",    type: tFn(tAny, tAny), description: "Infinite repetition of a value" },
  { name: "iter.once",      type: tFn(tAny, tAny), description: "Single-element iterator" },
  { name: "iter.empty",     type: tFn(tUnit, tAny), description: "Empty iterator" },
  { name: "iter.unfold",    type: tFn(tAny, tFn(tFn(tAny, tAny), tAny)), description: "Build iterator from seed function" },
  { name: "iter.scan",      type: tFn(tAny, tFn(tAny, tFn(tFn(tAny, tFn(tAny, tAny)), tAny))), description: "Running fold producing iterator" },
  { name: "iter.dedup",     type: tFn(tAny, tAny), description: "Remove consecutive duplicates" },
  { name: "iter.chunk",     type: tFn(tAny, tFn(tInt, tAny)), description: "Group into chunks of n" },
  { name: "iter.window",    type: tFn(tAny, tFn(tInt, tAny)), description: "Sliding window of size n" },
  { name: "iter.intersperse", type: tFn(tAny, tFn(tAny, tAny)), description: "Insert separator between elements" },
  { name: "iter.cycle",     type: tFn(tAny, tAny), description: "Infinite cycle over iterator" },
  { name: "iter.nth",       type: tFn(tAny, tFn(tInt, tAny)), description: "Element at index n" },
  { name: "iter.min",       type: tFn(tAny, tAny), description: "Minimum element" },
  { name: "iter.max",       type: tFn(tAny, tAny), description: "Maximum element" },
  { name: "iter.generate",  type: tFn(tFn(tUnit, tAny), tAny), description: "Iterator from callable" },

  // Streaming I/O
  { name: "fs.stream-lines",   type: tFn(tStr, tAny), description: "Read file, return iterator of lines" },
  { name: "http.stream-lines", type: tFn(tStr, tAny), description: "HTTP GET url, return iterator of response body lines" },
  { name: "proc.stream",       type: tFn(tStr, tFn(tAnyList, tAny)), description: "Run command, return iterator of stdout lines" },
  { name: "io.stdin-lines",    type: tFn(tUnit, tAny), description: "Return iterator of stdin lines" },

  // Runtime-dispatched for-loop builtins (internal, used by desugarer)
  { name: "__for_each",   type: tFn(tAny, tFn(tAny, tAny)), description: "For-each dispatch: List→map, Iter→iter.each" },
  { name: "__for_filter", type: tFn(tAny, tFn(tAny, tAny)), description: "For-filter dispatch: List→filter, Iter→iter.filter" },
  { name: "__for_fold",   type: tFn(tAny, tFn(tAny, tFn(tAny, tAny))), description: "For-fold dispatch: List→fold, Iter→iter.fold" },

  // Channel-iterator bridge
  { name: "iter-recv",  type: tFn(tAny, tAny), description: "Create iterator from channel receiver" },
  { name: "iter-send",  type: tFn(tAny, tFn(tAny, tUnit)), description: "Consume iterator, send each value through sender" },
  { name: "iter-spawn", type: tFn(tFn(tAny, tUnit), tAny), description: "Spawn producer with sender, return iterator of produced values" },

  // Async / concurrency
  { name: "async",          type: tAny, description: "Async effect marker" },
  { name: "spawn",          type: tFn(tAny, tAny), description: "Spawn a concurrent task, returns a Future" },
  { name: "await",          type: tFn(tAny, tAny), description: "Await a Future's result" },
  { name: "task-group",     type: tFn(tAny, tAny), description: "Run body in a structured task group" },
  { name: "task-yield",     type: tFn(tUnit, tUnit), description: "Cooperative yield to scheduler" },
  { name: "sleep",          type: tFn(tInt, tUnit), description: "Sleep for N milliseconds" },
  { name: "is-cancelled",   type: tFn(tUnit, tBool), description: "Check if current task is cancelled" },
  { name: "check-cancel",   type: tFn(tUnit, tUnit), description: "Raise if current task is cancelled" },
  { name: "shield",         type: tFn(tAny, tAny), description: "Run body in uncancellable section" },

  // Channels
  { name: "channel",        type: tFn(tInt, tAny), description: "Create bounded channel, returns (Sender, Receiver)" },
  { name: "send",           type: tFn(tAny, tFn(tAny, tUnit)), description: "Send value through channel" },
  { name: "recv",           type: tFn(tAny, tAny), description: "Receive value from channel (blocking)" },
  { name: "try-recv",       type: tFn(tAny, tAny), description: "Non-blocking receive, returns Option" },
  { name: "close-sender",   type: tFn(tAny, tUnit), description: "Close channel sender end" },
  { name: "close-receiver", type: tFn(tAny, tUnit), description: "Close channel receiver end" },

  // ── Tier 2: HTTP Client (std.http) ──
  { name: "http.get",   type: tFn(tStr, tAny), description: "GET request by URL, returns HttpRes" },
  { name: "http.post",  type: tFn(tStr, tFn(tStr, tAny)), description: "POST request: url body" },
  { name: "http.put",   type: tFn(tStr, tFn(tStr, tAny)), description: "PUT request: url body" },
  { name: "http.del",   type: tFn(tStr, tAny), description: "DELETE request by URL" },
  { name: "http.patch", type: tFn(tStr, tFn(tStr, tAny)), description: "PATCH request: url body" },
  { name: "http.req",   type: tFn(tAny, tAny), description: "Send custom HttpReq, returns HttpRes" },
  { name: "http.hdr",   type: tFn(tAny, tFn(tStr, tFn(tStr, tAny))), description: "Add header to HttpReq" },
  { name: "http.json",  type: tFn(tAny, tAny), description: "Parse HttpRes body as JSON" },
  { name: "http.ok?",   type: tFn(tAny, tBool), description: "Status in 200-299" },

  // ── Tier 2: HTTP Server (std.srv) ──
  { name: "srv.new",    type: tFn(tUnit, tAnyList), description: "Empty route list" },
  { name: "srv.get",    type: tFn(tAnyList, tFn(tStr, tFn(tAny, tAnyList))), description: "Add GET route" },
  { name: "srv.post",   type: tFn(tAnyList, tFn(tStr, tFn(tAny, tAnyList))), description: "Add POST route" },
  { name: "srv.put",    type: tFn(tAnyList, tFn(tStr, tFn(tAny, tAnyList))), description: "Add PUT route" },
  { name: "srv.del",    type: tFn(tAnyList, tFn(tStr, tFn(tAny, tAnyList))), description: "Add DELETE route" },
  { name: "srv.start",  type: tFn(tAnyList, tFn(tInt, tAny)), description: "Start server on port" },
  { name: "srv.stop",   type: tFn(tAny, tUnit), description: "Stop server" },
  { name: "srv.res",    type: tFn(tInt, tFn(tStr, tAny)), description: "Create response: status body" },
  { name: "srv.json",   type: tFn(tInt, tFn(tAny, tAny)), description: "JSON response: status json" },
  { name: "srv.hdr",    type: tFn(tAny, tFn(tStr, tFn(tStr, tAny))), description: "Add header to response" },
  { name: "srv.mw",     type: tFn(tAnyList, tFn(tAny, tAnyList)), description: "Add middleware" },

  // ── Tier 2: CSV (std.csv) ──
  { name: "csv.dec",    type: tFn(tStr, tAnyList), description: "Parse CSV string to rows" },
  { name: "csv.enc",    type: tFn(tAnyList, tStr), description: "Encode rows to CSV string" },
  { name: "csv.decf",   type: tFn(tStr, tAnyList), description: "Parse CSV file" },
  { name: "csv.encf",   type: tFn(tStr, tFn(tAnyList, tUnit)), description: "Write CSV file: path rows" },
  { name: "csv.hdr",    type: tFn(tAnyList, tAnyList), description: "Extract header row" },
  { name: "csv.rows",   type: tFn(tAnyList, tAnyList), description: "Extract data rows (skip header)" },
  { name: "csv.maps",   type: tFn(tAnyList, tAnyList), description: "Rows as maps keyed by header" },
  { name: "csv.opts",   type: tFn(tAny, tFn(tStr, tAnyList)), description: "Parse with custom CsvOpts" },

  // ── Tier 2: Process (std.proc) ──
  { name: "proc.run",   type: tFn(tStr, tFn(tAnyList, tAny)), description: "Run command with args, returns ProcResult" },
  { name: "proc.sh",    type: tFn(tStr, tAny), description: "Run shell command string" },
  { name: "proc.ok",    type: tFn(tStr, tFn(tAnyList, tStr)), description: "Run, throw if non-zero, return stdout" },
  { name: "proc.pipe",  type: tFn(tStr, tFn(tAnyList, tFn(tStr, tAny))), description: "Run with stdin: cmd args stdin" },
  { name: "proc.bg",    type: tFn(tStr, tFn(tAnyList, tAny)), description: "Start background process" },
  { name: "proc.wait",  type: tFn(tAny, tAny), description: "Wait for background process" },
  { name: "proc.kill",  type: tFn(tAny, tUnit), description: "Kill background process" },
  { name: "proc.exit",  type: tFn(tInt, tUnit), description: "Exit current process with code" },
  { name: "proc.pid",   type: tFn(tUnit, tInt), description: "Current process ID" },

  // ── Tier 2: DateTime (std.dt) ──
  { name: "dt.now",     type: tFn(tUnit, tAny), description: "Current datetime UTC" },
  { name: "dt.unix",    type: tFn(tUnit, tInt), description: "Current unix timestamp (seconds)" },
  { name: "dt.from",    type: tFn(tInt, tAny), description: "Datetime from unix timestamp" },
  { name: "dt.to",      type: tFn(tAny, tInt), description: "Datetime to unix timestamp" },
  { name: "dt.parse",   type: tFn(tStr, tFn(tStr, tAny)), description: "Parse datetime: value format" },
  { name: "dt.fmt",     type: tFn(tAny, tFn(tStr, tStr)), description: "Format datetime with format string" },
  { name: "dt.add",     type: tFn(tAny, tFn(tInt, tAny)), description: "Add duration (ms) to datetime" },
  { name: "dt.sub",     type: tFn(tAny, tFn(tAny, tInt)), description: "Difference between datetimes (ms)" },
  { name: "dt.tz",      type: tFn(tAny, tFn(tStr, tAny)), description: "Convert timezone" },
  { name: "dt.iso",     type: tFn(tAny, tStr), description: "Format as ISO 8601" },
  { name: "dt.ms",      type: tFn(tInt, tInt), description: "Milliseconds to duration" },
  { name: "dt.sec",     type: tFn(tInt, tInt), description: "Seconds to duration (ms)" },
  { name: "dt.min",     type: tFn(tInt, tInt), description: "Minutes to duration (ms)" },
  { name: "dt.hr",      type: tFn(tInt, tInt), description: "Hours to duration (ms)" },
  { name: "dt.day",     type: tFn(tInt, tInt), description: "Days to duration (ms)" },
];
