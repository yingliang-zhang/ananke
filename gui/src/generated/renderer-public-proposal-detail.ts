// @ts-nocheck
// To parse this data:
//
//   import { Convert, ProposalDetail } from "./file";
//
//   const proposalDetail = Convert.toProposalDetail(json);
//
// These functions will throw an error if the JSON doesn't
// match the expected interface, even if the JSON is valid.

/**
 * Public result of the Tauri get_proposal command for the current revision and its paired
 * records.
 */
export interface ProposalDetail {
    approval:  Approval;
    lifecycle: RevisionLifecycle;
    proposal:  ProposalDetailProposal;
    revision:  Revision;
}

export interface Approval {
    approval_id:              string;
    created_at:               string;
    created_by:               CreatedBy;
    decided_at:               null | string;
    decided_by:               null | string;
    decision_idempotency_key: null | string;
    proposal_id:              string;
    reason:                   null | string;
    revision:                 number;
    revision_hash:            string;
    state:                    ApprovalState;
}

export type CreatedBy = "local_gui_operator";

export type ApprovalState = "pending" | "approved" | "rejected" | "superseded" | "withdrawn";

export interface RevisionLifecycle {
    approval_id:   string;
    created_at:    string;
    proposal_id:   string;
    revision:      number;
    revision_hash: string;
    state:         ApprovalState;
    updated_at:    string;
    version:       number;
}

export interface ProposalDetailProposal {
    created_at:            string;
    created_by:            CreatedBy;
    current_revision:      number;
    current_revision_hash: string;
    project_id:            string;
    proposal_id:           string;
    state:                 ProposalState;
    workstream_id:         string;
}

export type ProposalState = "open" | "approved" | "withdrawn";

export interface Revision {
    acceptance_criteria:  [string, ...string[]];
    created_at:           string;
    created_by:           CreatedBy;
    idempotency_key:      string;
    parent_revision:      number | null;
    parent_revision_hash: null | string;
    policy:               ProposalPolicy;
    proposal_id:          string;
    revision:             number;
    schema_version:       SchemaVersion;
    task:                 ProposalTask;
}

export interface ProposalPolicy {
    adapter:    ProposalAdapterPolicy;
    authority:  Authority;
    budget:     ProposalBudgetPolicy;
    model_role: ModelRole;
}

export interface ProposalAdapterPolicy {
    access: Access;
    kind:   Kind;
    status: Status;
}

export type Access = "read_only";

export type Kind = "omp_audit";

export type Status = "future";

export type Authority = "deterministic";

export interface ProposalBudgetPolicy {
    dimensions: [string, string, ...string[]];
    status:     Status;
}

export type ModelRole = "advisory_only";

export type SchemaVersion = "ananke.proposal-revision.v1";

export interface ProposalTask {
    instructions: string;
    title:        string;
}

// Converts JSON strings to/from your types
// and asserts the results of JSON.parse at runtime
export class Convert {
    public static toProposalDetail(json: string): ProposalDetail {
        return cast(JSON.parse(json), r("ProposalDetail"));
    }

    public static proposalDetailToJson(value: ProposalDetail): string {
        return JSON.stringify(uncast(value, r("ProposalDetail")), null, 2);
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
    "ProposalDetail": o([
        { json: "approval", js: "approval", typ: r("Approval") },
        { json: "lifecycle", js: "lifecycle", typ: r("RevisionLifecycle") },
        { json: "proposal", js: "proposal", typ: r("ProposalDetailProposal") },
        { json: "revision", js: "revision", typ: r("Revision") },
    ], false),
    "Approval": o([
        { json: "approval_id", js: "approval_id", typ: "" },
        { json: "created_at", js: "created_at", typ: "" },
        { json: "created_by", js: "created_by", typ: r("CreatedBy") },
        { json: "decided_at", js: "decided_at", typ: u(null, "") },
        { json: "decided_by", js: "decided_by", typ: u(null, "") },
        { json: "decision_idempotency_key", js: "decision_idempotency_key", typ: u(null, "") },
        { json: "proposal_id", js: "proposal_id", typ: "" },
        { json: "reason", js: "reason", typ: u(null, "") },
        { json: "revision", js: "revision", typ: 0 },
        { json: "revision_hash", js: "revision_hash", typ: "" },
        { json: "state", js: "state", typ: r("ApprovalState") },
    ], false),
    "RevisionLifecycle": o([
        { json: "approval_id", js: "approval_id", typ: "" },
        { json: "created_at", js: "created_at", typ: "" },
        { json: "proposal_id", js: "proposal_id", typ: "" },
        { json: "revision", js: "revision", typ: 0 },
        { json: "revision_hash", js: "revision_hash", typ: "" },
        { json: "state", js: "state", typ: r("ApprovalState") },
        { json: "updated_at", js: "updated_at", typ: "" },
        { json: "version", js: "version", typ: 0 },
    ], false),
    "ProposalDetailProposal": o([
        { json: "created_at", js: "created_at", typ: "" },
        { json: "created_by", js: "created_by", typ: r("CreatedBy") },
        { json: "current_revision", js: "current_revision", typ: 0 },
        { json: "current_revision_hash", js: "current_revision_hash", typ: "" },
        { json: "project_id", js: "project_id", typ: "" },
        { json: "proposal_id", js: "proposal_id", typ: "" },
        { json: "state", js: "state", typ: r("ProposalState") },
        { json: "workstream_id", js: "workstream_id", typ: "" },
    ], false),
    "Revision": o([
        { json: "acceptance_criteria", js: "acceptance_criteria", typ: a("") },
        { json: "created_at", js: "created_at", typ: "" },
        { json: "created_by", js: "created_by", typ: r("CreatedBy") },
        { json: "idempotency_key", js: "idempotency_key", typ: "" },
        { json: "parent_revision", js: "parent_revision", typ: u(0, null) },
        { json: "parent_revision_hash", js: "parent_revision_hash", typ: u(null, "") },
        { json: "policy", js: "policy", typ: r("ProposalPolicy") },
        { json: "proposal_id", js: "proposal_id", typ: "" },
        { json: "revision", js: "revision", typ: 0 },
        { json: "schema_version", js: "schema_version", typ: r("SchemaVersion") },
        { json: "task", js: "task", typ: r("ProposalTask") },
    ], false),
    "ProposalPolicy": o([
        { json: "adapter", js: "adapter", typ: r("ProposalAdapterPolicy") },
        { json: "authority", js: "authority", typ: r("Authority") },
        { json: "budget", js: "budget", typ: r("ProposalBudgetPolicy") },
        { json: "model_role", js: "model_role", typ: r("ModelRole") },
    ], false),
    "ProposalAdapterPolicy": o([
        { json: "access", js: "access", typ: r("Access") },
        { json: "kind", js: "kind", typ: r("Kind") },
        { json: "status", js: "status", typ: r("Status") },
    ], false),
    "ProposalBudgetPolicy": o([
        { json: "dimensions", js: "dimensions", typ: a("") },
        { json: "status", js: "status", typ: r("Status") },
    ], false),
    "ProposalTask": o([
        { json: "instructions", js: "instructions", typ: "" },
        { json: "title", js: "title", typ: "" },
    ], false),
    "CreatedBy": [
        "local_gui_operator",
    ],
    "ApprovalState": [
        "approved",
        "pending",
        "rejected",
        "superseded",
        "withdrawn",
    ],
    "ProposalState": [
        "approved",
        "open",
        "withdrawn",
    ],
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
    "SchemaVersion": [
        "ananke.proposal-revision.v1",
    ],
};
