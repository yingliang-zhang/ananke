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
  } catch (e) { if(!silent) error=String(e); online=false; } if(tab!="chat") render();
}
async function launch(){ if(!boot) return; const launched = await invokeDecoded("launch_fixture",RunConvert.toRun); selected = launched.id; await refresh(); }
async function cancel(){ if(selected) { await invokeDecoded("cancel_run",CancelConvert.toCancel,{runId:selected}); await refresh(); } }
function esc(v:unknown){ return String(v).replace(/[&<>]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;"}[c]!)); }

// ── Conversations (left-rail conversation entries) ──────────────
interface ConversationEntry { jobId: string; firstMessage: string; timestamp: string; }
let conversations: ConversationEntry[] = [];
let selectedConversation = "";
const idempotencyKeys = new Set<string>();

function render(){ const run=runs.find(r=>r.id===selected); const counts=runs.reduce((a,r)=>(a[isActiveRunState(r.state)?"active":"done"]++,a),{active:0,done:0});
 app.innerHTML=`<header id="ananke-bootstrap-state"${mac2Selector("bootstrapState")} aria-busy="${boot?"false":"true"}"><b>ANANKE</b><span id="ananke-daemon-health"${mac2Selector("daemonHealth")} class="health ${online?"on":"off"}" aria-live="polite">● daemon ${online?"online":"offline"}</span><span id="ananke-run-summary">${counts.active} active · ${counts.done} settled</span><button id="ananke-refresh"${mac2Selector("refresh")} data-a="refresh">Refresh</button></header><main>
 <aside><small>PROJECTS</small><strong>${boot?.project.name??"Ananke"}</strong><div class="workstream">↳ ${boot?.workstream.name??"main"}</div><p>Durable Go lifecycle core</p></aside>
 <section id="ananke-run-list"${mac2Selector("runList")} class="runs"><div class="sectionhead"><small>RUNS</small><button id="ananke-launch-fixture"${mac2Selector("launchFixture")} data-a="launch">Launch fixture</button></div>${runs.length?runs.map(r=>`<button class="run ${r.id===selected?"selected":""}" data-run="${r.id}"><i class="s-${r.state}">${glyph(r.state)}</i><span>${esc(r.id.slice(0,18))}</span><em>${r.state}</em></button>`).join(""):`<div class="empty">No runs yet.<br/>Launch the real fixture.</div>`}${conversations.length?`<div class="sectionhead" style="margin-top:14px"><small>CONVERSATIONS</small></div>${conversations.map(c=>`<button class="run ${c.jobId===selectedConversation?"selected":""}" data-conversation="${c.jobId}"><i class="s-running">●</i><span>${esc(c.firstMessage.slice(0,18))}</span><em>chat</em></button>`).join("")}`:""}</section>
 <section id="ananke-run-detail" class="detail">${run?`<div class="detailhead"><div><small>RUN</small><h2 id="ananke-selected-run-id"${mac2Selector("selectedRunId")}>${esc(run.id)}</h2><span id="ananke-selected-run-state"${mac2Selector("selectedRunState")} class="badge s-${run.state}" aria-live="polite">${glyph(run.state)} ${run.state}</span></div><button id="ananke-cancel-run"${mac2Selector("cancelRun")} data-a="cancel" ${isActiveRunState(run.state)?"":"disabled"}>Cancel</button></div><nav><button data-tab="activity" class="${tab=="activity"?"active":""}">Activity</button><button data-tab="transcript" class="${tab=="transcript"?"active":""}">Transcript</button><button data-tab="chat" class="${tab=="chat"?"active":""}">Chat</button></nav><div class="feed">${tab=="chat"?renderChatPanel():events.length?events.map(e=>tab=="activity"?`<article><b>${e.seq}</b><span>${esc(e.type)}</span><pre>${esc(JSON.stringify(e.payload,null,2))}</pre></article>`:`<pre>${esc(JSON.stringify(e,null,2))}</pre>`).join(""):`<div class="empty">Waiting for canonical events.</div>`}</div><details><summary>Diagnostics</summary><pre>${esc(JSON.stringify(run.diagnostics,null,2))}</pre></details>`:`<div class="empty">Select a run to inspect durable activity.</div>`}</section></main>${error?`<div class="error">${esc(error)}</div>`:""}`;
 const grillPanel=document.createElement("section"); grillPanel.id="ananke-grill-review"; grillPanel.className="grill-review"; grillPanel.setAttribute(mac2SelectorContract.selectorAttribute,mac2SelectorContract.selectors.grillReview); grillPanel.innerHTML=renderGrillReview(grill.state); app.querySelector<HTMLElement>(".detail")?.prepend(grillPanel); app.querySelectorAll<HTMLElement>("[data-run]").forEach(x=>x.onclick=()=>{selected=x.dataset.run!;refresh(true)}); app.querySelectorAll<HTMLButtonElement>("[data-a]").forEach(x=>x.onclick=()=>{ if(x.dataset.a==="launch") void launch(); else if(x.dataset.a==="cancel") void cancel(); else void refresh(); }); app.querySelectorAll<HTMLButtonElement>("[data-tab]").forEach(x=>x.onclick=()=>{tab=x.dataset.tab!;render()}); app.querySelectorAll<HTMLElement>("[data-conversation]").forEach(x=>x.onclick=()=>{selectedConversation=x.dataset.conversation!;activeJobId=selectedConversation;chatMessages=[];tab="chat";void restoreChatMessages(false);render();}); bindGrillReview(app,grill); if(tab=="chat") { bindChatPanel(); document.querySelector<HTMLInputElement>("#chat-input")?.focus(); } }
setInterval(()=>refresh(true),1500); refresh().then(()=>{ // Step 4a: Restore last conversation on startup
  const storedJobId = localStorage.getItem("ananke_active_job");
  if (storedJobId) { activeJobId = storedJobId; tab = "chat"; void restoreChatMessages(true); }
});

// ── Chat — Conversational Panel ─────────────────────────────────
// Chat-based coding interaction (per docs/first-principles-redesign.md)
let activeJobId = "", chatPolling = false, activeJobText = "";
interface ChatMsg { role: "user"|"agent"|"system"; type: string; content: string; attestationHash?: string; diffPath?: string; }
let chatMessages: ChatMsg[] = [];

function renderChatPanel(): string {
  const msgs = chatMessages.map(m => {
    if (m.role === "user") return `<div class="chat-msg user"><b>You</b><p>${esc(m.content)}</p></div>`;
    if (m.role === "system") return `<div class="chat-msg system"><span class="badge s-failed">${esc(m.content)}</span></div>`;
    // agent message with optional evidence/diff
    let actions = "";
    if (m.attestationHash) {
      actions = `<div style="margin-top:6px;display:flex;gap:8px;flex-wrap:wrap">
        <button class="chat-view-diff" data-diff="${esc(m.diffPath ?? "")}">View Diff</button>
        <button class="chat-accept" data-hash="${esc(m.attestationHash)}">Accept</button>
        <button class="chat-reject" data-hash="${esc(m.attestationHash)}">Reject</button>
        <button class="chat-ask" data-hash="${esc(m.attestationHash)}">Ask for changes</button>
      </div>`;
    }
    return `<div class="chat-msg agent"><b>Agent</b><p>${esc(m.content)}</p>${actions}</div>`;
  }).join("");
  return `<div class="chat-container" style="display:flex;flex-direction:column;height:100%">
    <div id="chat-thread" style="flex:1;overflow-y:auto;padding:8px;max-height:400px">${msgs || '<div class="empty">Send a message to start a conversation.</div>'}</div>
    <div id="chat-diff-view" style="margin-top:4px"></div>
    <div style="display:flex;gap:8px;padding:8px 0">
      <input type="text" id="chat-input" placeholder="Describe the task..." style="flex:1" />
      <select id="chat-adapter" style="width:auto">
        <option value="omp">OMP (K3)</option>
        <option value="fake">Fake</option>
      </select>
      <button id="chat-send-btn" class="primary">Send</button>
    </div>
  </div>`;
}

function bindChatPanel() {
  const btn = document.querySelector<HTMLButtonElement>("#chat-send-btn");
  const input = document.querySelector<HTMLInputElement>("#chat-input");
  if (btn) btn.onclick = () => void sendChatMessage();
  if (input) input.onkeydown = (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void sendChatMessage(); } };
  // Step 4a: Auto-focus moved to render() to avoid stealing focus on every poll tick.
  // (focus handled in render)
  // F2 fix: restore conversation from daemon on panel mount.
  if (activeJobId && chatMessages.length === 0) {
    void restoreChatMessages(false);
  }
  // Bind evidence action buttons
  document.querySelectorAll<HTMLButtonElement>(".chat-view-diff").forEach(b => b.onclick = async () => {
    const dp = b.dataset.diff; if (!dp) return;
    const el = document.querySelector<HTMLElement>("#chat-diff-view");
    if (!el) return;
    try { const diff = await invoke<string>("read_repair_diff", { diffPath: dp }); el.innerHTML = `<pre style="background:#0d1117;padding:12px;border-radius:4px;font-size:0.75em;max-height:250px;overflow:auto;white-space:pre-wrap">${esc(diff)}</pre>`; }
    catch (e: any) { el.innerHTML = `<span style="color:#ff5555">${esc(String(e))}</span>`; }
  });
  document.querySelectorAll<HTMLButtonElement>(".chat-accept").forEach(b => b.onclick = async () => {
    try { await invoke("review_repair", { attestationHash: b.dataset.hash, action: "accept" }); chatMessages.push({role:"system",type:"info",content:"✓ Accepted. Outbox delivered."}); renderChatTab(); }
    catch (e: any) { alert(String(e)); }
  });
  document.querySelectorAll<HTMLButtonElement>(".chat-reject").forEach(b => b.onclick = async () => {
    try { await invoke("review_repair", { attestationHash: b.dataset.hash, action: "reject" }); chatMessages.push({role:"system",type:"info",content:"✗ Rejected."}); renderChatTab(); }
    catch (e: any) { alert(String(e)); }
  });
  document.querySelectorAll<HTMLButtonElement>(".chat-ask").forEach(b => b.onclick = async () => {
    // F1 fix: call review_repair with ask_changes action, then focus input for follow-up.
    const hash = b.dataset.hash;
    if (hash) {
      try { await invoke("review_repair", { attestationHash: hash, action: "ask_changes" }); }
      catch (e: any) { /* non-fatal: daemon treat ask_changes as no-op */ }
    }
    const input = document.querySelector<HTMLInputElement>("#chat-input");
    if (input) { input.focus(); input.placeholder = "Describe what to change..."; }
  });
}

function renderChatTab() { const el = document.querySelector<HTMLElement>("#chat-thread"); if (el) el.innerHTML = chatMessages.map(m => {
  if (m.role === "user") return `<div class="chat-msg user"><b>You</b><p>${esc(m.content)}</p></div>`;
  if (m.role === "system") return `<div class="chat-msg system"><span class="badge ${m.content.includes("✓")?"s-settled":"s-failed"}">${esc(m.content)}</span></div>`;
  let actions = "";
  if (m.attestationHash) actions = `<div style="margin-top:6px;display:flex;gap:8px;flex-wrap:wrap"><button class="chat-view-diff" data-diff="${esc(m.diffPath ?? "")}">View Diff</button><button class="chat-accept" data-hash="${esc(m.attestationHash)}">Accept</button><button class="chat-reject" data-hash="${esc(m.attestationHash)}">Reject</button><button class="chat-ask" data-hash="${esc(m.attestationHash)}">Ask for changes</button></div>`;
  return `<div class="chat-msg agent"><b>Agent</b><p>${esc(m.content)}</p>${actions}</div>`;
}).join(""); bindChatPanel(); }

async function sendChatMessage() {
  // F3 fix: disable Send while a job is in flight.
  if (chatPolling) return;
  const input = document.querySelector<HTMLInputElement>("#chat-input");
  const adapterSel = document.querySelector<HTMLSelectElement>("#chat-adapter");
  const sendBtn = document.querySelector<HTMLButtonElement>("#chat-send-btn");
  if (!input) return;
  const text = input.value.trim();
  if (!text) return;
  // Step 4b: Client-side idempotency — prevent re-submission of the same message while in flight.
  // Key is the message content; cleaned up on job completion to allow legitimate re-submission.
  if (idempotencyKeys.has(text)) return;
  idempotencyKeys.add(text);
  activeJobText = text;
  const adapterType = adapterSel?.value ?? "omp";
  const projectPath = boot?.project.root ?? "";
  if (!projectPath) { chatMessages.push({role:"system",type:"error",content:"No project root — select a project first."}); renderChatTab(); return; }

  // Add user message to conversation
  chatMessages.push({role:"user",type:"user_request",content:text});
  input.value = "";
  if (sendBtn) sendBtn.disabled = true;
  chatMessages.push({role:"agent",type:"agent_reasoning",content:"Starting..."});
  renderChatTab();

  try {
    const job = await invoke<RepairJobDto>("submit_repair", { projectPath, requestText: text, adapterType });
    activeJobId = job.job_id;
    // Step 4b: Add to left-rail conversations and persist for startup restore.
    conversations.push({ jobId: job.job_id, firstMessage: text, timestamp: new Date().toISOString() });
    selectedConversation = job.job_id;
    localStorage.setItem("ananke_active_job", job.job_id);
    // Update the "starting" message
    chatMessages[chatMessages.length-1].content = `Job ${job.job_id} started. Waiting for K3...`;
    render();
    pollChat();
  } catch (e: any) {
    chatMessages[chatMessages.length-1].content = `Error: ${esc(String(e))}`;
    renderChatTab();
  }
}

async function pollChat() {
  if (chatPolling || !activeJobId) return;
  chatPolling = true;
  const pollJobId = activeJobId;
  const poll = async () => {
    try {
      const job = await invoke<RepairJobDto>("poll_repair_job", { jobId: pollJobId });
      if (job.status === "running") {
        // Update last agent message
        const last = chatMessages[chatMessages.length-1];
        if (last && last.role === "agent") last.content = `Running... (started: ${job.started_at})`;
        renderChatTab();
        setTimeout(poll, 3000);
      } else if (job.status === "completed") {
        chatPolling = false; idempotencyKeys.delete(activeJobText);
        const sendBtn = document.querySelector<HTMLButtonElement>("#chat-send-btn"); if (sendBtn) sendBtn.disabled = false;
        chatMessages.push({role:"agent",type:"agent_evidence",content:`Completed. Attestation signed: ${job.attestation_hash}`,attestationHash:job.attestation_hash,diffPath:job.diff_path});
        renderChatTab();
      } else {
        chatPolling = false; idempotencyKeys.delete(activeJobText);
        const sendBtn = document.querySelector<HTMLButtonElement>("#chat-send-btn"); if (sendBtn) sendBtn.disabled = false;
        chatMessages.push({role:"system",type:"error",content:`Failed: ${job.error}`});
        renderChatTab();
      }
    } catch { chatPolling = false; idempotencyKeys.delete(activeJobText); const sendBtn = document.querySelector<HTMLButtonElement>("#chat-send-btn"); if (sendBtn) sendBtn.disabled = false; chatMessages.push({role:"system",type:"error",content:"Polling error — connection lost."}); renderChatTab(); }
  };
  poll();
}

interface RepairJobDto { job_id: string; status: string; attestation_hash: string; diff_path: string; error: string; started_at: string; }
interface RepairMessageDto { type: string; content: string; attestation_hash: string; diff_path: string; }

// F2 fix + Step 4a: restore conversation from daemon.
// switchTab=true on startup restore to auto-navigate to the chat tab.
async function restoreChatMessages(switchTab = false) {
  if (!activeJobId) return;
  try {
    const msgs = await invoke<RepairMessageDto[]>("get_repair_messages", { jobId: activeJobId });
    if (msgs && msgs.length > 0) {
      chatMessages = msgs.map(m => ({
        role: m.type === "error" ? "system" : "agent" as const,
        type: m.type,
        content: m.content,
        attestationHash: m.attestation_hash || undefined,
        diffPath: m.diff_path || undefined,
      }));
      if (switchTab) {
        // Step 4a: auto-navigate to chat tab and populate left-rail conversation.
        tab = "chat";
        if (!conversations.find(c => c.jobId === activeJobId)) {
          conversations.push({ jobId: activeJobId, firstMessage: chatMessages[0]?.content ?? "Conversation", timestamp: new Date().toISOString() });
        }
        selectedConversation = activeJobId;
        render();
      // Step 4a: Resume polling if the restored job is still running.
      try { const rj = await invoke<RepairJobDto>("poll_repair_job", { jobId: activeJobId }); if (rj.status === "running") pollChat(); } catch { /* job evicted */ }
      } else {
        renderChatTab();
      }
    }
  } catch { if (switchTab) localStorage.removeItem("ananke_active_job"); }
}
