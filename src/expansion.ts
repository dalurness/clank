// Clank expansion table — bidirectional terse ↔ verbose identifier mapping
// Source of truth for clank pretty / clank terse transformations

export type Direction = "pretty" | "terse";

// All qualified expansions: terse → verbose
const QUALIFIED_EXPANSIONS: [string, string][] = [
  // Module names (§3.1)
  // Only modules that actually change are listed
  // std.json, std.http, std.cli, std.csv, std.log, std.test, std.math keep their names

  // String functions (§3.2)
  ["str.len", "string.length"],
  ["str.get", "string.get"],
  ["str.slc", "string.slice"],
  ["str.has", "string.contains"],
  ["str.idx", "string.index-of"],
  ["str.ridx", "string.last-index-of"],
  ["str.pfx", "string.starts-with"],
  ["str.sfx", "string.ends-with"],
  ["str.up", "string.uppercase"],
  ["str.lo", "string.lowercase"],
  ["str.rep", "string.replace"],
  ["str.rep1", "string.replace-first"],
  ["str.pad", "string.pad-right"],
  ["str.lpad", "string.pad-left"],
  ["str.rev", "string.reverse"],
  ["str.enc", "string.encode"],
  ["str.dec", "string.decode"],
  ["str.cat", "string.concatenate"],
  ["str.fmt", "string.format"],
  ["str.lines", "string.lines"],
  ["str.words", "string.words"],
  ["str.chars", "string.chars"],
  ["str.int", "string.parse-int"],
  ["str.rat", "string.parse-rat"],
  ["str.show", "string.show"],

  // JSON functions (§3.3)
  ["json.enc", "json.encode"],
  ["json.dec", "json.decode"],
  ["json.get", "json.get"],
  ["json.idx", "json.index"],
  ["json.path", "json.path"],
  ["json.set", "json.set"],
  ["json.del", "json.delete"],
  ["json.keys", "json.keys"],
  ["json.vals", "json.values"],
  ["json.typ", "json.type-of"],
  ["json.int", "json.as-int"],
  ["json.str", "json.as-string"],
  ["json.bool", "json.as-bool"],
  ["json.arr", "json.as-array"],
  ["json.merge", "json.merge"],

  // Filesystem functions (§3.4)
  ["fs.open", "filesystem.open"],
  ["fs.close", "filesystem.close"],
  ["fs.read", "filesystem.read"],
  ["fs.readb", "filesystem.read-bytes"],
  ["fs.write", "filesystem.write"],
  ["fs.writeb", "filesystem.write-bytes"],
  ["fs.append", "filesystem.append"],
  ["fs.lines", "filesystem.lines"],
  ["fs.exists", "filesystem.exists"],
  ["fs.rm", "filesystem.remove"],
  ["fs.mv", "filesystem.move"],
  ["fs.cp", "filesystem.copy"],
  ["fs.mkdir", "filesystem.make-directory"],
  ["fs.ls", "filesystem.list"],
  ["fs.stat", "filesystem.stat"],
  ["fs.tmp", "filesystem.temp"],
  ["fs.cwd", "filesystem.current-directory"],
  ["fs.abs", "filesystem.absolute"],
  ["fs.with", "filesystem.with"],

  // Collection functions — Lists (§3.5)
  ["col.rev", "collection.reverse"],
  ["col.sort", "collection.sort"],
  ["col.sortby", "collection.sort-by"],
  ["col.uniq", "collection.unique"],
  ["col.zip", "collection.zip"],
  ["col.unzip", "collection.unzip"],
  ["col.flat", "collection.flatten"],
  ["col.flatmap", "collection.flat-map"],
  ["col.take", "collection.take"],
  ["col.drop", "collection.drop"],
  ["col.nth", "collection.nth"],
  ["col.find", "collection.find"],
  ["col.any", "collection.any"],
  ["col.all", "collection.all"],
  ["col.count", "collection.count"],
  ["col.enum", "collection.enumerate"],
  ["col.chunk", "collection.chunk"],
  ["col.win", "collection.window"],
  ["col.intersperse", "collection.intersperse"],
  ["col.range", "collection.range"],
  ["col.rep", "collection.repeat"],
  ["col.sum", "collection.sum"],
  ["col.prod", "collection.product"],
  ["col.min", "collection.minimum"],
  ["col.max", "collection.maximum"],
  ["col.group", "collection.group-by"],
  ["col.scan", "collection.scan"],

  // Collection functions — Maps (§3.5)
  ["map.new", "map.new"],
  ["map.of", "map.of"],
  ["map.get", "map.get"],
  ["map.set", "map.set"],
  ["map.del", "map.delete"],
  ["map.has", "map.contains"],
  ["map.keys", "map.keys"],
  ["map.vals", "map.values"],
  ["map.pairs", "map.pairs"],
  ["map.len", "map.length"],
  ["map.merge", "map.merge"],
  ["map.mapv", "map.map-values"],
  ["map.filterv", "map.filter-values"],

  // Collection functions — Sets (§3.5)
  ["set.new", "set.new"],
  ["set.of", "set.of"],
  ["set.has", "set.contains"],
  ["set.add", "set.add"],
  ["set.rm", "set.remove"],
  ["set.union", "set.union"],
  ["set.inter", "set.intersection"],
  ["set.diff", "set.difference"],
  ["set.len", "set.length"],
  ["set.list", "set.to-list"],

  // HTTP functions (§3.6)
  ["http.get", "http.get"],
  ["http.post", "http.post"],
  ["http.put", "http.put"],
  ["http.del", "http.delete"],
  ["http.patch", "http.patch"],
  ["http.req", "http.request"],
  ["http.hdr", "http.header"],
  ["http.json", "http.json"],
  ["http.ok?", "http.ok?"],

  // Error functions (§3.7)
  ["err.new", "error.new"],
  ["err.ctx", "error.context"],
  ["err.wrap", "error.wrap"],

  // Process functions (§3.8)
  ["proc.run", "process.run"],
  ["proc.sh", "process.shell"],
  ["proc.ok", "process.ok"],
  ["proc.pipe", "process.pipe"],
  ["proc.bg", "process.background"],
  ["proc.wait", "process.wait"],
  ["proc.kill", "process.kill"],
  ["proc.exit", "process.exit"],
  ["proc.pid", "process.pid"],

  // Environment functions (§3.9)
  ["env.get", "environment.get"],
  ["env.get!", "environment.get!"],
  ["env.set", "environment.set"],
  ["env.rm", "environment.remove"],
  ["env.all", "environment.all"],
  ["env.has", "environment.has"],

  // Server functions (§3.10)
  ["srv.new", "server.new"],
  ["srv.get", "server.get"],
  ["srv.post", "server.post"],
  ["srv.put", "server.put"],
  ["srv.del", "server.delete"],
  ["srv.start", "server.start"],
  ["srv.stop", "server.stop"],
  ["srv.res", "server.response"],
  ["srv.json", "server.json"],
  ["srv.hdr", "server.header"],
  ["srv.mw", "server.middleware"],

  // DateTime functions (§3.11)
  ["dt.now", "datetime.now"],
  ["dt.unix", "datetime.unix"],
  ["dt.from", "datetime.from-unix"],
  ["dt.to", "datetime.to-unix"],
  ["dt.parse", "datetime.parse"],
  ["dt.fmt", "datetime.format"],
  ["dt.add", "datetime.add"],
  ["dt.sub", "datetime.subtract"],
  ["dt.tz", "datetime.timezone"],
  ["dt.iso", "datetime.iso"],
  ["dt.ms", "datetime.milliseconds"],
  ["dt.sec", "datetime.seconds"],
  ["dt.min", "datetime.minutes"],
  ["dt.hr", "datetime.hours"],
  ["dt.day", "datetime.days"],

  // CSV functions (§3.12)
  ["csv.dec", "csv.decode"],
  ["csv.enc", "csv.encode"],
  ["csv.decf", "csv.decode-file"],
  ["csv.encf", "csv.encode-file"],
  ["csv.hdr", "csv.header"],
  ["csv.rows", "csv.rows"],
  ["csv.maps", "csv.as-maps"],
  ["csv.opts", "csv.with-options"],

  // Regex functions (§3.13)
  ["rx.new", "regex.new"],
  ["rx.match", "regex.match"],
  ["rx.all", "regex.all"],
  ["rx.test", "regex.test"],
  ["rx.rep", "regex.replace"],
  ["rx.rep1", "regex.replace-first"],
  ["rx.split", "regex.split"],
];

// Module path expansions for import statements: use std.X → use std.Y
const MODULE_PATH_EXPANSIONS: [string, string][] = [
  ["std.str", "std.string"],
  ["std.fs", "std.filesystem"],
  ["std.col", "std.collection"],
  ["std.err", "std.error"],
  ["std.proc", "std.process"],
  ["std.env", "std.environment"],
  ["std.srv", "std.server"],
  ["std.dt", "std.datetime"],
  ["std.rx", "std.regex"],
];

// Unqualified built-in expansions (§3.14)
const UNQUALIFIED_EXPANSIONS: [string, string][] = [
  ["len", "length"],
  ["cat", "concatenate"],
  ["cmp", "compare"],
  ["eq", "equal"],
  ["neq", "not-equal"],
  ["ok?", "is-ok"],
  ["some?", "is-some"],
  ["unwrap?", "unwrap-option"],
  ["map-opt", "map-option"],
  ["map-res", "map-result"],
];

// Build lookup maps
function buildMaps(entries: [string, string][]): { toVerbose: Map<string, string>; toTerse: Map<string, string> } {
  const toVerbose = new Map<string, string>();
  const toTerse = new Map<string, string>();
  for (const [terse, verbose] of entries) {
    if (terse !== verbose) {
      toVerbose.set(terse, verbose);
      toTerse.set(verbose, terse);
    }
  }
  return { toVerbose, toTerse };
}

const qualified = buildMaps(QUALIFIED_EXPANSIONS);
const modulePaths = buildMaps(MODULE_PATH_EXPANSIONS);
const unqualified = buildMaps(UNQUALIFIED_EXPANSIONS);

// Terse module prefixes that map to verbose prefixes (for bare import: use std.str)
// Extracted from module path expansions
const MODULE_PREFIX_TERSE_TO_VERBOSE = new Map<string, string>();
const MODULE_PREFIX_VERBOSE_TO_TERSE = new Map<string, string>();
for (const [terse, verbose] of MODULE_PATH_EXPANSIONS) {
  const tersePart = terse.split(".")[1];
  const verbosePart = verbose.split(".")[1];
  if (tersePart !== verbosePart) {
    MODULE_PREFIX_TERSE_TO_VERBOSE.set(tersePart, verbosePart);
    MODULE_PREFIX_VERBOSE_TO_TERSE.set(verbosePart, tersePart);
  }
}

// Also need bare module prefix mappings for qualified calls (str.X → string.X)
// These are derived from the qualified table
const BARE_PREFIX_TERSE = new Set<string>();
const BARE_PREFIX_VERBOSE = new Set<string>();
for (const [terse, verbose] of QUALIFIED_EXPANSIONS) {
  if (terse !== verbose) {
    BARE_PREFIX_TERSE.add(terse.split(".")[0]);
    BARE_PREFIX_VERBOSE.add(verbose.split(".")[0]);
  }
}

export const expansionTable = {
  qualified,
  modulePaths,
  unqualified,
  MODULE_PREFIX_TERSE_TO_VERBOSE,
  MODULE_PREFIX_VERBOSE_TO_TERSE,
  BARE_PREFIX_TERSE,
  BARE_PREFIX_VERBOSE,
};
