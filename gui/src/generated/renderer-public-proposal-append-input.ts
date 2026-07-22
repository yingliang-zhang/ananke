// @ts-nocheck
// To parse this data:
//
//   import { Convert, AppendProposalRevisionInput } from "./file";
//
//   const appendProposalRevisionInput = Convert.toAppendProposalRevisionInput(json);
//
// These functions will throw an error if the JSON doesn't
// match the expected interface, even if the JSON is valid.

/**
 * Public arguments of the Tauri append_proposal_revision command.
 */
export interface AppendProposalRevisionInput {
    expected_current_revision:      number;
    expected_current_revision_hash: string;
    idempotency_key:                string;
    proposal_id:                    string;
    revision_input:                 AppendProposalRevisionInputBody;
}

export interface AppendProposalRevisionInputBody {
    acceptance_criteria: [string, ...string[]];
    policy:              AppendProposalPolicy;
    task:                AppendProposalTask;
}

export interface AppendProposalPolicy {
    adapter:    AppendProposalAdapterPolicy;
    authority:  Authority;
    budget:     AppendProposalBudgetPolicy;
    model_role: ModelRole;
}

export interface AppendProposalAdapterPolicy {
    access: Access;
    kind:   Kind;
    status: Status;
}

export type Access = "read_only";

export type Kind = "omp_audit";

export type Status = "future";

export type Authority = "deterministic";

export interface AppendProposalBudgetPolicy {
    dimensions: [string, string, ...string[]];
    status:     Status;
}

export type ModelRole = "advisory_only";

export interface AppendProposalTask {
    instructions: string;
    title:        string;
}

// Converts JSON strings to/from your types
// and asserts the results of JSON.parse at runtime
export class Convert {
    public static toAppendProposalRevisionInput(json: string): AppendProposalRevisionInput {
        return cast(JSON.parse(json), r("AppendProposalRevisionInput"));
    }

    public static appendProposalRevisionInputToJson(value: AppendProposalRevisionInput): string {
        return JSON.stringify(uncast(value, r("AppendProposalRevisionInput")), null, 2);
    }
}

function invalidValue(typ: any, val: any, key: any, parent: any = ''): never {
    const prettyTyp = prettyTypeName(typ);
    const parentText = parent ? ` on ${parent}` : '';
    const keyText = key ? ` for key "${key}"` : '';
    throw Error(`Invalid value${keyText}${parentText}. Expected ${prettyTyp} but got ${JSON.stringify(val)}`);
}

function prettyTypeName(typ: any): string {
    if (Array.isArray(typ)) {
        if (typ.length === 2 && typ[0] === undefined) {
            return `an optional ${prettyTypeName(typ[1])}`;
        } else {
            return `one of [${typ.map(a => { return prettyTypeName(a); }).join(", ")}]`;
        }
    } else if (typeof typ === "object" && typ.literal !== undefined) {
        return typ.literal;
    } else {
        return typeof typ;
    }
}

function jsonToJSProps(typ: any): any {
    if (typ.jsonToJS === undefined) {
        const map: any = {};
        typ.props.forEach((p: any) => map[p.json] = { key: p.js, typ: p.typ });
        typ.jsonToJS = map;
    }
    return typ.jsonToJS;
}

function jsToJSONProps(typ: any): any {
    if (typ.jsToJSON === undefined) {
        const map: any = {};
        typ.props.forEach((p: any) => map[p.js] = { key: p.json, typ: p.typ });
        typ.jsToJSON = map;
    }
    return typ.jsToJSON;
}

function transform(val: any, typ: any, getProps: any, key: any = '', parent: any = ''): any {
    function transformPrimitive(typ: string, val: any): any {
        if (typeof typ === typeof val) return val;
        return invalidValue(typ, val, key, parent);
    }

    function transformUnion(typs: any[], val: any): any {
        // val must validate against one typ in typs
        const l = typs.length;
        for (let i = 0; i < l; i++) {
            const typ = typs[i];
            try {
                return transform(val, typ, getProps);
            } catch (_) {}
        }
        return invalidValue(typs, val, key, parent);
    }

    function transformEnum(cases: string[], val: any): any {
        if (cases.indexOf(val) !== -1) return val;
        return invalidValue(cases.map(a => { return l(a); }), val, key, parent);
    }

    function transformArray(typ: any, val: any): any {
        // val must be an array with no invalid elements
        if (!Array.isArray(val)) return invalidValue(l("array"), val, key, parent);
        return val.map(el => transform(el, typ, getProps));
    }

    function transformDate(val: any): any {
        if (val === null) {
            return null;
        }
        const d = new Date(val);
        if (isNaN(d.valueOf())) {
            return invalidValue(l("Date"), val, key, parent);
        }
        return d;
    }

    function transformObject(props: { [k: string]: any }, additional: any, val: any): any {
        if (val === null || typeof val !== "object" || Array.isArray(val)) {
            return invalidValue(l(ref || "object"), val, key, parent);
        }
        const result: any = {};
        Object.getOwnPropertyNames(props).forEach(key => {
            const prop = props[key];
            const v = Object.prototype.hasOwnProperty.call(val, key) ? val[key] : undefined;
            result[prop.key] = transform(v, prop.typ, getProps, key, ref);
        });
        Object.getOwnPropertyNames(val).forEach(key => {
            if (!Object.prototype.hasOwnProperty.call(props, key)) {
                result[key] = transform(val[key], additional, getProps, key, ref);
            }
        });
        return result;
    }

    if (typ === "any") return val;
    if (typ === null) {
        if (val === null) return val;
        return invalidValue(typ, val, key, parent);
    }
    if (typ === false) return invalidValue(typ, val, key, parent);
    let ref: any = undefined;
    while (typeof typ === "object" && typ.ref !== undefined) {
        ref = typ.ref;
        typ = typeMap[typ.ref];
    }
    if (Array.isArray(typ)) return transformEnum(typ, val);
    if (typeof typ === "object") {
        return typ.hasOwnProperty("unionMembers") ? transformUnion(typ.unionMembers, val)
            : typ.hasOwnProperty("arrayItems")    ? transformArray(typ.arrayItems, val)
            : typ.hasOwnProperty("props")         ? transformObject(getProps(typ), typ.additional, val)
            : invalidValue(typ, val, key, parent);
    }
    // Numbers can be parsed by Date but shouldn't be.
    if (typ === Date && typeof val !== "number") return transformDate(val);
    return transformPrimitive(typ, val);
}

function cast<T>(val: any, typ: any): T {
    return transform(val, typ, jsonToJSProps);
}

function uncast<T>(val: T, typ: any): any {
    return transform(val, typ, jsToJSONProps);
}

function l(typ: any) {
    return { literal: typ };
}

function a(typ: any) {
    return { arrayItems: typ };
}

function u(...typs: any[]) {
    return { unionMembers: typs };
}

function o(props: any[], additional: any) {
    return { props, additional };
}

function m(additional: any) {
    return { props: [], additional };
}

function r(name: string) {
    return { ref: name };
}

const typeMap: any = {
    "AppendProposalRevisionInput": o([
        { json: "expected_current_revision", js: "expected_current_revision", typ: 0 },
        { json: "expected_current_revision_hash", js: "expected_current_revision_hash", typ: "" },
        { json: "idempotency_key", js: "idempotency_key", typ: "" },
        { json: "proposal_id", js: "proposal_id", typ: "" },
        { json: "revision_input", js: "revision_input", typ: r("AppendProposalRevisionInputBody") },
    ], false),
    "AppendProposalRevisionInputBody": o([
        { json: "acceptance_criteria", js: "acceptance_criteria", typ: a("") },
        { json: "policy", js: "policy", typ: r("AppendProposalPolicy") },
        { json: "task", js: "task", typ: r("AppendProposalTask") },
    ], false),
    "AppendProposalPolicy": o([
        { json: "adapter", js: "adapter", typ: r("AppendProposalAdapterPolicy") },
        { json: "authority", js: "authority", typ: r("Authority") },
        { json: "budget", js: "budget", typ: r("AppendProposalBudgetPolicy") },
        { json: "model_role", js: "model_role", typ: r("ModelRole") },
    ], false),
    "AppendProposalAdapterPolicy": o([
        { json: "access", js: "access", typ: r("Access") },
        { json: "kind", js: "kind", typ: r("Kind") },
        { json: "status", js: "status", typ: r("Status") },
    ], false),
    "AppendProposalBudgetPolicy": o([
        { json: "dimensions", js: "dimensions", typ: a("") },
        { json: "status", js: "status", typ: r("Status") },
    ], false),
    "AppendProposalTask": o([
        { json: "instructions", js: "instructions", typ: "" },
        { json: "title", js: "title", typ: "" },
    ], false),
    "Access": [
        "read_only",
    ],
    "Kind": [
        "omp_audit",
    ],
    "Status": [
        "future",
    ],
    "Authority": [
        "deterministic",
    ],
    "ModelRole": [
        "advisory_only",
    ],
};
