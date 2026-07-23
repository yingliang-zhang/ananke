import { invoke } from "@tauri-apps/api/core";
import { Convert as BootstrapConvert } from "./generated/renderer-public-bootstrap";
import type { Bootstrap } from "./generated/renderer-public-bootstrap";
import { Convert as CancelConvert } from "./generated/renderer-public-cancel";
import { Convert as RunConvert } from "./generated/renderer-public-run";
import type { Run } from "./generated/renderer-public-run";
import { Convert as EventConvert } from "./generated/renderer-public-event";
import type { Event } from "./generated/renderer-public-event";
import { Convert as HealthConvert } from "./generated/renderer-public-health";
import { isActiveRunState } from "./run-state";
import { Convert as ProposalListInputConvert } from "./generated/renderer-public-proposal-list-input";
import { Convert as ProposalListConvert } from "./generated/renderer-public-proposal-list";
import { Convert as ProposalGetInputConvert } from "./generated/renderer-public-proposal-get-input";
import { Convert as ProposalDetailConvert } from "./generated/renderer-public-proposal-detail";
import type { ProposalDetail } from "./generated/renderer-public-proposal-detail";
import { Convert as GrillEvaluateInputConvert } from "./generated/renderer-public-grill-evaluate-input";
import { Convert as GrillEvaluationConvert } from "./generated/renderer-public-grill-evaluation";
import { Convert as GrillDefaultInputConvert } from "./generated/renderer-public-grill-record-default-input";
import { Convert as GrillDefaultRecordConvert } from "./generated/renderer-public-grill-default-record";
import { Convert as GrillAnswerInputConvert } from "./generated/renderer-public-grill-record-answer-input";
import { Convert as GrillAnswerRecordConvert } from "./generated/renderer-public-grill-answer-record";
import { Convert as GrillOverrideInputConvert } from "./generated/renderer-public-grill-record-override-input";
import { Convert as GrillOverrideRecordConvert } from "./generated/renderer-public-grill-override-record";
import { bindGrillReview, GrillReviewController, renderGrillReview } from "./grill-review";
import type { GrillRevisionIdentity } from "./grill-review";
import "./styles.css";
import mac2SelectorContract from "../../contracts/mac2-accessibility.json";

let boot: Bootstrap | null = null, runs: Run[] = [], selected = "", events: Event[] = [], tab = "activity", error = "", online = false;
const app = document.querySelector<HTMLDivElement>("#app")!;
const glyph = (s:string) => ({running:"●",cancelling:"◌",cleanup_required:"!",failed:"×",cancelled:"−",completed:"✓"}[s] ?? "·");
const attention = (s:string) => ({cleanup_required:0,failed:1,cancelling:2,running:3,cancelled:4,completed:5}[s] ?? 9);
const mac2Selector = (name: keyof typeof mac2SelectorContract.selectors) => ` ${mac2SelectorContract.selectorAttribute}="${mac2SelectorContract.selectors[name]}"`;
async function invokeDecoded<T>(command:string, decode:(json:string)=>T, args?:Record<string,unknown>): Promise<T> {
 const json = JSON.stringify(await invoke<unknown>(command,args));
 if(json===undefined) throw new Error("Tauri command returned no JSON");
 return decode(json);
}
const grill = new GrillReviewController({
 evaluate: async input => invokeDecoded("evaluate_grill",GrillEvaluationConvert.toGrillEvaluation,{input:GrillEvaluateInputConvert.toEvaluateGrillInput(JSON.stringify(input))}),
 recordDefault: async input => { await invokeDecoded("record_grill_default",GrillDefaultRecordConvert.toGrillDefaultRecord,{input:GrillDefaultInputConvert.toRecordGrillDefaultInput(JSON.stringify(input))}); },
 recordAnswer: async input => { await invokeDecoded("record_grill_answer",GrillAnswerRecordConvert.toGrillAnswerRecord,{input:GrillAnswerInputConvert.toRecordGrillAnswerInput(JSON.stringify(input))}); },
 recordOverride: async input => { await invokeDecoded("record_grill_override",GrillOverrideRecordConvert.toGrillOverrideRecord,{input:GrillOverrideInputConvert.toRecordGrillOverrideInput(JSON.stringify(input))}); },
},render);
function grillRevision(detail:ProposalDetail): GrillRevisionIdentity | null { const {approval,lifecycle,proposal,revision}=detail;
 if(proposal.proposal_id!==revision.proposal_id||proposal.proposal_id!==lifecycle.proposal_id||proposal.proposal_id!==approval.proposal_id||proposal.current_revision!==revision.revision||proposal.current_revision!==lifecycle.revision||proposal.current_revision!==approval.revision||proposal.current_revision_hash!==lifecycle.revision_hash||proposal.current_revision_hash!==approval.revision_hash) return null;
 return {proposal_id:proposal.proposal_id,revision:proposal.current_revision,revision_hash:proposal.current_revision_hash};
}
async function refreshGrill(){ if(!boot) return; try { const listInput=ProposalListInputConvert.toListProposalsInput(JSON.stringify({project_id:boot.project.id,workstream_id:boot.workstream.id})); const list=await invokeDecoded("list_proposals",ProposalListConvert.toProposalList,{input:listInput}); const proposal=list.proposals.filter(candidate=>candidate.project_id===boot!.project.id&&candidate.workstream_id===boot!.workstream.id).sort((left,right)=>left.proposal_id.localeCompare(right.proposal_id))[0]; if(!proposal){grill.setRevision(null);return;} const detailInput=ProposalGetInputConvert.toGetProposalInput(JSON.stringify({proposal_id:proposal.proposal_id})); const changed=grill.setRevision(grillRevision(await invokeDecoded("get_proposal",ProposalDetailConvert.toProposalDetail,{input:detailInput}))); if(changed&&grill.state.revision) void grill.refresh(); } catch { grill.setRevision(null); grill.markUnavailable(); } }
async function refresh(silent=false) {
 try { boot ??= await invokeDecoded("bootstrap",BootstrapConvert.toBootstrap); online = (await invokeDecoded("daemon_health",HealthConvert.toHealth)).online;
  runs = await invokeDecoded("list_runs",json=>{ const result:unknown=JSON.parse(json); if(!Array.isArray(result)) throw new Error("Tauri command returned a non-array result"); return result.map(entry=>{ const entryJson=JSON.stringify(entry); if(entryJson===undefined) throw new Error("Tauri command returned no JSON"); return RunConvert.toRun(entryJson); }); });
  runs.sort((a,b)=>attention(a.state)-attention(b.state)); selected ||= runs[0]?.id ?? "";
  const run = selected ? await invokeDecoded("get_run",RunConvert.toRun,{runId:selected}) : undefined; events = run ? await invokeDecoded("list_events",json=>{ const result:unknown=JSON.parse(json); if(!Array.isArray(result)) throw new Error("Tauri command returned a non-array result"); return result.map(entry=>{ const entryJson=JSON.stringify(entry); if(entryJson===undefined) throw new Error("Tauri command returned no JSON"); return EventConvert.toEvent(entryJson); }); },{runId:run.id,afterSeq:0}) : []; await refreshGrill(); error="";
 } catch (e) { if(!silent) error=String(e); online=false; } render();
}
async function launch(){ if(!boot) return; const launched = await invokeDecoded("launch_fixture",RunConvert.toRun); selected = launched.id; await refresh(); }
async function cancel(){ if(selected) { await invokeDecoded("cancel_run",CancelConvert.toCancel,{runId:selected}); await refresh(); } }
function esc(v:unknown){ return String(v).replace(/[&<>]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;"}[c]!)); }
function render(){ const run=runs.find(r=>r.id===selected); const counts=runs.reduce((a,r)=>(a[isActiveRunState(r.state)?"active":"done"]++,a),{active:0,done:0});
 app.innerHTML=`<header id="ananke-bootstrap-state"${mac2Selector("bootstrapState")} aria-busy="${boot?"false":"true"}"><b>ANANKE</b><span id="ananke-daemon-health"${mac2Selector("daemonHealth")} class="health ${online?"on":"off"}" aria-live="polite">● daemon ${online?"online":"offline"}</span><span id="ananke-run-summary">${counts.active} active · ${counts.done} settled</span><button id="ananke-refresh"${mac2Selector("refresh")} data-a="refresh">Refresh</button></header><main>
 <aside><small>PROJECTS</small><strong>${boot?.project.name??"Ananke"}</strong><div class="workstream">↳ ${boot?.workstream.name??"main"}</div><p>Durable Go lifecycle core</p></aside>
 <section id="ananke-run-list"${mac2Selector("runList")} class="runs"><div class="sectionhead"><small>RUNS</small><button id="ananke-launch-fixture"${mac2Selector("launchFixture")} data-a="launch">Launch fixture</button></div>${runs.length?runs.map(r=>`<button class="run ${r.id===selected?"selected":""}" data-run="${r.id}"><i class="s-${r.state}">${glyph(r.state)}</i><span>${esc(r.id.slice(0,18))}</span><em>${r.state}</em></button>`).join(""):`<div class="empty">No runs yet.<br/>Launch the real fixture.</div>`}</section>
 <section id="ananke-run-detail" class="detail">${run?`<div class="detailhead"><div><small>RUN</small><h2 id="ananke-selected-run-id"${mac2Selector("selectedRunId")}>${esc(run.id)}</h2><span id="ananke-selected-run-state"${mac2Selector("selectedRunState")} class="badge s-${run.state}" aria-live="polite">${glyph(run.state)} ${run.state}</span></div><button id="ananke-cancel-run"${mac2Selector("cancelRun")} data-a="cancel" ${isActiveRunState(run.state)?"":"disabled"}>Cancel</button></div><nav><button data-tab="activity" class="${tab==="activity"?"active":""}">Activity</button><button data-tab="transcript" class="${tab==="transcript"?"active":""}">Transcript</button></nav><div class="feed">${events.length?events.map(e=>tab==="activity"?`<article><b>${e.seq}</b><span>${esc(e.type)}</span><pre>${esc(JSON.stringify(e.payload,null,2))}</pre></article>`:`<pre>${esc(JSON.stringify(e,null,2))}</pre>`).join(""):`<div class="empty">Waiting for canonical events.</div>`}</div><details><summary>Diagnostics</summary><pre>${esc(JSON.stringify(run.diagnostics,null,2))}</pre></details>`:`<div class="empty">Select a run to inspect durable activity.</div>`}</section></main>${error?`<div class="error">${esc(error)}</div>`:""}`;
 const grillPanel=document.createElement("section"); grillPanel.id="ananke-grill-review"; grillPanel.className="grill-review"; grillPanel.setAttribute(mac2SelectorContract.selectorAttribute,mac2SelectorContract.selectors.grillReview); grillPanel.innerHTML=renderGrillReview(grill.state); app.querySelector<HTMLElement>(".detail")?.prepend(grillPanel); app.querySelectorAll<HTMLElement>("[data-run]").forEach(x=>x.onclick=()=>{selected=x.dataset.run!;refresh(true)}); app.querySelectorAll<HTMLButtonElement>("[data-a]").forEach(x=>x.onclick=()=>{ if(x.dataset.a==="launch") void launch(); else if(x.dataset.a==="cancel") void cancel(); else void refresh(); }); app.querySelectorAll<HTMLButtonElement>("[data-tab]").forEach(x=>x.onclick=()=>{tab=x.dataset.tab!;render()}); bindGrillReview(app,grill); }
setInterval(()=>refresh(true),1500); refresh();
