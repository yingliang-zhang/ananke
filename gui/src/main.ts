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
async function refresh(silent=false) {
 try { boot ??= await invokeDecoded("bootstrap",BootstrapConvert.toBootstrap); online = (await invokeDecoded("daemon_health",HealthConvert.toHealth)).online;
  runs = await invokeDecoded("list_runs",json=>{ const result:unknown=JSON.parse(json); if(!Array.isArray(result)) throw new Error("Tauri command returned a non-array result"); return result.map(entry=>{ const entryJson=JSON.stringify(entry); if(entryJson===undefined) throw new Error("Tauri command returned no JSON"); return RunConvert.toRun(entryJson); }); });
  runs.sort((a,b)=>attention(a.state)-attention(b.state)); selected ||= runs[0]?.id ?? "";
  const run = selected ? await invokeDecoded("get_run",RunConvert.toRun,{runId:selected}) : undefined; events = run ? await invokeDecoded("list_events",json=>{ const result:unknown=JSON.parse(json); if(!Array.isArray(result)) throw new Error("Tauri command returned a non-array result"); return result.map(entry=>{ const entryJson=JSON.stringify(entry); if(entryJson===undefined) throw new Error("Tauri command returned no JSON"); return EventConvert.toEvent(entryJson); }); },{runId:run.id,afterSeq:0}) : []; error="";
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
 app.querySelectorAll<HTMLElement>("[data-run]").forEach(x=>x.onclick=()=>{selected=x.dataset.run!;refresh(true)}); app.querySelectorAll<HTMLButtonElement>("[data-a]").forEach(x=>x.onclick=()=>{ if(x.dataset.a==="launch") void launch(); else if(x.dataset.a==="cancel") void cancel(); else void refresh(); }); app.querySelectorAll<HTMLButtonElement>("[data-tab]").forEach(x=>x.onclick=()=>{tab=x.dataset.tab!;render()}); }
setInterval(()=>refresh(true),1500); refresh();
