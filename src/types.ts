// Clank type representations
// Discriminated unions for the type system (used by type checker, not syntax)

// ── Primitive types ──

export type PrimitiveType = "int" | "rat" | "bool" | "str" | "unit";

// ── Type representations ──

export type Type =
  | { tag: "t-primitive"; name: PrimitiveType }
  | { tag: "t-fn"; param: Type; effects: Effect[]; result: Type }
  | { tag: "t-list"; element: Type }
  | { tag: "t-tuple"; elements: Type[] }
  | { tag: "t-record"; fields: { name: string; type: Type }[] }
  | { tag: "t-variant"; variants: VariantCase[] }
  | { tag: "t-var"; id: number }
  | { tag: "t-generic"; name: string; args: Type[] };

export type VariantCase = { name: string; fields: Type[] };

// ── Effect annotations ──

export type Effect =
  | { tag: "e-named"; name: string }
  | { tag: "e-var"; id: number };

// ── Type schemes (polymorphic types) ──

export type TypeScheme = {
  typeVars: number[];
  effectVars: number[];
  body: Type;
};

// ── Constructors ──

export const tPrimitive = (name: PrimitiveType): Type => ({ tag: "t-primitive", name });
export const tInt: Type = tPrimitive("int");
export const tRat: Type = tPrimitive("rat");
export const tBool: Type = tPrimitive("bool");
export const tStr: Type = tPrimitive("str");
export const tUnit: Type = tPrimitive("unit");

export const tFn = (param: Type, result: Type, effects: Effect[] = []): Type =>
  ({ tag: "t-fn", param, effects, result });

export const tList = (element: Type): Type => ({ tag: "t-list", element });
export const tTuple = (elements: Type[]): Type => ({ tag: "t-tuple", elements });

export const tRecord = (fields: { name: string; type: Type }[]): Type =>
  ({ tag: "t-record", fields });

export const tVariant = (variants: VariantCase[]): Type =>
  ({ tag: "t-variant", variants });

export const tVar = (id: number): Type => ({ tag: "t-var", id });

export const tGeneric = (name: string, args: Type[]): Type =>
  ({ tag: "t-generic", name, args });

export const eNamed = (name: string): Effect => ({ tag: "e-named", name });
export const eVar = (id: number): Effect => ({ tag: "e-var", id });
