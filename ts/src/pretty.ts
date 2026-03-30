// Clank pretty-print / terse transformation
// Bidirectional lexical substitution: terse ↔ verbose identifiers

import { expansionTable, type Direction } from "./expansion.js";

export type TransformResult = {
  source: string;
  transformations: number;
  direction: Direction;
};

// Identifier character: alphanumeric, underscore, hyphen
function isIdentStart(ch: string): boolean {
  return (ch >= "a" && ch <= "z") || (ch >= "A" && ch <= "Z") || ch === "_";
}

function isIdentChar(ch: string): boolean {
  return isIdentStart(ch) || (ch >= "0" && ch <= "9") || ch === "-";
}

// Scan an identifier starting at pos, return the identifier string
function scanIdent(source: string, pos: number): string {
  let end = pos;
  while (end < source.length && isIdentChar(source[end])) end++;
  // Trailing ? or ! are part of ident (e.g., ok?, get!, some?)
  if (end < source.length && (source[end] === "?" || source[end] === "!")) end++;
  return source.slice(pos, end);
}

export function transform(source: string, direction: Direction): TransformResult {
  const { qualified, modulePaths, unqualified } = expansionTable;
  const qualifiedMap = direction === "pretty" ? qualified.toVerbose : qualified.toTerse;
  const moduleMap = direction === "pretty" ? modulePaths.toVerbose : modulePaths.toTerse;
  const unqualMap = direction === "pretty" ? unqualified.toVerbose : unqualified.toTerse;

  let result = "";
  let pos = 0;
  let transformations = 0;
  // Track whether we're in an import context (after 'use')
  let inUseStatement = false;
  // Track whether we just saw 'use' keyword + module path and are now in the import list
  let inImportList = false;

  while (pos < source.length) {
    const ch = source[pos];

    // Skip string literals
    if (ch === '"') {
      result += ch;
      pos++;
      while (pos < source.length && source[pos] !== '"') {
        if (source[pos] === "\\") {
          result += source[pos];
          pos++;
          if (pos < source.length) {
            result += source[pos];
            pos++;
          }
        } else {
          result += source[pos];
          pos++;
        }
      }
      if (pos < source.length) {
        result += source[pos]; // closing quote
        pos++;
      }
      continue;
    }

    // Skip comments
    if (ch === "#") {
      while (pos < source.length && source[pos] !== "\n") {
        result += source[pos];
        pos++;
      }
      continue;
    }

    // Newline resets use-statement context
    if (ch === "\n") {
      result += ch;
      pos++;
      inUseStatement = false;
      inImportList = false;
      continue;
    }

    // Track import list parentheses
    if (inUseStatement && ch === "(") {
      inImportList = true;
      result += ch;
      pos++;
      continue;
    }
    if (inImportList && ch === ")") {
      inImportList = false;
      inUseStatement = false;
      result += ch;
      pos++;
      continue;
    }

    // Identifier
    if (isIdentStart(ch)) {
      const ident = scanIdent(source, pos);

      // Check if 'use' keyword — start tracking import context
      if (ident === "use") {
        inUseStatement = true;
        inImportList = false;
        result += ident;
        pos += ident.length;
        continue;
      }

      // Look ahead for dot-qualified name: ident.ident
      let fullIdent = ident;
      let lookAhead = pos + ident.length;
      if (lookAhead < source.length && source[lookAhead] === ".") {
        const afterDot = lookAhead + 1;
        if (afterDot < source.length && isIdentStart(source[afterDot])) {
          const secondPart = scanIdent(source, afterDot);
          fullIdent = ident + "." + secondPart;
        }
      }

      // In import list, expand unqualified names that appear in qualified table
      // e.g., use std.str (slc) → use std.string (slice)
      if (inImportList) {
        const expanded = expandImportedName(ident, direction);
        if (expanded !== null && expanded !== ident) {
          result += expanded;
          transformations++;
          pos += ident.length;
        } else {
          result += ident;
          pos += ident.length;
        }
        continue;
      }

      // In use statement (module path): use std.str → use std.string
      if (inUseStatement && !inImportList) {
        // Try to expand as module path (std.X)
        if (fullIdent.length > ident.length) {
          const moduleExpanded = moduleMap.get(fullIdent);
          if (moduleExpanded !== undefined) {
            result += moduleExpanded;
            transformations++;
            pos += fullIdent.length;
            continue;
          }
        }
        // Pass through
        result += ident;
        pos += ident.length;
        continue;
      }

      // Try qualified expansion first (str.slc → string.slice)
      if (fullIdent.length > ident.length) {
        const expanded = qualifiedMap.get(fullIdent);
        if (expanded !== undefined) {
          result += expanded;
          transformations++;
          pos += fullIdent.length;
          continue;
        }
      }

      // Try unqualified expansion (len → length)
      const unqualExpanded = unqualMap.get(ident);
      if (unqualExpanded !== undefined) {
        result += unqualExpanded;
        transformations++;
        pos += ident.length;
        continue;
      }

      // No expansion — pass through
      result += ident;
      pos += ident.length;
      continue;
    }

    // Any other character — pass through
    result += ch;
    pos++;
  }

  return { source: result, transformations, direction };
}

// Expand an imported name based on which module it was imported from.
// We look up the name as if it were qualified with each terse/verbose module prefix
// and see if there's a match.
function expandImportedName(name: string, direction: Direction): string | null {
  const { qualified } = expansionTable;
  const qualifiedMap = direction === "pretty" ? qualified.toVerbose : qualified.toTerse;
  const unqualMap = direction === "pretty" ? expansionTable.unqualified.toVerbose : expansionTable.unqualified.toTerse;

  // The import list names are bare function names.
  // We need to check if "prefix.name" exists in the qualified table for any known prefix.
  const prefixes = direction === "pretty" ? expansionTable.BARE_PREFIX_TERSE : expansionTable.BARE_PREFIX_VERBOSE;
  for (const prefix of prefixes) {
    const qualName = prefix + "." + name;
    const expanded = qualifiedMap.get(qualName);
    if (expanded !== undefined) {
      // Return just the function part (after the dot)
      return expanded.split(".")[1];
    }
  }

  // Also try unqualified expansions (e.g., cat → concatenate in import lists)
  const unqualExpanded = unqualMap.get(name);
  if (unqualExpanded !== undefined) {
    return unqualExpanded;
  }

  return null;
}
