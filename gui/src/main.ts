import { invoke } from "@tauri-apps/api/core";
import { isActiveRunState } from "./run-state";
import "./styles.css";

type Run = { id:string; state:string; diagnostics:{ project_id:string; workstream_id:string; worker_pid:number; supervisor_pid:number; committed_offset:number } };
type Event = { seq:number; type:string; payload:unknown };
type Bootstrap = { project:{id:string;name:string}; workstream:{id:string;name:string} };
let boot: Bootstrap | null = null, runs: Run[] = [], selected = "", events: Event[] = [], tab = "activity", error = "", online = false;
const app = document.querySelector<HTMLDivElement>("#app")!;
const glyph = (s:string) => ({running:"●",cancelling:"◌",cleanup_required:"!",failed:"×",cancelled:"−",completed:"✓"}[s] ?? "·");
const attention = (s:string) => ({cleanup_required:0,failed:1,cancelling:2,running:3,cancelled:4,completed:5}[s] ?? 9);
async function refresh(silent=false) {
 try { boot ??= await invoke<Bootstrap>("bootstrap"); online = (await invoke<{online:boolean}>("daemon_health")).online;
  runs = await invoke<Run[]>("list_runs");
  runs.sort((a,b)=>attention(a.state)-attention(b.state)); selected ||= runs[0]?.id ?? "";
  const run = runs.find(r=>r.id===selected); events = run ? await invoke<Event[]>("list_events",{runId:run.id,afterSeq:0}) : []; error="";
 } catch (e) { if(!silent) error=String(e); online=false; } render();
}
async function launch(){ if(!boot) return; await invoke("launch_fixture"); await refresh(); }
async function cancel(){ if(selected) { await invoke("cancel_run",{runId:selected}); await refresh(); } }
function esc(v:unknown){ return String(v).replace(/[&<>]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;"}[c]!)); }
function render(){ const run=runs.find(r=>r.id===selected); const counts=runs.reduce((a,r)=>(a[isActiveRunState(r.state)?"active":"done"]++,a),{active:0,done:0});
 app.innerHTML=`<header><b>ANANKE</b><span class="health ${online?"on":"off"}">● daemon ${online?"online":"offline"}</span><span>${counts.active} active · ${counts.done} settled</span><button data-a="refresh">Refresh</button></header><main>
 <aside><small>PROJECTS</small><strong>${boot?.project.name??"Ananke"}</strong><div class="workstream">↳ ${boot?.workstream.name??"main"}</div><p>Durable Go lifecycle core</p></aside>
 <section class="runs"><div class="sectionhead"><small>RUNS</small><button data-a="launch">Launch fixture</button></div>${runs.length?runs.map(r=>`<button class="run ${r.id===selected?"selected":""}" data-run="${r.id}"><i class="s-${r.state}">${glyph(r.state)}</i><span>${esc(r.id.slice(0,18))}</span><em>${r.state}</em></button>`).join(""):`<div class="empty">No runs yet.<br/>Launch the real fixture.</div>`}</section>
 <section class="detail">${run?`<div class="detailhead"><div><small>RUN</small><h2>${esc(run.id)}</h2><span class="badge s-${run.state}">${glyph(run.state)} ${run.state}</span></div><button data-a="cancel" ${isActiveRunState(run.state)?"":"disabled"}>Cancel</button></div><nav><button data-tab="activity" class="${tab==="activity"?"active":""}">Activity</button><button data-tab="transcript" class="${tab==="transcript"?"active":""}">Transcript</button></nav><div class="feed">${events.length?events.map(e=>tab==="activity"?`<article><b>${e.seq}</b><span>${esc(e.type)}</span><pre>${esc(JSON.stringify(e.payload,null,2))}</pre></article>`:`<pre>${esc(JSON.stringify(e,null,2))}</pre>`).join(""):`<div class="empty">Waiting for canonical events.</div>`}</div><details><summary>Diagnostics</summary><pre>${esc(JSON.stringify(run.diagnostics,null,2))}</pre></details>`:`<div class="empty">Select a run to inspect durable activity.</div>`}</section></main>${error?`<div class="error">${esc(error)}</div>`:""}`;
 app.querySelectorAll<HTMLElement>("[data-run]").forEach(x=>x.onclick=()=>{selected=x.dataset.run!;refresh(true)}); app.querySelectorAll<HTMLButtonElement>("[data-a]").forEach(x=>x.onclick=()=>{ if(x.dataset.a==="launch") void launch(); else if(x.dataset.a==="cancel") void cancel(); else void refresh(); }); app.querySelectorAll<HTMLButtonElement>("[data-tab]").forEach(x=>x.onclick=()=>{tab=x.dataset.tab!;render()}); }
setInterval(()=>refresh(true),1500); refresh();
