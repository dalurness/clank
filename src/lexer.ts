// Clank lexer — tokenizes source into structured tokens
// ASCII-only, # comments, no semicolons

import type { Loc } from "./ast.js";

// ── Token types ──

export type TokenTag =
  | "int" | "rat" | "str" | "bool"
  | "ident" | "keyword"
  | "op" | "delim"
  | "eof";

export type Token = { tag: TokenTag; value: string; loc: Loc };

export type LexError = {
  code: string;
  message: string;
  location: Loc;
  context: string;
};

// ── Keywords ──

const KEYWORDS = new Set([
  "let", "in", "for", "fn", "if", "then", "else", "match", "do", "type", "effect",
  "affine", "handle", "resume", "perform", "mod", "use", "pub", "clone", "true", "false",
  "interface", "impl", "Self", "deriving", "where", "opaque", "return", "test", "alias",
]);

// ── Multi-char operators (longest-match order) ──

const MULTI_OPS = [
  "==", "!=", "<=", ">=", "&&", "||", "++", "|>", "=>", "<-", "->",
];

const SINGLE_OPS = new Set([
  "+", "-", "*", "/", "%", "<", ">", "!", "=", "\\",
]);

const DELIMITERS = new Set([
  "{", "}", "(", ")", "[", "]", ",", ":", ".", "|", "&",
]);

// ── Helpers ──

function isAlpha(ch: string): boolean {
  return (ch >= "a" && ch <= "z") || (ch >= "A" && ch <= "Z");
}

function isDigit(ch: string): boolean {
  return ch >= "0" && ch <= "9";
}

function isIdentChar(ch: string): boolean {
  return isAlpha(ch) || isDigit(ch) || ch === "_" || ch === "-";
}

function isWhitespace(ch: string): boolean {
  return ch === " " || ch === "\t" || ch === "\n" || ch === "\r";
}

// ── Lexer ──

export function lex(source: string): Token[] | LexError {
  const tokens: Token[] = [];
  let pos = 0;
  let line = 1;
  let col = 1;

  function loc(): Loc {
    return { line, col };
  }

  function endLoc(): { end_line: number; end_col: number } {
    return { end_line: line, end_col: col };
  }

  function peek(): string {
    return pos < source.length ? source[pos] : "";
  }

  function advance(): string {
    const ch = source[pos++];
    if (ch === "\n") { line++; col = 1; } else { col++; }
    return ch;
  }

  function error(msg: string, l?: Loc): LexError {
    const at = l ?? loc();
    return { code: "E001", message: msg, location: at, context: source.slice(Math.max(0, pos - 20), pos + 20) };
  }

  while (pos < source.length) {
    const ch = peek();

    // Whitespace
    if (isWhitespace(ch)) { advance(); continue; }

    // Comment
    if (ch === "#") {
      while (pos < source.length && peek() !== "\n") advance();
      continue;
    }

    const start = loc();

    // String literal
    if (ch === '"') {
      advance(); // opening quote
      let value = "";
      while (pos < source.length && peek() !== '"') {
        if (peek() === "\\") {
          advance();
          const esc = advance();
          if (esc === "n") value += "\n";
          else if (esc === "t") value += "\t";
          else if (esc === "\\") value += "\\";
          else if (esc === '"') value += '"';
          else return error(`invalid escape \\${esc}`, start);
        } else {
          value += advance();
        }
      }
      if (pos >= source.length) return error("unterminated string", start);
      advance(); // closing quote
      tokens.push({ tag: "str", value, loc: { ...start, ...endLoc() } });
      continue;
    }

    // Number literal (int or rat)
    if (isDigit(ch)) {
      let num = "";
      while (pos < source.length && isDigit(peek())) num += advance();
      if (peek() === "." && pos + 1 < source.length && isDigit(source[pos + 1])) {
        num += advance(); // the dot
        while (pos < source.length && isDigit(peek())) num += advance();
        tokens.push({ tag: "rat", value: num, loc: { ...start, ...endLoc() } });
      } else {
        tokens.push({ tag: "int", value: num, loc: { ...start, ...endLoc() } });
      }
      continue;
    }

    // Identifier or keyword
    if (isAlpha(ch) || ch === "_") {
      let word = "";
      while (pos < source.length && isIdentChar(peek())) word += advance();
      const end = endLoc();
      if (word === "true" || word === "false") {
        tokens.push({ tag: "bool", value: word, loc: { ...start, ...end } });
      } else if (KEYWORDS.has(word)) {
        tokens.push({ tag: "keyword", value: word, loc: { ...start, ...end } });
      } else {
        tokens.push({ tag: "ident", value: word, loc: { ...start, ...end } });
      }
      continue;
    }

    // Multi-char operators (try longest match first)
    if (pos + 1 < source.length) {
      const two = source.slice(pos, pos + 2);
      if (MULTI_OPS.includes(two)) {
        advance(); advance();
        tokens.push({ tag: "op", value: two, loc: { ...start, ...endLoc() } });
        continue;
      }
    }

    // Range operators: ..= (3-char) and .. (2-char)
    if (ch === "." && pos + 1 < source.length && source[pos + 1] === ".") {
      if (pos + 2 < source.length && source[pos + 2] === "=") {
        advance(); advance(); advance();
        tokens.push({ tag: "op", value: "..=", loc: { ...start, ...endLoc() } });
      } else {
        advance(); advance();
        tokens.push({ tag: "op", value: "..", loc: { ...start, ...endLoc() } });
      }
      continue;
    }

    // Single-char operators
    if (SINGLE_OPS.has(ch)) {
      advance();
      tokens.push({ tag: "op", value: ch, loc: { ...start, ...endLoc() } });
      continue;
    }

    // Delimiters
    if (DELIMITERS.has(ch)) {
      advance();
      tokens.push({ tag: "delim", value: ch, loc: { ...start, ...endLoc() } });
      continue;
    }

    // Unknown character
    return error(`unexpected character '${ch}'`, start);
  }

  tokens.push({ tag: "eof", value: "", loc: loc() });
  return tokens;
}
