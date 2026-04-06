// Package pretty implements bidirectional terse ↔ verbose identifier mapping
// for Clank source files.
package pretty

// Direction indicates which way to transform identifiers.
type Direction int

const (
	Pretty Direction = iota // terse → verbose
	Terse                   // verbose → terse
)

func (d Direction) String() string {
	if d == Pretty {
		return "pretty"
	}
	return "terse"
}

// qualifiedExpansions maps terse → verbose for qualified identifiers.
var qualifiedExpansions = [][2]string{
	// String functions
	{"str.len", "string.length"},
	{"str.get", "string.get"},
	{"str.slc", "string.slice"},
	{"str.has", "string.contains"},
	{"str.idx", "string.index-of"},
	{"str.ridx", "string.last-index-of"},
	{"str.pfx", "string.starts-with"},
	{"str.sfx", "string.ends-with"},
	{"str.up", "string.uppercase"},
	{"str.lo", "string.lowercase"},
	{"str.rep", "string.replace"},
	{"str.rep1", "string.replace-first"},
	{"str.pad", "string.pad-right"},
	{"str.lpad", "string.pad-left"},
	{"str.rev", "string.reverse"},
	{"str.enc", "string.encode"},
	{"str.dec", "string.decode"},
	{"str.cat", "string.concatenate"},
	{"concat", "concatenate"},
	{"str.fmt", "string.format"},
	{"str.lines", "string.lines"},
	{"str.words", "string.words"},
	{"str.chars", "string.chars"},
	{"str.int", "string.parse-int"},
	{"str.rat", "string.parse-rat"},
	{"str.show", "string.show"},

	// JSON functions
	{"json.enc", "json.encode"},
	{"json.dec", "json.decode"},
	{"json.get", "json.get"},
	{"json.idx", "json.index"},
	{"json.path", "json.path"},
	{"json.set", "json.set"},
	{"json.del", "json.delete"},
	{"json.keys", "json.keys"},
	{"json.vals", "json.values"},
	{"json.typ", "json.type-of"},
	{"json.int", "json.as-int"},
	{"json.str", "json.as-string"},
	{"json.bool", "json.as-bool"},
	{"json.arr", "json.as-array"},
	{"json.merge", "json.merge"},

	// Filesystem functions
	{"fs.open", "filesystem.open"},
	{"fs.close", "filesystem.close"},
	{"fs.read", "filesystem.read"},
	{"fs.readb", "filesystem.read-bytes"},
	{"fs.write", "filesystem.write"},
	{"fs.writeb", "filesystem.write-bytes"},
	{"fs.append", "filesystem.append"},
	{"fs.lines", "filesystem.lines"},
	{"fs.exists", "filesystem.exists"},
	{"fs.rm", "filesystem.remove"},
	{"fs.mv", "filesystem.move"},
	{"fs.cp", "filesystem.copy"},
	{"fs.mkdir", "filesystem.make-directory"},
	{"fs.ls", "filesystem.list"},
	{"fs.stat", "filesystem.stat"},
	{"fs.tmp", "filesystem.temp"},
	{"fs.cwd", "filesystem.current-directory"},
	{"fs.abs", "filesystem.absolute"},
	{"fs.with", "filesystem.with"},

	// Collection functions — Lists
	{"col.rev", "collection.reverse"},
	{"col.sort", "collection.sort"},
	{"col.sortby", "collection.sort-by"},
	{"col.uniq", "collection.unique"},
	{"col.zip", "collection.zip"},
	{"col.unzip", "collection.unzip"},
	{"col.flat", "collection.flatten"},
	{"col.flatmap", "collection.flat-map"},
	{"col.take", "collection.take"},
	{"col.drop", "collection.drop"},
	{"col.nth", "collection.nth"},
	{"col.find", "collection.find"},
	{"col.any", "collection.any"},
	{"col.all", "collection.all"},
	{"col.count", "collection.count"},
	{"col.enum", "collection.enumerate"},
	{"col.chunk", "collection.chunk"},
	{"col.win", "collection.window"},
	{"col.intersperse", "collection.intersperse"},
	{"col.range", "collection.range"},
	{"col.rep", "collection.repeat"},
	{"col.sum", "collection.sum"},
	{"col.prod", "collection.product"},
	{"col.min", "collection.minimum"},
	{"col.max", "collection.maximum"},
	{"col.group", "collection.group-by"},
	{"col.scan", "collection.scan"},

	// Collection functions — Maps
	{"map.new", "map.new"},
	{"map.of", "map.of"},
	{"map.get", "map.get"},
	{"map.set", "map.set"},
	{"map.del", "map.delete"},
	{"map.has", "map.contains"},
	{"map.keys", "map.keys"},
	{"map.vals", "map.values"},
	{"map.pairs", "map.pairs"},
	{"map.len", "map.length"},
	{"map.merge", "map.merge"},
	{"map.mapv", "map.map-values"},
	{"map.filterv", "map.filter-values"},

	// Collection functions — Sets
	{"set.new", "set.new"},
	{"set.of", "set.of"},
	{"set.has", "set.contains"},
	{"set.add", "set.add"},
	{"set.rm", "set.remove"},
	{"set.union", "set.union"},
	{"set.inter", "set.intersection"},
	{"set.diff", "set.difference"},
	{"set.len", "set.length"},
	{"set.list", "set.to-list"},

	// HTTP functions
	{"http.get", "http.get"},
	{"http.post", "http.post"},
	{"http.put", "http.put"},
	{"http.del", "http.delete"},
	{"http.patch", "http.patch"},
	{"http.req", "http.request"},
	{"http.hdr", "http.header"},
	{"http.json", "http.json"},
	{"http.ok?", "http.ok?"},

	// Error functions
	{"err.new", "error.new"},
	{"err.ctx", "error.context"},
	{"err.wrap", "error.wrap"},

	// Process functions
	{"proc.run", "process.run"},
	{"proc.sh", "process.shell"},
	{"proc.ok", "process.ok"},
	{"proc.pipe", "process.pipe"},
	{"proc.bg", "process.background"},
	{"proc.wait", "process.wait"},
	{"proc.kill", "process.kill"},
	{"proc.exit", "process.exit"},
	{"proc.pid", "process.pid"},

	// Environment functions
	{"env.get", "environment.get"},
	{"env.get!", "environment.get!"},
	{"env.set", "environment.set"},
	{"env.rm", "environment.remove"},
	{"env.all", "environment.all"},
	{"env.has", "environment.has"},

	// Server functions
	{"srv.new", "server.new"},
	{"srv.get", "server.get"},
	{"srv.post", "server.post"},
	{"srv.put", "server.put"},
	{"srv.del", "server.delete"},
	{"srv.start", "server.start"},
	{"srv.stop", "server.stop"},
	{"srv.res", "server.response"},
	{"srv.json", "server.json"},
	{"srv.hdr", "server.header"},
	{"srv.mw", "server.middleware"},

	// DateTime functions
	{"dt.now", "datetime.now"},
	{"dt.unix", "datetime.unix"},
	{"dt.from", "datetime.from-unix"},
	{"dt.to", "datetime.to-unix"},
	{"dt.parse", "datetime.parse"},
	{"dt.fmt", "datetime.format"},
	{"dt.add", "datetime.add"},
	{"dt.sub", "datetime.subtract"},
	{"dt.tz", "datetime.timezone"},
	{"dt.iso", "datetime.iso"},
	{"dt.ms", "datetime.milliseconds"},
	{"dt.sec", "datetime.seconds"},
	{"dt.min", "datetime.minutes"},
	{"dt.hr", "datetime.hours"},
	{"dt.day", "datetime.days"},

	// CSV functions
	{"csv.dec", "csv.decode"},
	{"csv.enc", "csv.encode"},
	{"csv.decf", "csv.decode-file"},
	{"csv.encf", "csv.encode-file"},
	{"csv.hdr", "csv.header"},
	{"csv.rows", "csv.rows"},
	{"csv.maps", "csv.as-maps"},
	{"csv.opts", "csv.with-options"},

	// Regex functions
	{"rx.new", "regex.new"},
	{"rx.match", "regex.match"},
	{"rx.all", "regex.all"},
	{"rx.test", "regex.test"},
	{"rx.rep", "regex.replace"},
	{"rx.rep1", "regex.replace-first"},
	{"rx.split", "regex.split"},
}

var modulePathExpansions = [][2]string{
	{"std.str", "std.string"},
	{"std.fs", "std.filesystem"},
	{"std.col", "std.collection"},
	{"std.err", "std.error"},
	{"std.proc", "std.process"},
	{"std.env", "std.environment"},
	{"std.srv", "std.server"},
	{"std.dt", "std.datetime"},
	{"std.rx", "std.regex"},
}

var unqualifiedExpansions = [][2]string{
	{"len", "length"},
	{"cat", "concatenate"},
	{"cmp", "compare"},
	{"eq", "equal"},
	{"neq", "not-equal"},
	{"ok?", "is-ok"},
	{"some?", "is-some"},
	{"unwrap?", "unwrap-option"},
	{"map-opt", "map-option"},
	{"map-res", "map-result"},
}

type bimap struct {
	toVerbose map[string]string
	toTerse   map[string]string
}

type tables struct {
	qualified   bimap
	modulePaths bimap
	unqualified bimap
	// Bare module prefixes that have expansions (e.g., "str", "string")
	barePrefixTerse   map[string]bool
	barePrefixVerbose map[string]bool
}

var expansionTable = buildTables()

func buildBimap(entries [][2]string) bimap {
	m := bimap{
		toVerbose: make(map[string]string, len(entries)),
		toTerse:   make(map[string]string, len(entries)),
	}
	for _, e := range entries {
		terse, verbose := e[0], e[1]
		if terse != verbose {
			m.toVerbose[terse] = verbose
			m.toTerse[verbose] = terse
		}
	}
	return m
}

func buildTables() tables {
	t := tables{
		qualified:         buildBimap(qualifiedExpansions),
		modulePaths:       buildBimap(modulePathExpansions),
		unqualified:       buildBimap(unqualifiedExpansions),
		barePrefixTerse:   make(map[string]bool),
		barePrefixVerbose: make(map[string]bool),
	}
	for _, e := range qualifiedExpansions {
		terse, verbose := e[0], e[1]
		if terse != verbose {
			t.barePrefixTerse[splitDot(terse)] = true
			t.barePrefixVerbose[splitDot(verbose)] = true
		}
	}
	return t
}

// splitDot returns the part before the first dot.
func splitDot(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return s[:i]
		}
	}
	return s
}

// splitDotAfter returns the part after the first dot.
func splitDotAfter(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return s[i+1:]
		}
	}
	return ""
}
