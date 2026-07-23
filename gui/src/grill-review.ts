import type { GrillEvaluation, GrillQuestion } from "./generated/renderer-public-grill-evaluation";

export interface GrillRevisionIdentity {
  proposal_id: string;
  revision: number;
  revision_hash: string;
}

export interface GrillRecordInput extends GrillRevisionIdentity {
  question_id: string;
}

export interface GrillReviewClient {
  evaluate(input: GrillRevisionIdentity): Promise<GrillEvaluation>;
  recordDefault(input: GrillRecordInput): Promise<void>;
  recordAnswer(input: GrillRecordInput): Promise<void>;
  recordOverride(input: GrillRecordInput): Promise<void>;
}

export type GrillAction = "default" | "acknowledge" | "waive";

export interface GrillReviewState {
  error: string;
  evaluation: GrillEvaluation | null;
  pending: boolean;
  revision: GrillRevisionIdentity | null;
}

const unavailableMessage = "Grill review is unavailable. Retry the deterministic review.";

function sameIdentity(left: GrillRevisionIdentity | null, right: GrillRevisionIdentity | null): boolean {
  return left?.proposal_id === right?.proposal_id &&
    left?.revision === right?.revision &&
    left?.revision_hash === right?.revision_hash;
}

function evaluationMatchesIdentity(evaluation: GrillEvaluation, identity: GrillRevisionIdentity): boolean {
  return evaluation.proposal_id === identity.proposal_id &&
    evaluation.revision === identity.revision &&
    evaluation.revision_hash === identity.revision_hash;
}

function questionMatchesIdentity(question: GrillQuestion, identity: GrillRevisionIdentity): boolean {
  return question.proposal_id === identity.proposal_id &&
    question.revision === identity.revision &&
    question.revision_hash === identity.revision_hash;
}

function isScopeCompatibleWaiver(question: GrillQuestion): boolean {
  return question.waivable &&
    question.rule_class === "scope_compatibility" &&
    question.question_id === "grill_question_scope_compatibility";
}

export class GrillReviewController {
  private operation = 0;
  private snapshot: GrillReviewState = {
    error: "",
    evaluation: null,
    pending: false,
    revision: null,
  };

  constructor(
    private readonly client: GrillReviewClient,
    private readonly onChange: () => void = () => undefined,
  ) {}

  get state(): Readonly<GrillReviewState> {
    return this.snapshot;
  }

  setRevision(revision: GrillRevisionIdentity | null): boolean {
    const next = revision == null ? null : {
      proposal_id: revision.proposal_id,
      revision: revision.revision,
      revision_hash: revision.revision_hash,
    };
    if (sameIdentity(this.snapshot.revision, next)) return false;
    this.operation += 1;
    this.snapshot = {
      error: "",
      evaluation: null,
      pending: false,
      revision: next,
    };
    this.publish();
    return true;
  }
  markUnavailable(): void {
    this.operation += 1;
    this.snapshot = { ...this.snapshot, error: unavailableMessage, evaluation: null, pending: false };
    this.publish();
  }


  async refresh(): Promise<void> {
    const identity = this.snapshot.revision;
    if (identity == null || this.snapshot.pending) return;
    const operation = this.beginOperation();
    try {
      await this.evaluateFor(operation, identity);
    } catch {
      this.fail(operation);
    } finally {
      this.finish(operation);
    }
  }

  async record(action: GrillAction, questionID: string): Promise<void> {
    const { evaluation, revision } = this.snapshot;
    if (evaluation == null || revision == null || this.snapshot.pending) return;
    const question = evaluation.shown_questions.find((candidate) => candidate.question_id === questionID);
    if (question == null || !questionMatchesIdentity(question, revision)) return;
    if (action === "waive" && !isScopeCompatibleWaiver(question)) return;

    const input: GrillRecordInput = {
      proposal_id: revision.proposal_id,
      revision: revision.revision,
      revision_hash: revision.revision_hash,
      question_id: question.question_id,
    };
    const operation = this.beginOperation();
    try {
      switch (action) {
        case "default":
          await this.client.recordDefault(input);
          break;
        case "acknowledge":
          await this.client.recordAnswer(input);
          break;
        case "waive":
          await this.client.recordOverride(input);
          break;
      }
      if (operation !== this.operation) return;
      await this.evaluateFor(operation, revision);
    } catch {
      this.fail(operation);
    } finally {
      this.finish(operation);
    }
  }

  private beginOperation(): number {
    const operation = ++this.operation;
    this.snapshot = { ...this.snapshot, error: "", pending: true };
    this.publish();
    return operation;
  }

  private async evaluateFor(operation: number, identity: GrillRevisionIdentity): Promise<void> {
    const evaluation = await this.client.evaluate(identity);
    if (operation !== this.operation) return;
    if (!evaluationMatchesIdentity(evaluation, identity)) throw new Error("unexpected Grill evaluation identity");
    this.snapshot = { ...this.snapshot, error: "", evaluation };
    this.publish();
  }

  private fail(operation: number): void {
    if (operation !== this.operation) return;
    this.snapshot = { ...this.snapshot, error: unavailableMessage };
    this.publish();
  }

  private finish(operation: number): void {
    if (operation !== this.operation) return;
    this.snapshot = { ...this.snapshot, pending: false };
    this.publish();
  }

  private publish(): void {
    this.onChange();
  }
}

function esc(value: unknown): string {
  return String(value).replace(/[&<>]/g, (character) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
  })[character]!);
}

function label(value: string): string {
  return value.replaceAll("_", " ");
}

function questionsForDisplay(evaluation: GrillEvaluation | null): GrillQuestion[] {
  return evaluation == null
    ? []
    : [...evaluation.shown_questions]
      .sort((left, right) => left.question_sequence - right.question_sequence)
      .slice(0, 5);
}

function actionButton(action: GrillAction, question: GrillQuestion, text: string, disabled: boolean): string {
  return `<button data-grill-action="${action}" data-grill-question="${esc(question.question_id)}"${disabled ? " disabled" : ""}>${text}</button>`;
}

function renderQuestion(question: GrillQuestion, disabled: boolean): string {
  const waiver = isScopeCompatibleWaiver(question)
    ? actionButton("waive", question, "Waive scope compatibility", disabled)
    : "";
  return `<article class="grill-question"><header><small>QUESTION ${question.question_sequence}</small><h3>${esc(label(question.rule_class))}</h3></header><dl class="grill-question-context"><dt>Risk</dt><dd>${esc(question.risk)}</dd><dt>Default</dt><dd>${esc(label(question.default))}</dd><dt>Remedial step</dt><dd>${esc(label(question.remedial_step))}</dd><dt>Waivable</dt><dd>${question.waivable ? "yes" : "no"}</dd></dl><div class="grill-actions">${actionButton("default", question, `Record default: ${esc(label(question.default))}`, disabled)}${actionButton("acknowledge", question, "Acknowledge", disabled)}${waiver}</div></article>`;
}

export function renderGrillReview(state: Readonly<GrillReviewState>): string {
  const { evaluation, revision } = state;
  const disabled = state.pending || revision == null;
  const status = evaluation?.status ?? (revision == null ? "guarded" : "not evaluated");
  const identity = revision == null
    ? `<p class="grill-guard" role="status">Awaiting a current proposal revision.</p>`
    : `<dl class="grill-identity"><dt>Proposal ID</dt><dd>${esc(revision.proposal_id)}</dd><dt>Revision</dt><dd>${revision.revision}</dd><dt>Revision hash</dt><dd><code>${esc(revision.revision_hash)}</code></dd><dt>Review status</dt><dd data-grill-status aria-live="polite">${esc(status)}</dd></dl>`;
  const questions = questionsForDisplay(evaluation);
  const content = evaluation == null
    ? `<div class="empty">${revision == null ? "Grill review is guarded until a current revision is available." : "Refresh the deterministic review to load its bounded questions."}</div>`
    : questions.length === 0
      ? `<div class="empty">No active Grill questions.</div>`
      : `<div class="grill-questions">${questions.map((question) => renderQuestion(question, disabled)).join("")}</div>`;
  const error = state.error === "" ? "" : `<p class="grill-error" role="alert">${esc(state.error)}</p>`;
  return `<div class="grill-head"><div><small>DETERMINISTIC GRILL</small><h2>Grill review</h2></div><button data-grill-action="refresh"${disabled ? " disabled" : ""}>${state.pending ? "Review pending…" : "Refresh review"}</button></div>${identity}${error}${content}`;
}

export function bindGrillReview(root: ParentNode, controller: GrillReviewController): void {
  root.querySelectorAll<HTMLButtonElement>("[data-grill-action]").forEach((button) => {
    button.onclick = () => {
      if (button.disabled) return;
      const action = button.dataset.grillAction;
      if (action === "refresh") {
        void controller.refresh();
        return;
      }
      const questionID = button.dataset.grillQuestion;
      if (questionID != null && (action === "default" || action === "acknowledge" || action === "waive")) {
        void controller.record(action, questionID);
      }
    };
  });
}
