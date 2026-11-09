#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
if (!process.argv[2]) throw new Error('Usage: node render_charts.mjs /absolute/path/to/pele/dist/pele.js (run at repo root)');
const {render} = await import(pathToFileURL(path.resolve(process.argv[2])).href);
const root=process.cwd();
const folders=fs.readdirSync(path.join(root,'docs/skills')).filter(n=>n.startsWith('verify-'));
const charts=[];
for(const folder of folders){
 const base=path.join(root,'docs/skills',folder,'evidence');
 if(fs.existsSync(path.join(base,'chart.mmd')))charts.push(base);
 if(folder==='verify-execution-lifecycle')for(const n of ['ExecutionLifecycle','AuthenticatedMetadata'])charts.push(path.join(base,n));
}
const entries=charts.map(base=>{
 const source=fs.readFileSync(path.join(base,'chart.mmd'),'utf8');
 const {svg}=render(source,{limit:1_000_000,outputLimit:8_000_000,maxEdges:10_000});
 const styled=svg.replace(/(<svg[^>]*>)/,'$1<style>.pele{--pele-bg:var(--background,#fff);--pele-fg:var(--foreground,#222);--pele-muted:var(--muted-foreground,#666);--pele-line:var(--foreground,#808080);--pele-font:system-ui,sans-serif}.pele .pele-node text{fill:#1c1c1c}.pele .blue rect{fill:#c3e0ff}.pele .teal rect{fill:#bfe3df}.pele .orange rect{fill:#f0d3bf}.pele .pink rect{fill:#ffcbe6}</style>');
 fs.writeFileSync(path.join(base,'chart.svg'),styled);
 return {name:source.match(/title: "([^"]+)"/)?.[1]??path.basename(base),svg:styled,source,report:fs.readFileSync(path.join(base,'report.md'),'utf8')};
});
const esc=s=>s.replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;');
const items=entries.sort((a,b)=> (a.name.includes('bundle')?-2:a.name.includes('child execution')?-1:0)-(b.name.includes('bundle')?-2:b.name.includes('child execution')?-1:0));
const html=`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{font-family:var(--font-sans,system-ui);color:var(--foreground,#222)}select{font:inherit;padding:10px;max-width:100%;color:var(--foreground,#222);background:var(--card,#fafafa);border:1px solid var(--border,#bbb);border-radius:var(--radius,8px)}article[hidden]{display:none}.diagram{overflow:auto;margin-top:16px}svg{width:100%;height:auto;max-height:1150px}pre{white-space:pre-wrap;font:12px var(--font-mono,monospace)}p{line-height:1.5;color:var(--muted-foreground,#666)}details{margin:16px 0}</style></head><body><select aria-label="Subsystem" onchange="document.querySelectorAll('article').forEach((e,i)=>e.hidden=i!=this.value)">${items.map((c,i)=>`<option value="${i}">${esc(c.name)}</option>`).join('')}</select><p>Source-backed finite safety models. All fixed configurations pass. Deliberate model faults are negative controls, not current RCC defects. Terminal stutters and label-preserving steps are hidden; each report states the projection count.</p>${items.map((c,i)=>`<article ${i?'hidden':''}><div class="diagram">${c.svg}</div><details><summary>TLC result and bounds</summary><pre>${esc(c.report)}</pre></details><details><summary>Mermaid source</summary><pre><code class="language-mermaid">${esc(c.source)}</code></pre></details></article>`).join('')}<p>Model bounds and source bindings are in each skill's feature map. These charts do not prove Go locking, filesystem durability, cryptography, eventual progress, or cross-platform behavior.</p></body></html>`;
fs.writeFileSync(path.join(root,'docs/skills/verify-execution-lifecycle/evidence/statecharts.html'),html);
console.log(`Rendered ${entries.length} SVGs and gallery`);
