package render

// defaultTemplates contains embedded HTML templates for all page types.
// Each kind template must define: "main" block.
// The baseof template calls: {{template "main" .}}
// Sidebar is inlined in baseof using shared sidebar logic.
var defaultTemplates = map[string]string{
	"baseof": `<!DOCTYPE html>
<html lang="{{.Page.Lang}}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{if .Page.Title}}{{.Page.Title}} | {{.Site.Title}}{{else}}{{.Site.Title}}{{end}}</title>
  {{if .Page.Description}}<meta name="description" content="{{.Page.Description}}">{{end}}
  {{if .Page.NoIndex}}<meta name="robots" content="noindex">{{end}}
  <link rel="canonical" href="{{trimSlash .Site.BaseURL}}{{.Page.RelPermalink}}">
  {{if .Assets.CSS}}<link rel="stylesheet" href="{{.Assets.CSS}}">{{end}}
  {{if .Assets.KaTeX}}<link rel="stylesheet" href="{{.Assets.KaTeX}}">{{end}}
  <style>
    :root {
      --bg: #ffffff;
      --fg: #1a1a1a;
      --muted: #6b7280;
      --accent: #2563eb;
      --border: #e5e7eb;
      --sidebar-width: 260px;
      --content-max: 860px;
      --code-bg: #f3f4f6;
      --nav-h: 56px;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #0f1117;
        --fg: #e2e8f0;
        --muted: #94a3b8;
        --accent: #60a5fa;
        --border: #1e2533;
        --code-bg: #1e2533;
      }
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: var(--bg); color: var(--fg); line-height: 1.65; }
    a { color: var(--accent); text-decoration: none; }
    a:hover { text-decoration: underline; }
    nav.topnav { position: sticky; top: 0; z-index: 100; height: var(--nav-h); background: var(--bg); border-bottom: 1px solid var(--border); display: flex; align-items: center; padding: 0 1.5rem; gap: 1rem; }
    nav.topnav .brand { font-weight: 700; font-size: 1.1rem; color: var(--fg); }
    nav.topnav .spacer { flex: 1; }
    nav.topnav a { color: var(--muted); font-size: 0.9rem; }
    nav.topnav a:hover { color: var(--accent); text-decoration: none; }
    .layout { display: flex; min-height: calc(100vh - var(--nav-h)); }
    .sidebar { width: var(--sidebar-width); flex-shrink: 0; border-right: 1px solid var(--border); padding: 1.5rem 1rem; font-size: 0.875rem; overflow-y: auto; position: sticky; top: var(--nav-h); height: calc(100vh - var(--nav-h)); }
    .sidebar h3 { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); margin-bottom: 0.5rem; margin-top: 1rem; }
    .sidebar h3:first-child { margin-top: 0; }
    .sidebar ul { list-style: none; }
    .sidebar li { margin: 0.2rem 0; }
    .sidebar a { color: var(--fg); display: block; padding: 0.2rem 0.5rem; border-radius: 4px; }
    .sidebar a:hover, .sidebar a.active { background: var(--code-bg); color: var(--accent); text-decoration: none; }
    .main { flex: 1; min-width: 0; padding: 2rem; }
    .content-wrap { max-width: var(--content-max); margin: 0 auto; }
    .page-header { margin-bottom: 2rem; padding-bottom: 1rem; border-bottom: 1px solid var(--border); }
    .page-header h1 { font-size: 2rem; font-weight: 700; line-height: 1.25; }
    .page-meta { margin-top: 0.5rem; font-size: 0.875rem; color: var(--muted); display: flex; gap: 1rem; flex-wrap: wrap; }
    .tags { display: flex; gap: 0.4rem; flex-wrap: wrap; margin-top: 0.75rem; }
    .tag { background: var(--code-bg); color: var(--muted); padding: 0.2rem 0.6rem; border-radius: 12px; font-size: 0.8rem; }
    .tag:hover { color: var(--accent); text-decoration: none; }
    .prose { max-width: none; }
    .prose h1, .prose h2, .prose h3, .prose h4 { font-weight: 700; line-height: 1.3; margin: 1.5em 0 0.5em; }
    .prose h1 { font-size: 1.75rem; }
    .prose h2 { font-size: 1.4rem; border-bottom: 1px solid var(--border); padding-bottom: 0.25rem; }
    .prose h3 { font-size: 1.15rem; }
    .prose p { margin: 1em 0; }
    .prose ul, .prose ol { margin: 1em 0; padding-left: 1.75em; }
    .prose li { margin: 0.25em 0; }
    .prose blockquote { border-left: 3px solid var(--accent); padding-left: 1rem; color: var(--muted); margin: 1em 0; }
    .prose code { background: var(--code-bg); padding: 0.15em 0.35em; border-radius: 3px; font-size: 0.875em; font-family: "JetBrains Mono", "Fira Code", monospace; }
    .prose pre { background: var(--code-bg); padding: 1rem; border-radius: 6px; overflow-x: auto; margin: 1em 0; }
    .prose pre code { background: none; padding: 0; font-size: 0.875rem; }
    .prose table { border-collapse: collapse; width: 100%; margin: 1em 0; }
    .prose th, .prose td { border: 1px solid var(--border); padding: 0.5rem 0.75rem; text-align: left; }
    .prose th { background: var(--code-bg); font-weight: 600; }
    .prose img { max-width: 100%; border-radius: 6px; }
    .prose hr { border: none; border-top: 1px solid var(--border); margin: 2rem 0; }
    .prose a { color: var(--accent); }
    .list-page .page-list { list-style: none; margin-top: 1rem; }
    .list-page .page-list li { padding: 1rem 0; border-bottom: 1px solid var(--border); }
    .list-page .page-list li:last-child { border-bottom: none; }
    .list-page .page-list h2 { font-size: 1.1rem; font-weight: 600; margin: 0 0 0.25rem; }
    .list-page .page-list .meta { font-size: 0.8rem; color: var(--muted); }
    .list-page .page-list .summary { font-size: 0.9rem; color: var(--muted); margin-top: 0.25rem; }
    .breadcrumb { font-size: 0.8rem; color: var(--muted); margin-bottom: 1rem; }
    .breadcrumb a { color: var(--muted); }
    .breadcrumb a:hover { color: var(--accent); }
    footer { border-top: 1px solid var(--border); text-align: center; padding: 1.5rem; font-size: 0.8rem; color: var(--muted); }
    @media (max-width: 768px) {
      .sidebar { display: none; }
      .main { padding: 1rem; }
      .page-header h1 { font-size: 1.5rem; }
    }
  </style>
</head>
<body>
<nav class="topnav">
  <a class="brand" href="/">{{.Site.Title}}</a>
  <span class="spacer"></span>
  <a href="/search/">Search</a>
</nav>
<div class="layout">
  <aside class="sidebar">
    {{template "page-sidebar" .}}
  </aside>
  <main class="main">
    <div class="content-wrap">
      {{template "page-main" .}}
    </div>
  </main>
</div>
<footer>
  <p>{{.Site.Title}} &mdash; Built with <a href="https://github.com/tamnd/tago">tago</a></p>
</footer>
{{if .Assets.JSHead}}<script src="{{.Assets.JSHead}}"></script>{{end}}
{{if .Assets.JS}}<script src="{{.Assets.JS}}"></script>{{end}}
{{if .Assets.KaTeX}}<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/katex.min.js"></script>
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/contrib/auto-render.min.js"
  onload="renderMathInElement(document.body,{delimiters:[{left:'$$',right:'$$',display:true},{left:'$',right:'$',display:false}]})"></script>{{end}}
{{template "page-live-reload" .}}
</body>
</html>`,

	"page": `{{define "page-main"}}
<article>
  {{template "breadcrumb" .}}
  <header class="page-header">
    <h1>{{.Page.Title}}</h1>
    <div class="page-meta">
      {{if not .Page.Date.IsZero}}<span>{{formatDate .Page.Date}}</span>{{end}}
      {{if gt .Page.ReadingTime 0}}<span>{{.Page.ReadingTime}} min read</span>{{end}}
      {{if gt .Page.WordCount 0}}<span>{{.Page.WordCount}} words</span>{{end}}
    </div>
    {{if .Page.Tags}}
    <div class="tags">
      {{range .Page.Tags}}<a class="tag" href="/tags/{{urlize .}}/">{{.}}</a>{{end}}
    </div>
    {{end}}
  </header>
  <div class="prose">{{rawHTML .Page.ContentHTML}}</div>
  {{if .Site.EditURLBase}}
  <hr style="margin-top:3rem">
  <p style="font-size:0.8rem;color:var(--muted)"><a href="{{.Site.EditURLBase}}{{.Page.FilePath}}">Edit this page</a></p>
  {{end}}
</article>
{{end}}
{{define "page-sidebar"}}
  {{if .Page.Ancestors}}
  <ul>
    <li><a href="/">Home</a></li>
    {{range .Page.Ancestors}}
    <li>&#8250; <a href="{{.RelPermalink}}">{{.LinkTitle}}</a></li>
    {{end}}
  </ul>
  <hr style="border:none;border-top:1px solid var(--border);margin:1rem 0">
  {{end}}
  {{if .Page.Parent}}
    <h3>{{.Page.Parent.LinkTitle}}</h3>
    <ul>
    {{range .Page.Parent.Children}}
      <li><a href="{{.RelPermalink}}"{{if eq .RelPermalink $.Page.RelPermalink}} class="active"{{end}}>{{.LinkTitle}}</a></li>
    {{end}}
    </ul>
  {{else if .Page.Children}}
    <h3>Contents</h3>
    <ul>
    {{range .Page.Children}}
      <li><a href="{{.RelPermalink}}">{{.LinkTitle}}</a></li>
    {{end}}
    </ul>
  {{end}}
{{end}}
{{define "page-live-reload"}}{{end}}`,

	"section": `{{define "page-main"}}
<div class="list-page">
  {{template "breadcrumb" .}}
  <header class="page-header">
    <h1>{{.Page.Title}}</h1>
    {{if .Page.Description}}<p style="color:var(--muted);margin-top:0.5rem">{{.Page.Description}}</p>{{end}}
  </header>
  {{if .Page.ContentHTML}}<div class="prose" style="margin-bottom:2rem">{{rawHTML .Page.ContentHTML}}</div>{{end}}
  <ul class="page-list">
    {{range .Pages}}
    <li>
      <h2><a href="{{.RelPermalink}}">{{.Title}}</a></h2>
      <div class="meta">
        {{if not .Date.IsZero}}{{formatDate .Date}}{{end}}
        {{if .Tags}}&nbsp;&middot;{{range .Tags}} <a class="tag" href="/tags/{{urlize .}}/">{{.}}</a>{{end}}{{end}}
      </div>
      {{if .Summary}}<p class="summary">{{.Summary}}</p>{{end}}
    </li>
    {{else}}
    <li><p style="color:var(--muted)">No pages yet.</p></li>
    {{end}}
  </ul>
</div>
{{end}}
{{define "page-sidebar"}}
  {{if .Page.Ancestors}}
  <ul>
    <li><a href="/">Home</a></li>
    {{range .Page.Ancestors}}
    <li>&#8250; <a href="{{.RelPermalink}}">{{.LinkTitle}}</a></li>
    {{end}}
  </ul>
  <hr style="border:none;border-top:1px solid var(--border);margin:1rem 0">
  {{end}}
  {{if .Page.Children}}
    <h3>{{.Page.LinkTitle}}</h3>
    <ul>
    {{range .Page.Children}}
      <li><a href="{{.RelPermalink}}">{{.LinkTitle}}</a></li>
    {{end}}
    </ul>
  {{end}}
{{end}}
{{define "page-live-reload"}}{{end}}`,

	"home": `{{define "page-main"}}
<div class="list-page">
  <header class="page-header">
    <h1>{{.Site.Title}}</h1>
    {{if .Site.Description}}<p style="color:var(--muted);margin-top:0.5rem">{{.Site.Description}}</p>{{end}}
  </header>
  {{if .Page.ContentHTML}}<div class="prose" style="margin-bottom:2rem">{{rawHTML .Page.ContentHTML}}</div>{{end}}
  <ul class="page-list">
    {{range .Pages}}
    <li>
      <h2><a href="{{.RelPermalink}}">{{.Title}}</a></h2>
      {{if .Description}}<p class="summary">{{.Description}}</p>{{end}}
    </li>
    {{else}}
    <li><p style="color:var(--muted)">No sections yet.</p></li>
    {{end}}
  </ul>
</div>
{{end}}
{{define "page-sidebar"}}
<h3>Sections</h3>
<ul>
{{range .Pages}}<li><a href="{{.RelPermalink}}">{{.LinkTitle}}</a></li>{{end}}
</ul>
{{end}}
{{define "page-live-reload"}}{{end}}`,

	"taxonomy": `{{define "page-main"}}
<div class="list-page">
  <header class="page-header">
    <h1>Tags</h1>
  </header>
  <div class="tags" style="margin-top:1rem">
    {{range .Tags}}<a class="tag" href="/tags/{{urlize .Name}}/" style="font-size:{{tagFontSize .Count}}rem">{{.Name}} <span style="color:var(--muted)">({{.Count}})</span></a>{{end}}
  </div>
</div>
{{end}}
{{define "page-sidebar"}}{{end}}
{{define "page-live-reload"}}{{end}}`,

	"term": `{{define "page-main"}}
<div class="list-page">
  <header class="page-header">
    <h1>Tag: {{.Page.Title}}</h1>
  </header>
  <ul class="page-list">
    {{range .Pages}}
    <li>
      <h2><a href="{{.RelPermalink}}">{{.Title}}</a></h2>
      <div class="meta">{{if not .Date.IsZero}}{{formatDate .Date}}{{end}}</div>
      {{if .Summary}}<p class="summary">{{.Summary}}</p>{{end}}
    </li>
    {{end}}
  </ul>
</div>
{{end}}
{{define "page-sidebar"}}{{end}}
{{define "page-live-reload"}}{{end}}`,

	"search": `{{define "page-main"}}
<div>
  <header class="page-header">
    <h1>Search</h1>
  </header>
  <div style="margin-bottom:1rem">
    <input id="search-input" type="search" placeholder="Search..." autocomplete="off"
      style="width:100%;padding:0.6rem 1rem;border:1px solid var(--border);border-radius:6px;font-size:1rem;background:var(--bg);color:var(--fg)">
  </div>
  <ul id="search-results" class="page-list"></ul>
</div>
<script>
(function() {
  var data = null;
  var input = document.getElementById('search-input');
  var results = document.getElementById('search-results');

  function loadData(cb) {
    if (data) return cb(data);
    fetch('/en.search-data.json').then(r => r.json()).then(function(d) {
      data = d;
      cb(data);
    });
  }

  function search(q) {
    q = q.toLowerCase().trim();
    results.innerHTML = '';
    if (!q || !data) return;
    var hits = [];
    for (var url in data) {
      var item = data[url];
      var text = (item.title + ' ' + (item.data[''] || '')).toLowerCase();
      if (text.includes(q)) {
        hits.push({url: url, title: item.title, snippet: item.data[''] || ''});
      }
    }
    hits.slice(0, 20).forEach(function(h) {
      var li = document.createElement('li');
      li.innerHTML = '<h2><a href="' + h.url + '">' + h.title + '</a></h2>' +
        '<p class="summary">' + h.snippet.slice(0, 150) + '...</p>';
      results.appendChild(li);
    });
    if (!hits.length) {
      results.innerHTML = '<li><p style="color:var(--muted)">No results for &ldquo;' + q + '&rdquo;</p></li>';
    }
  }

  input.addEventListener('input', function() { loadData(function() { search(input.value); }); });
  var q = new URLSearchParams(location.search).get('q');
  if (q) { input.value = q; loadData(function() { search(q); }); }
})();
</script>
{{end}}
{{define "page-sidebar"}}{{end}}
{{define "page-live-reload"}}{{end}}`,

	"graph": `{{define "page-main"}}
<script>const graphData = {{.GraphDataJSON | safeJS}};</script>
<div id="graph-container" style="width:100%;height:calc(100vh - 60px);"></div>
<script src="https://d3js.org/d3.v7.min.js"></script>
<script>
(function(){
  const data = graphData;
  const width = document.getElementById('graph-container').clientWidth;
  const height = document.getElementById('graph-container').clientHeight || 600;
  const svg = d3.select('#graph-container').append('svg')
    .attr('width', width).attr('height', height)
    .call(d3.zoom().on('zoom', e => g.attr('transform', e.transform)));
  const g = svg.append('g');
  const color = d3.scaleOrdinal(d3.schemeTableau10);
  const sim = d3.forceSimulation(data.nodes)
    .force('link', d3.forceLink(data.links).id(d=>d.id)
      .distance(d => d.kind==='tree' ? 30 : 80)
      .strength(d => d.kind==='tree' ? 1 : 0.25))
    .force('charge', d3.forceManyBody().strength(d => d.kind==='tag' ? -120 : -55))
    .force('collide', d3.forceCollide(8))
    .force('center', d3.forceCenter(width/2, height/2));
  const link = g.append('g').selectAll('line').data(data.links).join('line')
    .attr('stroke', d => d.kind==='tree' ? '#94a3b8' : '#cbd5e1')
    .attr('stroke-width', d => d.kind==='tree' ? 1.5 : 0.5)
    .attr('stroke-opacity', d => d.kind==='tree' ? 0.7 : 0.3);
  const node = g.append('g').selectAll('circle').data(data.nodes).join('circle')
    .attr('r', d => d.kind==='tag' ? 5 : Math.max(3, 6-d.depth))
    .attr('fill', d => d.kind==='tag' ? '#94a3b8' : color(d.section))
    .attr('cursor', 'pointer')
    .on('click', (e,d) => { if(d.url) window.location.href=d.url; })
    .call(d3.drag().on('start',(e,d)=>{if(!e.active)sim.alphaTarget(0.3).restart();d.fx=d.x;d.fy=d.y})
      .on('drag',(e,d)=>{d.fx=e.x;d.fy=e.y})
      .on('end',(e,d)=>{if(!e.active)sim.alphaTarget(0);d.fx=null;d.fy=null}));
  node.append('title').text(d=>d.title);
  sim.on('tick', () => {
    link.attr('x1',d=>d.source.x).attr('y1',d=>d.source.y).attr('x2',d=>d.target.x).attr('y2',d=>d.target.y);
    node.attr('cx',d=>d.x).attr('cy',d=>d.y);
  });
})();
</script>
{{end}}
{{define "page-sidebar"}}{{end}}
{{define "page-live-reload"}}{{end}}`,

	"tree": `{{define "page-main"}}
<script>const treeData = {{.TreeDataJSON | safeJS}};</script>
<div style="margin-bottom:1rem;display:flex;gap:.5rem">
  <button onclick="expandAll()" style="padding:.3rem .8rem;border:1px solid var(--border);border-radius:4px;cursor:pointer;background:var(--bg)">Expand All</button>
  <button onclick="collapseAll()" style="padding:.3rem .8rem;border:1px solid var(--border);border-radius:4px;cursor:pointer;background:var(--bg)">Collapse All</button>
</div>
<div id="tree-container" style="overflow:auto;width:100%;height:calc(100vh - 120px);"></div>
<script src="https://d3js.org/d3.v7.min.js"></script>
<script>
(function(){
  var raw = treeData;
  var byId = {};
  raw.items.forEach(function(n){ byId[n.id] = Object.assign({}, n, {children:[]}); });
  raw.items.forEach(function(n){ if(n.parent && byId[n.parent]) byId[n.parent].children.push(byId[n.id]); });
  var rootEntry = byId[raw.rootId] || {id:'/',name:raw.rootName,children:Object.keys(byId).filter(function(k){var n=byId[k];return !n.parent||n.parent==='/';}).map(function(k){return byId[k];})};
  var dx=22, dy=220;
  var treeLayout = d3.tree().nodeSize([dx,dy]);
  var rootNode = d3.hierarchy(rootEntry);
  rootNode.descendants().forEach(function(d){if(d.depth>1&&d.children){d._children=d.children;d.children=null;}});
  var svg = d3.select('#tree-container').append('svg').attr('width','100%');
  var g = svg.append('g').attr('transform','translate(40,20)');
  function update(){
    treeLayout(rootNode);
    var nodes=rootNode.descendants(), links=rootNode.links();
    var h = Math.max(600, nodes.length*dx);
    svg.attr('height', h+40);
    g.selectAll('.link').data(links,function(d){return d.target.data.id;}).join('path').attr('class','link')
      .attr('fill','none').attr('stroke','#94a3b8').attr('stroke-width',1)
      .attr('d',d3.linkHorizontal().x(function(d){return d.y;}).y(function(d){return d.x;}));
    var node=g.selectAll('.node').data(nodes,function(d){return d.data.id;}).join('g').attr('class','node')
      .attr('transform',function(d){return 'translate('+d.y+','+d.x+')';});
    node.append('circle').attr('r',4).attr('fill',function(d){return d._children?'#60a5fa':'#e2e8f0';}).attr('stroke','#94a3b8')
      .style('cursor','pointer').on('click',function(e,d){if(d._children){d.children=d._children;d._children=null;}else{d._children=d.children;d.children=null;}update();});
    node.append('text').attr('dy','0.31em').attr('x',function(d){return d.children||d._children?-8:8;})
      .attr('text-anchor',function(d){return d.children||d._children?'end':'start';})
      .style('font-size','12px').style('fill','var(--fg)').style('cursor','pointer')
      .text(function(d){return d.data.name;}).on('click',function(e,d){if(d.data.url)window.location.href=d.data.url;});
  }
  window.expandAll=function(){rootNode.descendants().forEach(function(d){if(d._children){d.children=d._children;d._children=null;}});update();};
  window.collapseAll=function(){rootNode.descendants().forEach(function(d){if(d.depth>0&&d.children){d._children=d.children;d.children=null;}});update();};
  update();
})();
</script>
{{end}}
{{define "page-sidebar"}}{{end}}
{{define "page-live-reload"}}{{end}}`,

	"calendar": `{{define "page-main"}}
<script>const calendarData = {{.CalendarDataJSON | safeJS}};</script>
<div style="display:flex;gap:2rem;flex-wrap:wrap">
  <div id="cal-grid" style="min-width:280px"></div>
  <div id="cal-list" style="flex:1;min-width:280px"></div>
</div>
<script>
(function(){
  var data = calendarData;
  var days = Object.keys(data).sort().reverse();
  var today = days[0] || new Date().toISOString().slice(0,10);
  var curMonth = today.slice(0,7);
  function pad2(n){ return n < 10 ? '0'+n : ''+n; }
  function renderMonth(ym){
    var parts = ym.split('-');
    var y = parseInt(parts[0],10), m = parseInt(parts[1],10);
    var first = new Date(y,m-1,1).getDay();
    var daysInMonth = new Date(y,m,0).getDate();
    var html = '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:.5rem">'
      + '<button onclick="changeMonth(-1)" style="background:none;border:none;cursor:pointer;font-size:1.2rem">&#8249;</button>'
      + '<strong>' + ym + '</strong>'
      + '<button onclick="changeMonth(1)" style="background:none;border:none;cursor:pointer;font-size:1.2rem">&#8250;</button>'
      + '</div><div style="display:grid;grid-template-columns:repeat(7,36px);gap:2px;text-align:center">';
    ['Su','Mo','Tu','We','Th','Fr','Sa'].forEach(function(d){html+='<div style="font-size:.7rem;color:var(--muted)">'+d+'</div>';});
    for(var i=0;i<first;i++) html+='<div></div>';
    for(var d=1;d<=daysInMonth;d++){
      var key=ym+'-'+pad2(d);
      var has=data[key];
      html+='<div onclick="showDay(\''+key+'\')" style="padding:4px;cursor:'+(has?'pointer':'default')+';background:'+(has?'var(--accent)':'none')+';color:'+(has?'#fff':'var(--fg)')+';border-radius:4px;font-size:.85rem">'+d+'</div>';
    }
    html+='</div>';
    document.getElementById('cal-grid').innerHTML=html;
  }
  function showDay(key){
    var entries=data[key]||[];
    if(!entries.length){document.getElementById('cal-list').innerHTML='<p style="color:var(--muted)">No entries</p>';return;}
    var html='<h3 style="margin-bottom:1rem">'+key+'</h3>';
    entries.forEach(function(e){html+='<div style="margin-bottom:1rem;padding:.75rem;border:1px solid var(--border);border-radius:6px">'
      +'<a href="'+e.url+'" style="font-weight:600">'+e.title+'</a>'
      +'<div style="font-size:.8rem;color:var(--muted);margin-top:.25rem">'+(e.path||'')+' &middot; '+e.rt+'</div>'
      +'</div>';});
    document.getElementById('cal-list').innerHTML=html;
  }
  window.changeMonth=function(delta){
    var parts=curMonth.split('-');
    var y=parseInt(parts[0],10), m=parseInt(parts[1],10);
    var nd=new Date(y,m-1+delta,1);
    curMonth=nd.getFullYear()+'-'+pad2(nd.getMonth()+1);
    renderMonth(curMonth);
  };
  window.showDay=showDay;
  renderMonth(curMonth);
  if(days[0]) showDay(days[0]);
})();
</script>
{{end}}
{{define "page-sidebar"}}{{end}}
{{define "page-live-reload"}}{{end}}`,

	"404": `{{define "page-main"}}
<div style="text-align:center;padding:4rem 2rem">
  <h1 style="font-size:6rem;font-weight:900;color:var(--muted)">404</h1>
  <p style="font-size:1.25rem;margin:1rem 0">Page not found</p>
  <a href="/" style="color:var(--accent)">&#8592; Back to home</a>
</div>
{{end}}
{{define "page-sidebar"}}{{end}}
{{define "page-live-reload"}}{{end}}`,

	"breadcrumb": `{{define "breadcrumb"}}
{{if .Page.Ancestors}}
<nav class="breadcrumb" aria-label="breadcrumb">
  <a href="/">Home</a>
  {{range .Page.Ancestors}}
  &rsaquo; <a href="{{.RelPermalink}}">{{.LinkTitle}}</a>
  {{end}}
</nav>
{{end}}
{{end}}`,

	"live-reload-js": `{{define "page-live-reload"}}
<script>
(function() {
  var ws = new WebSocket('ws://' + location.host + '/__tago_ws');
  ws.onmessage = function(e) { if (e.data === 'reload') location.reload(); };
  ws.onclose = function() { setTimeout(function() { location.reload(); }, 1000); };
})();
</script>
{{end}}`,
}
