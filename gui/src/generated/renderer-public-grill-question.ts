// @ts-nocheck
// To parse this data:
//
//   import { Convert, GrillQuestion } from "./file";
//
//   const grillQuestion = Convert.toGrillQuestion(json);
//
// These functions will throw an error if the JSON doesn't
// match the expected interface, even if the JSON is valid.

export interface GrillQuestion {
    blocking:          boolean;
    default:           Default;
    proposal_id:       string;
    question_id:       string;
    question_sequence: number;
    record_sequence:   number;
    remedial_step:     RemedialStep;
    revision:          number;
    revision_hash:     string;
    risk:              Risk;
    rule_class:        RuleClass;
    waivable:          boolean;
    written_at:        string;
    written_by:        WrittenBy;
}

export type Default = "needs_rewrite" | "deny";

export type RemedialStep = "declare_observable_outcome" | "declare_scope_compatibility" | "declare_acceptance_evidence" | "record_local_authorization" | "require_isolated_worktree" | "set_deadline_attempt_cap";

export type Risk = "medium" | "high" | "critical";

export type RuleClass = "observable_outcome" | "scope_compatibility" | "acceptance_evidence" | "destructive_external_authorization" | "adapter_worktree_isolation" | "autonomy_budget";

export type WrittenBy = "deterministic_grill";

// Converts JSON strings to/from your types
// and asserts the results of JSON.parse at runtime
export class Convert {
    public static toGrillQuestion(json: string): GrillQuestion {
        return validateP2c(cast(JSON.parse(json), r("GrillQuestion")), p2cSchema, "GrillQuestion");
    }

    public static grillQuestionToJson(value: GrillQuestion): string {
        return JSON.stringify(uncast(validateP2c(value, p2cSchema, "GrillQuestion"), r("GrillQuestion")), null, 2);
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
    "GrillQuestion": o([
        { json: "blocking", js: "blocking", typ: true },
        { json: "default", js: "default", typ: r("Default") },
        { json: "proposal_id", js: "proposal_id", typ: "" },
        { json: "question_id", js: "question_id", typ: "" },
        { json: "question_sequence", js: "question_sequence", typ: 0 },
        { json: "record_sequence", js: "record_sequence", typ: 0 },
        { json: "remedial_step", js: "remedial_step", typ: r("RemedialStep") },
        { json: "revision", js: "revision", typ: 0 },
        { json: "revision_hash", js: "revision_hash", typ: "" },
        { json: "risk", js: "risk", typ: r("Risk") },
        { json: "rule_class", js: "rule_class", typ: r("RuleClass") },
        { json: "waivable", js: "waivable", typ: true },
        { json: "written_at", js: "written_at", typ: "" },
        { json: "written_by", js: "written_by", typ: r("WrittenBy") },
    ], false),
    "Default": [
        "deny",
        "needs_rewrite",
    ],
    "RemedialStep": [
        "declare_acceptance_evidence",
        "declare_observable_outcome",
        "declare_scope_compatibility",
        "record_local_authorization",
        "require_isolated_worktree",
        "set_deadline_attempt_cap",
    ],
    "Risk": [
        "critical",
        "high",
        "medium",
    ],
    "RuleClass": [
        "acceptance_evidence",
        "adapter_worktree_isolation",
        "autonomy_budget",
        "destructive_external_authorization",
        "observable_outcome",
        "scope_compatibility",
    ],
    "WrittenBy": [
        "deterministic_grill",
    ],
};

const p2cSchema = {"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://ananke.local/contracts/renderer-public-grill-evaluation.schema.json#/properties/shown_questions/items","title":"GrillQuestion","type":"object","additionalProperties":false,"required":["proposal_id","revision","revision_hash","question_id","question_sequence","record_sequence","rule_class","risk","blocking","waivable","default","remedial_step","written_at","written_by"],"properties":{"proposal_id":{"type":"string","pattern":"^[a-z][a-z0-9_]{2,63}$"},"revision":{"type":"integer","minimum":1},"revision_hash":{"type":"string","pattern":"^sha256:[0-9a-f]{64}$"},"question_id":{"type":"string","pattern":"^grill_question_(observable_outcome|scope_compatibility|acceptance_evidence|destructive_external_authorization|adapter_worktree_isolation|autonomy_budget)$"},"question_sequence":{"type":"integer","minimum":1,"maximum":10},"record_sequence":{"type":"integer","minimum":1,"maximum":40},"rule_class":{"type":"string","enum":["observable_outcome","scope_compatibility","acceptance_evidence","destructive_external_authorization","adapter_worktree_isolation","autonomy_budget"]},"risk":{"type":"string","enum":["medium","high","critical"]},"blocking":{"const":true},"waivable":{"type":"boolean"},"default":{"type":"string","enum":["needs_rewrite","deny"]},"remedial_step":{"type":"string","enum":["declare_observable_outcome","declare_scope_compatibility","declare_acceptance_evidence","record_local_authorization","require_isolated_worktree","set_deadline_attempt_cap"]},"written_at":{"type":"string","x-ananke-utc-timestamp":true},"written_by":{"const":"deterministic_grill"}}};
const p2cRules = [
    { ruleClass: "observable_outcome", questionID: "grill_question_observable_outcome", priority: 10, defaultValue: "needs_rewrite", risk: "high", remedialStep: "declare_observable_outcome", waivable: false },
    { ruleClass: "scope_compatibility", questionID: "grill_question_scope_compatibility", priority: 20, defaultValue: "needs_rewrite", risk: "medium", remedialStep: "declare_scope_compatibility", waivable: true },
    { ruleClass: "acceptance_evidence", questionID: "grill_question_acceptance_evidence", priority: 30, defaultValue: "needs_rewrite", risk: "high", remedialStep: "declare_acceptance_evidence", waivable: false },
    { ruleClass: "destructive_external_authorization", questionID: "grill_question_destructive_external_authorization", priority: 40, defaultValue: "deny", risk: "critical", remedialStep: "record_local_authorization", waivable: false },
    { ruleClass: "adapter_worktree_isolation", questionID: "grill_question_adapter_worktree_isolation", priority: 50, defaultValue: "needs_rewrite", risk: "high", remedialStep: "require_isolated_worktree", waivable: false },
    { ruleClass: "autonomy_budget", questionID: "grill_question_autonomy_budget", priority: 60, defaultValue: "needs_rewrite", risk: "high", remedialStep: "set_deadline_attempt_cap", waivable: false },
];

function p2cFail(path: string, message: string): never {
    throw Error(path + " " + message);
}

function p2cTimestamp(value: string): boolean {
    const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/.exec(value);
    if (match === null) return false;
    const [year, month, day, hour, minute, second] = match.slice(1).map(Number);
    const days = month === 2 ? (year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28) : ([4, 6, 9, 11].includes(month) ? 30 : 31);
    return month >= 1 && month <= 12 && day >= 1 && day <= days && hour <= 23 && minute <= 59 && second <= 59;
}

function p2cType(value: unknown, type: string): boolean {
    if (type === "object") return value !== null && typeof value === "object" && !Array.isArray(value);
    if (type === "array") return Array.isArray(value);
    if (type === "integer") return Number.isInteger(value);
    if (type === "number") return typeof value === "number" && Number.isFinite(value);
    return typeof value === type;
}

function p2cValidateSchema(value: any, schema: any, path: string): void {
    if (Object.hasOwn(schema, "const") && !Object.is(value, schema.const)) p2cFail(path, "must equal its schema const");
    if (schema.enum !== undefined && !schema.enum.some((candidate: unknown) => Object.is(candidate, value))) p2cFail(path, "must equal a schema enum value");
    if (schema.type !== undefined && !(Array.isArray(schema.type) ? schema.type : [schema.type]).some((type: string) => p2cType(value, type))) p2cFail(path, "has the wrong schema type");
    if (typeof value === "string") {
        if (schema.pattern !== undefined && !(new RegExp(schema.pattern)).test(value)) p2cFail(path, "does not match its schema pattern");
        if (schema["x-ananke-utc-timestamp"] === true && !p2cTimestamp(value)) p2cFail(path, "must be a semantic UTC timestamp");
    }
    if (typeof value === "number") {
        if (schema.minimum !== undefined && value < schema.minimum) p2cFail(path, "is below its schema minimum");
        if (schema.maximum !== undefined && value > schema.maximum) p2cFail(path, "is above its schema maximum");
    }
    if (Array.isArray(value)) {
        if (schema.minItems !== undefined && value.length < schema.minItems) p2cFail(path, "has too few items");
        if (schema.maxItems !== undefined && value.length > schema.maxItems) p2cFail(path, "has too many items");
        if (schema.items !== undefined) value.forEach((entry, index) => p2cValidateSchema(entry, schema.items, path + "[" + index + "]"));
    }
    if (value !== null && typeof value === "object" && !Array.isArray(value) && schema.properties !== undefined) {
        const properties = schema.properties;
        for (const required of schema.required ?? []) if (!Object.hasOwn(value, required)) p2cFail(path, "is missing " + required);
        if (schema.additionalProperties === false) for (const key of Object.keys(value)) if (!Object.hasOwn(properties, key)) p2cFail(path + "." + key, "is an unknown field");
        for (const [key, property] of Object.entries(properties)) if (Object.hasOwn(value, key)) p2cValidateSchema(value[key], property, path + "." + key);
    }
}

function p2cRuleForQuestionID(questionID: string): any {
    return p2cRules.find((rule) => rule.questionID === questionID);
}

function p2cValidateQuestion(value: any, path: string): void {
    const rule = p2cRules.find((candidate) => candidate.ruleClass === value.rule_class);
    if (rule === undefined || value.question_id !== rule.questionID || value.blocking !== true || value.default !== rule.defaultValue || value.risk !== rule.risk || value.remedial_step !== rule.remedialStep || value.waivable !== rule.waivable) p2cFail(path, "does not match its fixed P2b rule");
}

function p2cValidateEvaluation(value: any): void {
    const shown = value.shown_questions;
    const shownIDs = new Set<string>();
    let priority = 0;
    for (const question of shown) {
        p2cValidateQuestion(question, "$.shown_questions");
        if (question.proposal_id !== value.proposal_id || question.revision !== value.revision || question.revision_hash !== value.revision_hash) p2cFail("$.shown_questions", "must match Evaluation identity");
        const rule = p2cRules.find((candidate) => candidate.ruleClass === question.rule_class)!;
        if (rule.priority <= priority || shownIDs.has(question.question_id)) p2cFail("$.shown_questions", "must retain unique P2b priority order");
        shownIDs.add(question.question_id);
        priority = rule.priority;
    }
    const newIDs = value.new_question_ids;
    if (new Set(newIDs).size !== newIDs.length) p2cFail("$.new_question_ids", "must be unique");
    let shownOffset = 0;
    for (const questionID of newIDs) {
        if (p2cRuleForQuestionID(questionID) === undefined) p2cFail("$.new_question_ids", "has an invalid Question ID");
        const offset = shown.slice(shownOffset).findIndex((question: any) => question.question_id === questionID);
        if (offset === -1) p2cFail("$.new_question_ids", "must preserve shown Question order");
        shownOffset += offset + 1;
    }
    const deferred = value.deferred_rule_classes;
    const deferredRules = new Set<string>();
    let deferredPriority = 0;
    for (const ruleClass of deferred) {
        const rule = p2cRules.find((candidate) => candidate.ruleClass === ruleClass);
        if (rule === undefined || deferredRules.has(ruleClass) || shown.some((question: any) => question.rule_class === ruleClass) || rule.priority <= deferredPriority) p2cFail("$.deferred_rule_classes", "must remain ordered, unique, and unshown");
        deferredRules.add(ruleClass);
        deferredPriority = rule.priority;
    }
    if (value.new_records !== 0 && value.new_records !== newIDs.length + 1) p2cFail("$.new_records", "must include one Evaluation plus each new Question");
    if (value.status === "clear" && (shown.length !== 0 || newIDs.length !== 0 || deferred.length !== 0 || value.new_records !== 0)) p2cFail("$", "clear Evaluations cannot retain active or appended Questions");
}

function validateP2c<T>(value: T, schema: any, topLevel: string): T {
    p2cValidateSchema(value, schema, "$");
    if (topLevel === "GrillQuestion") p2cValidateQuestion(value, "$");
    if (topLevel === "GrillEvaluation") p2cValidateEvaluation(value);
    return value;
}
