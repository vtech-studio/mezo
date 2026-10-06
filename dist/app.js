'use strict';

const icons = {
  more:'<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
  bell:'<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9Z"/><path d="M10 21h4M12 2v-1"/>',
  book:'<path d="M12 6c-3-3-8-3-10-2v15c3-1 7-1 10 2 3-3 7-3 10-2V4c-2-1-7-1-10 2Zm0 0v15"/>',
  receipt:'<path d="M5 3v18l3-2 4 2 4-2 3 2V3l-3 2-4-2-4 2-3-2Z"/><path d="M9 9h6M9 13h6"/>',
  table:'<path d="M3 8h18v5H3zM6 13v7m12-7v7M4 4h16"/>',
  arrow:'<path d="M4 12h15m-6-6 6 6-6 6"/>',
  arrowUp:'<path d="M6 18 18 6M6 6h12v12"/>',
  check:'<path d="m5 12 4 4L19 6"/>',
  circleCheck:'<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>',
  heart:'<path d="M12 20 4 12a5 5 0 0 1 8-6 5 5 0 0 1 8 6l-8 8Z"/>',
  close:'<path d="m6 6 12 12M6 18 18 6"/>',
  clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  water:'<path d="M12 2C9 6 5 10 5 15a7 7 0 0 0 14 0c0-5-4-9-7-13Z"/><path d="M8 15c0 2 1 3 3 3"/>',
  utensils:'<path d="M5 3v6c0 2 4 2 4 0V3M7 3v18M19 3c-5 3-5 10 0 10V3Zm0 10v8"/>',
  help:'<circle cx="12" cy="12" r="9"/><path d="M9 9a3 3 0 1 1 4 3c-1 .5-1 1-1 2M12 17h.01"/>',
  leaf:'<path d="M20 3C5 2 1 9 6 16s16 0 14-13ZM5 20 16 9"/>',
  search:'<circle cx="10" cy="10" r="6"/><path d="m15 15 5 5"/>',
  lock:'<rect x="5" y="10" width="14" height="11" rx="3"/><path d="M8 10V6a4 4 0 0 1 8 0v4M12 14v3"/>',
  card:'<rect x="2" y="4" width="20" height="16" rx="3"/><path d="M2 9h20M6 15h4"/>',
  pix:'<path d="m12 2 4 4-4 4-4-4 4-4ZM2 12l4-4 4 4-4 4-4-4Zm12 0 4-4 4 4-4 4-4-4Zm-2 2 4 4-4 4-4-4 4-4Z"/>',
  info:'<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
  history:'<path d="M3 11a9 9 0 1 1 2 7M3 4v7h7M12 7v5l3 2"/>',
  reset:'<path d="M3 11a9 9 0 1 1 2 7M3 4v7h7"/>',
  people:'<circle cx="9" cy="7" r="3"/><path d="M3 21v-3a6 6 0 0 1 12 0v3M16 4a3 3 0 0 1 0 6m2 4a5 5 0 0 1 3 4v3"/>',
};
const icon = (name, cls='') => `<svg class="icon ${cls}" viewBox="0 0 24 24" aria-hidden="true">${icons[name] || icons.info}</svg>`;
const money = value => new Intl.NumberFormat('pt-BR', {style:'currency',currency:'BRL'}).format(value / 100);
const escapeHTML = text => String(text).replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
const timeLabel = date => {
  const minutes = Math.max(0, Math.floor((Date.now() - date) / 60000));
  return minutes === 0 ? 'agora' : `há ${minutes} min`;
};
const tableName = n => String(n).padStart(2,'0');
const reasons = [
  {id:'help',label:'Preciso de ajuda',icon:'help'},
  {id:'order',label:'Fazer um pedido',icon:'utensils'},
  {id:'water',label:'Pedir água',icon:'water'},
  {id:'bill',label:'Pedir a conta',icon:'receipt'},
];
let menu = [
  {id:'burrata',name:'Burrata da casa',description:'Tomates assados, pesto de manjericão e pão de fermentação natural.',price:3800,category:'Entradas',art:'burrata',tag:'Favorito da casa',tags:['Vegetariano','Contém leite e glúten']},
  {id:'croqueta',name:'Croquetas de costela',description:'Crocantes por fora, cremosas por dentro. Com maionese de ervas.',price:2800,category:'Entradas',art:'croqueta',tags:['4 unidades','Contém glúten e ovos']},
  {id:'risoto',name:'Risoto de cogumelos',description:'Arroz arbóreo, mix de cogumelos, parmesão e um toque de vinho branco.',price:5800,category:'Principais',art:'risoto',tag:'Feito com carinho',tags:['Vegetariano','Contém leite']},
  {id:'peixe',name:'Peixe do dia',description:'Grelhado com manteiga de limão, purê de batata e legumes da estação.',price:6800,category:'Principais',art:'peixe',tags:['Consulte a espécie','Contém peixe e leite']},
  {id:'bife',name:'Bife & batatas',description:'Filé grelhado, batatas rústicas e molho de mostarda da casa.',price:7400,category:'Principais',art:'bife',tags:['Contém mostarda','Escolha o ponto com o garçom']},
  {id:'limonada',name:'Limonada da casa',description:'Limão fresquinho, hortelã e um pouquinho de doçura.',price:1200,category:'Bebidas',art:'drink',tags:['350 ml','Sem álcool']},
  {id:'agua',name:'Água mineral',description:'Com ou sem gás. Servida bem gelada.',price:700,category:'Bebidas',art:'water',tags:['500 ml','Sem álcool']},
  {id:'brownie',name:'Brownie quentinho',description:'Chocolate intenso, sorvete de baunilha e calda feita aqui.',price:2400,category:'Sobremesas',art:'brownie',tags:['Contém leite, ovos e glúten']},
];
let receiptItems = [{id:'burrata',qty:1},{id:'risoto',qty:1},{id:'agua',qty:2},{id:'limonada',qty:1}];
const subtotal = () => serverMode ? liveTotals.subtotal : receiptItems.reduce((total,item) => total + (item.price ?? menu.find(d => d.id===item.id)?.price ?? 0) * item.qty,0);
const serviceValue = () => serverMode ? liveTotals.service : state.service ? Math.round(subtotal() * .1) : 0;
const totalValue = () => serverMode ? liveTotals.total : subtotal() + serviceValue();
const storageKey = 'mezo-demo-v1';
const seedState = () => ({
  service:true,
  calls:[
    {id:'seed-3',table:3,reason:'water',note:'Uma água com gás, por favor.',status:'pending',created:Date.now()-240000},
    {id:'seed-12',table:12,reason:'order',note:'Queremos pedir os pratos principais.',status:'pending',created:Date.now()-120000},
    {id:'seed-6',table:6,reason:'bill',note:'',status:'progress',created:Date.now()-360000,accepted:Date.now()-60000},
    {id:'seed-2',table:2,reason:'help',note:'',status:'done',created:Date.now()-900000,completed:Date.now()-720000},
  ],
});
function loadState(){
  try{
    const saved=JSON.parse(localStorage.getItem(storageKey));
    if(saved && typeof saved.service==='boolean' && Array.isArray(saved.calls) && saved.calls.every(c => typeof c.id==='string' && Number.isInteger(c.table) && reasons.some(r=>r.id===c.reason) && ['pending','progress','done','cancelled'].includes(c.status) && typeof c.created==='number' && typeof c.note==='string')) return saved;
  }catch{}
  return seedState();
}
const serverMode=!!document.querySelector('meta[name="mezo-runtime"]');
document.querySelector('.preview-note').hidden=serverMode;
const tenantSlug=serverMode ? location.pathname.split('/')[2] : '';
let tableNumber=8, tableSeats=2, staffName='Ana', staffID='', restaurantName='Casa do Sol';
let tableData=[], csrf='', liveTotals={subtotal:0,service:0,total:0}, eventStream=null, liveReady=false;
let liveTransport='events', livePollTimer=null;
let state=serverMode ? {service:true,calls:[]} : loadState();
let role='customer';
let route='mesa';
let category='Todos';
let query='';
let filter='all';
let selectedReason='help';
let selectedMethod=null;
let toastTimer;
const main=document.querySelector('#main');
const sheet=document.querySelector('#sheet');
function saveState(){if(serverMode)return;try{localStorage.setItem(storageKey,JSON.stringify(state));}catch{}}
saveState();

function dishArt(type){
  const plate='<ellipse cx="60" cy="72" rx="46" ry="10" fill="#d9d2bf" opacity=".4"/><ellipse cx="60" cy="56" rx="45" ry="32" fill="#fffdf5" stroke="#d1c8b6"/><ellipse cx="60" cy="56" rx="34" ry="23" fill="#f5f0e0" stroke="#e3dbc8"/>';
  const herbs='<path d="M38 41q-14-14-17-3 5 12 17 3M76 45q12-17 18-6-2 12-18 6M48 69q-10 15-17 7 0-13 17-7" fill="#70815a"/><path d="m35 38 5 6m40 0 9-5m-50 34 7-2" fill="none" stroke="#516a49" stroke-width="1"/>';
  let food='';
  if(type==='burrata') food='<ellipse cx="61" cy="56" rx="18" ry="13" fill="#fefdf5" stroke="#d7cbb1"/><path d="m54 46 5-8 7 8" stroke="#d7cbb1" fill="#fefdf5"/><ellipse cx="36" cy="51" rx="8" ry="6" fill="#b95639"/><ellipse cx="81" cy="66" rx="8" ry="6" fill="#cc6c44"/><ellipse cx="77" cy="39" rx="7" ry="5" fill="#bf5739"/>'+herbs;
  else if(type==='croqueta') food='<ellipse cx="43" cy="46" rx="12" ry="7" transform="rotate(-25 43 46)" fill="#bd863e"/><ellipse cx="65" cy="41" rx="12" ry="7" transform="rotate(10 65 41)" fill="#c99146"/><ellipse cx="48" cy="63" rx="12" ry="7" transform="rotate(15 48 63)" fill="#c99146"/><ellipse cx="74" cy="59" rx="12" ry="7" transform="rotate(-20 74 59)" fill="#ad7937"/><path d="m39 45 6-3m15 0 8 1m-24 20 6 1m21-5 6-2" stroke="#e6b964" stroke-width="2"/>';
  else if(type==='risoto') food='<ellipse cx="60" cy="55" rx="29" ry="19" fill="#dbc79b"/><path d="M35 47q5-15 18-2l-8 6Zm27 11q5-15 18-2l-8 6Zm-3-18q6-12 16-2l-8 5Z" fill="#987050"/><path d="m43 47 4 8m23 1 4 8m-7-22 2 6" stroke="#bc9772" stroke-width="3"/><path d="m48 63 5-2m5-14 4-2m-22 8 4 1m22 14 4-2" stroke="#f5ecce" stroke-width="2"/>'+herbs;
  else if(type==='peixe') food='<ellipse cx="50" cy="59" rx="18" ry="12" fill="#e4d6b5"/><path d="m50 38 33 13-16 17-30-14Z" fill="#d6a56b" stroke="#bb8c58"/><path d="m49 46 23 9m-28-4 23 9" stroke="#a97540" stroke-width="2"/><path d="M78 36a10 10 0 0 1 10 12H78Z" fill="#e5c968" stroke="#c0a449"/>'+herbs;
  else if(type==='bife') food='<path d="M39 42q19-13 38 0 14 15-10 28-19 6-33-7-10-9 5-21Z" fill="#955a3d"/><path d="m41 45 29 14m-34-7 26 13m-11-25 27 13" stroke="#6a3e2e" stroke-width="2"/><path d="m80 35 6 3-4 17-6-3Zm8 18 7 2-8 17-7-3Z" fill="#d3ad68"/>'+herbs;
  else if(type==='brownie') food='<path d="m35 49 32-7 18 12-33 9Z" fill="#694331"/><path d="m35 49 17 14v15L35 65Zm17 14 33-9v14l-33 10Z" fill="#543729"/><ellipse cx="64" cy="45" rx="13" ry="11" fill="#f5ebd3" stroke="#d6c9a9"/><path d="M29 67q17-5 27 5t28-3" stroke="#9d6544" stroke-width="3" fill="none"/>';
  else if(type==='drink') return '<svg viewBox="0 0 120 100" aria-hidden="true"><ellipse cx="60" cy="85" rx="28" ry="6" fill="#d9d2bf"/><path d="m37 26 6 55q16 8 34 0l6-55Z" fill="#e5dcb5" stroke="#b9ad8c"/><path d="m41 43 4 35q15 5 29 0l4-35Z" fill="#cfcd91"/><path d="M68 62 82 10" stroke="#bf6c44" stroke-width="3"/><rect x="47" y="44" width="11" height="12" rx="2" fill="#f7f5e1" opacity=".7"/><rect x="61" y="57" width="10" height="11" rx="2" fill="#f7f5e1" opacity=".7"/><circle cx="41" cy="29" r="12" fill="#d6ca73" stroke="#9c9d55"/><circle cx="41" cy="29" r="8" fill="#e9de9f"/><path d="m41 22 0 14m-7-7h14" stroke="#f7f1c8"/><path d="M49 35q9-17 17-8-4 10-17 8" fill="#66845b"/></svg>';
  else if(type==='water') return '<svg viewBox="0 0 120 100" aria-hidden="true"><ellipse cx="60" cy="85" rx="25" ry="6" fill="#d9d2bf"/><path d="M51 24v-7h18v7l8 15v41q-16 8-34 0V39Z" fill="#d9e1d9" stroke="#9daa9d"/><rect x="50" y="12" width="20" height="7" rx="2" fill="#64806b"/><path d="M45 51h30v20H45Z" fill="#fffdf5"/><path d="m55 61 5-7 5 7a5 5 0 0 1-10 0" fill="#839e87"/><path d="M49 37v10m0 29v4" stroke="#f5f7ef" stroke-width="3" stroke-linecap="round"/></svg>';
  return `<svg viewBox="0 0 120 100" aria-hidden="true">${plate}${food}</svg>`;
}
const welcomeArt = `<div class="welcome-art" aria-hidden="true"><svg viewBox="0 0 160 160" fill="none"><path d="M12 134q37-11 85-1t48-3" stroke="#d9cdb7" stroke-width="1.4"/><ellipse cx="87" cy="91" rx="50" ry="32" stroke="currentColor" stroke-width="1.3" transform="rotate(-15 87 91)"/><ellipse cx="87" cy="91" rx="37" ry="23" stroke="currentColor" stroke-width="1" transform="rotate(-15 87 91)"/><path d="M46 148 28 91m-6-20 6 20c5 8 15 4 14-4l-6-20M29 67l6 23M135 125l-6-79c-16 10-16 34 2 34M59 73q6-18 24-13-2 17-24 13Zm-1 4 21-13M98 112q1-18 20-20 1 17-20 20Zm-2 4 18-19" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/><path d="M68 35c-4-9 5-9 1-18m16 14c-4-9 5-9 1-18" stroke="#b9b49b" stroke-width="1.4" stroke-linecap="round"/><circle cx="107" cy="41" r="3" fill="#b9b49b" stroke="none"/></svg></div>`;

function renderNav(){
  const pending=state.calls.filter(c=>c.status==='pending').length;
  const routes=role==='customer' ? [['mesa','table','Minha mesa'],['cardapio','book','Cardápio'],['conta','receipt','Minha conta']] : [['fila','bell','Chamados'],['mesas','table','Mesas'],['historico','history','Histórico']];
  document.querySelector('#bottom-nav').innerHTML=routes.map(([id,i,label])=>`<a href="#${id}" class="nav-item ${route===id?'active':''}" ${route===id?'aria-current="page"':''}>${icon(i)}${id==='fila'&&pending?`<span class="nav-count">${pending}</span>`:''}<span>${label}</span></a>`).join('');
  document.querySelectorAll('[data-role]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.role===role)));
  document.querySelector('.live-label').innerHTML=role==='customer'?'<span></span> Tudo a seu tempo':serverMode&&!liveReady?'Acesso da equipe':`<span></span> Bom atendimento, ${escapeHTML(staffName)}`;
}
function activityCard(call){
  const reason=reasons.find(r=>r.id===call.reason);
  const progress=call.status==='progress';
  return `<article class="activity-card"><div class="activity-top"><span class="activity-icon">${icon(progress?'people':reason.icon)}</span><div><strong>${reason.label}</strong><small>Mesa ${tableName(tableNumber)} · enviado ${timeLabel(call.created)}</small></div><span class="badge ${progress?'':'amber'}">${progress?'A caminho':'Na fila'}</span></div><p class="activity-copy">${progress?`<strong>${escapeHTML(call.assigned || staffName)} assumiu seu chamado.</strong> Já já a equipe chega à sua mesa.`:'Seu chamado chegou à equipe. Pode ficar à vontade.'}</p></article>`;
}
function renderHome(){
  const active=state.calls.filter(c=>c.table===tableNumber&&['pending','progress'].includes(c.status));
  main.innerHTML=`<div class="table-line"><span class="table-id">${icon('table')} Mesa ${tableName(tableNumber)}</span><span class="badge">Aberta · ${tableSeats} pessoas</span></div><section class="welcome"><div><p class="eyebrow">BOA COMIDA. BOA COMPANHIA.</p><h1>Aproveite.<br>A gente cuida<br><em>do resto.</em></h1></div>${welcomeArt}</section><button class="call-action" data-action="call"><span class="call-icon">${icon('bell')}</span><span><strong>Chamar garçom</strong><small>Precisou, é só tocar.</small></span>${icon('arrow')}</button><div class="shortcut-grid"><a href="#cardapio" class="shortcut">${icon('book')}${icon('arrowUp','small-arrow')}<strong>Nosso cardápio</strong><small>Escolha com calma.</small></a><a href="#conta" class="shortcut">${icon('receipt')}${icon('arrowUp','small-arrow')}<strong>Minha conta</strong><small>${money(totalValue())} até agora</small></a></div><section><div class="section-heading"><h2>Seu atendimento</h2>${active.length?`<span class="badge neutral">${active.length} ${active.length===1?'chamado':'chamados'}</span>`:'<span class="eyebrow" style="font-size:8px">ESTAMOS POR AQUI</span>'}</div>${active.length?`<div class="call-list">${active.map(activityCard).join('')}</div>`:`<div class="empty-note">${icon('circleCheck')}<p><strong>Tudo certo por aqui.</strong>Quando precisar, nossa equipe está a um toque.</p></div>`}</section><footer class="hospitality-note">${icon('heart')} Feito para você se sentir em casa.<span class="powered">mezô</span></footer>`;
}
function menuList(){
  const dishes=menu.filter(d=>(category==='Todos'||d.category===category)&&(!query||(d.name+' '+d.description).normalize('NFD').replace(/[\u0300-\u036f]/g,'').toLowerCase().includes(query.normalize('NFD').replace(/[\u0300-\u036f]/g,'').toLowerCase())));
  const list=document.querySelector('#menu-list');
  if(!list)return;
  list.innerHTML=dishes.length?dishes.map(d=>`<button class="menu-item" data-action="dish" data-id="${escapeHTML(d.id)}" aria-label="Ver ${escapeHTML(d.name)}, ${money(d.price)}"><span class="dish-art">${dishArt(d.art)}</span><span class="dish-info">${d.tag?`<span class="dish-tag">${escapeHTML(d.tag)}</span>`:''}<h3>${escapeHTML(d.name)}</h3><p>${escapeHTML(d.description)}</p><strong>${money(d.price)}</strong></span></button>`).join(''):`<div class="empty-state">${icon('search')}<h3>Nenhum prato por aqui</h3><p>Tente outro nome ou uma categoria diferente.</p><button class="secondary-button" data-action="clear-search">Limpar busca e filtros</button></div>`;
}
function renderMenu(){
  main.innerHTML=`<div class="page-heading"><p class="eyebrow">FRESCO. SIMPLES. DA CASA.</p><h1>O que vai ser hoje?</h1><p>Um pouco de carinho em cada prato.</p></div><label class="search-box">${icon('search')}<input id="menu-search" type="search" placeholder="Encontre seu próximo favorito" aria-label="Buscar no cardápio" value="${escapeHTML(query)}"></label><div class="chips" aria-label="Categorias">${['Todos','Entradas','Principais','Bebidas','Sobremesas'].map(c=>`<button class="chip ${category===c?'active':''}" data-category="${c}" aria-pressed="${category===c}">${c}</button>`).join('')}</div><div class="menu-list" id="menu-list"></div><p class="menu-footer">${icon('leaf')} Tem alguma restrição alimentar? Fale com a gente.</p>`;
  menuList();
}
function renderBill(){
  main.innerHTML=`<div class="page-heading"><p class="eyebrow">MESA ${tableName(tableNumber)} · ${tableSeats} PESSOAS</p><h1>Foi bom ter você.</h1><p>Sua conta, com tudo bem explicadinho.</p></div><section class="receipt" aria-label="Consumo da mesa"><div class="receipt-head"><strong>O que veio à mesa</strong><small>${receiptItems.reduce((n,i)=>n+i.qty,0)} itens</small></div><div class="receipt-lines">${receiptItems.map(item=>{const d=item.name?item:menu.find(d=>d.id===item.id);return `<div class="receipt-item"><span class="receipt-qty">${item.qty}×</span><div><strong>${escapeHTML(d.name)}</strong><small>${item.qty>1?`${money(d.price)} cada`:'Preparado pela casa'}</small></div><span class="item-price">${money(d.price*item.qty)}</span></div>`;}).join('')}</div><div class="receipt-totals"><div class="total-line"><span>Subtotal</span><span>${money(subtotal())}</span></div><div class="total-line"><label><input type="checkbox" id="service-toggle" ${state.service?'checked':''}> Serviço de 10% <span style="font-size:9px">(opcional)</span></label><span id="service-value">${money(serviceValue())}</span></div><div class="total-line grand"><span>Total da mesa</span><strong id="bill-total">${money(totalValue())}</strong></div></div></section><div class="section-heading"><h2>Como prefere encerrar?</h2></div><button class="primary-button" data-action="payment">Escolher pagamento ${icon('arrow')}</button><p class="payment-caption">${icon('info')} ${serverMode?'Pagamento online em configuração · solicite à equipe':'Pagamento demonstrativo · nenhuma cobrança real'}</p><button class="secondary-button" data-action="ask-bill">${icon('bell')} Prefiro pagar com o garçom</button><footer class="hospitality-note">${icon('heart')} Sua presença fez nosso dia.<span class="powered">mezô</span></footer>`;
}
function waiterCard(call){
  const r=reasons.find(r=>r.id===call.reason);
  return `<article class="waiter-card ${call.status}" data-call="${escapeHTML(call.id)}"><div class="waiter-card-head"><span class="table-square">${tableName(call.table)}</span><div><h3>Mesa ${tableName(call.table)}</h3><small>${call.table===12?'4':'2'} pessoas · salão principal</small></div><span class="badge ${call.status==='pending'?'amber':''}">${call.status==='pending'?'Aguardando':(!serverMode||call.assignedId===staffID?'Com você':escapeHTML(call.assigned))}</span></div><div class="waiter-message">${r.label}${call.note?`<p class="note">${escapeHTML(call.note)}</p>`:''}</div><p class="call-meta">${icon('clock')} Chamado ${timeLabel(call.created)}${call.status==='progress'?` · assumido por ${escapeHTML(call.assigned || staffName)}`:''}</p><button class="primary-button" data-action="${call.status==='pending'?'accept':'complete'}" data-id="${escapeHTML(call.id)}" ${serverMode&&call.status==='progress'&&call.assignedId!==staffID?'disabled':''}>${icon(call.status==='pending'?'arrow':'check')}${call.status==='pending'?'Assumir atendimento':'Concluir atendimento'}</button></article>`;
}
function renderQueue(){
  const pending=state.calls.filter(c=>c.status==='pending');
  const progress=state.calls.filter(c=>c.status==='progress'&&(!serverMode||c.assignedId===staffID));
  const done=state.calls.filter(c=>c.status==='done'&&(!serverMode||c.assignedId===staffID));
  const calls=state.calls.filter(c=>filter==='all'?['pending','progress'].includes(c.status):c.status===filter&&(!serverMode||filter!=='progress'||c.assignedId===staffID)).sort((a,b)=>a.created-b.created);
  main.innerHTML=`<div class="page-heading with-action"><div><p class="eyebrow">SEU TURNO · SALÃO PRINCIPAL</p><h1>Bom trabalho, ${escapeHTML(staffName)}.</h1><p>Um atendimento de cada vez, com atenção.</p></div><span class="avatar" aria-label="${escapeHTML(staffName)}">${escapeHTML(staffName.slice(0,1))}</span></div><div class="waiter-summary"><div class="stat"><strong>${pending.length}</strong><small>Na fila</small></div><div class="stat"><strong>${progress.length}</strong><small>Com você</small></div><div class="stat"><strong>${done.length}</strong><small>Concluídos</small></div></div><div class="chips" aria-label="Filtrar chamados">${[['all','Todos'],['pending','Aguardando'],['progress','Com você']].map(([id,label])=>`<button class="chip ${filter===id?'active':''}" data-filter="${id}" aria-pressed="${filter===id}">${label}</button>`).join('')}</div><p class="filter-summary">${calls.length} ${calls.length===1?'atendimento':'atendimentos'} · mais antigos primeiro</p><div class="call-list">${calls.length?calls.map(waiterCard).join(''):`<div class="empty-state">${icon('circleCheck')}<h3>Tudo em dia!</h3><p>${filter==='progress'?'Você ainda não assumiu nenhum chamado.':'Nenhum chamado aguardando nesta fila.'}</p></div>`}</div>`;
}
function renderTables(){
  main.innerHTML=`<div class="page-heading"><p class="eyebrow">SALÃO PRINCIPAL</p><h1>Cada mesa importa.</h1><p>Veja quem está esperando por você.</p></div><div class="tables-grid">${(serverMode?tableData.map(t=>t.number):Array.from({length:12},(_,i)=>i+1)).map(n=>{const calls=state.calls.filter(c=>c.table===n&&['pending','progress'].includes(c.status));const occupied=serverMode?!!tableData.find(t=>t.number===n)?.sessionId:[2,3,6,8,10,12].includes(n);return `<button class="table-card ${calls.length?'has-call':''}" data-action="table-details" data-table="${n}"><div class="table-card-head"><strong>${tableName(n)}</strong><span class="badge ${calls.length?'amber':occupied?'':'neutral'}">${calls.length?'Chamado':occupied?'Ocupada':'Livre'}</span></div><p>${calls.length?reasons.find(r=>r.id===calls[0].reason).label:occupied?'Aproveitando a casa':'Pronta para receber'}</p><small>${occupied?`${serverMode?tableData.find(t=>t.number===n)?.seats:n===12?4:2} pessoas`:'—'}${calls.length?` · ${timeLabel(calls[0].created)}`:''}</small></button>`;}).join('')}</div>`;
}
function renderHistory(){
  const done=state.calls.filter(c=>c.status==='done'&&(!serverMode||c.assignedId===staffID)).sort((a,b)=>(b.completed||b.created)-(a.completed||a.created));
  main.innerHTML=`<div class="page-heading"><p class="eyebrow">ATENDIMENTOS DO TURNO</p><h1>Pequenos cuidados.</h1><p>${done.length} ${done.length===1?'atendimento concluído':'atendimentos concluídos'} por você.</p></div><div>${done.length?done.map(c=>`<article class="history-row">${icon('circleCheck')}<div><strong>Mesa ${tableName(c.table)} · ${reasons.find(r=>r.id===c.reason).label}</strong><small>Concluído por ${escapeHTML(c.assigned || staffName)}</small></div><span>${timeLabel(c.completed||c.created)}</span></article>`).join(''):`<div class="empty-state">${icon('history')}<h3>Seu histórico começa aqui</h3><p>Os atendimentos concluídos aparecerão nesta tela.</p></div>`}</div><div class="shift-note">${icon('heart')} Cada chamado atendido faz diferença.</div>`;
}
function render(){
  if(serverMode&&!liveReady)return;
  renderNav();
  const views={mesa:renderHome,cardapio:renderMenu,conta:renderBill,fila:renderQueue,mesas:renderTables,historico:renderHistory};
  (views[route]||renderHome)();
}
async function navigate(next,focus=false){
  const isWaiter=['fila','mesas','historico'].includes(next);
  role=isWaiter?'waiter':'customer';
  route=['mesa','cardapio','conta','fila','mesas','historico'].includes(next)?next:'mesa';
  if(serverMode){
    liveReady=false;
    main.innerHTML='<div class="empty-state"><h3>Preparando seu lugar…</h3><p>Conectando ao restaurante.</p></div>';
    try{await refreshLive();liveReady=true;connectLive();}catch(e){if(eventStream)eventStream.close();clearInterval(livePollTimer);renderAccess(e);return;}
  }
  render();
  window.scrollTo({top:0,behavior:'instant'});
  if(focus)main.focus({preventScroll:true});
}

function toast(message){
  const el=document.querySelector('#toast');
  el.textContent=message;el.classList.add('show');
  clearTimeout(toastTimer);toastTimer=setTimeout(()=>el.classList.remove('show'),3400);
}
function openSheet(title,body){
  sheet.innerHTML=`<div class="sheet-handle" aria-hidden="true"></div><div class="sheet-top"><h2 id="sheet-title">${escapeHTML(title)}</h2><button class="icon-button" data-action="close-sheet" aria-label="Fechar">${icon('close')}</button></div>${body}`;
  if(!sheet.open)sheet.showModal();
}
function closeSheet(){sheet.close();}
function openCall(reason='help',note=''){
  selectedReason=reason;
  openSheet('Como podemos ajudar?',`<p class="sheet-description">Conte o que precisa. Nossa equipe recebe seu chamado com o número da mesa.</p><div class="reason-list" role="group" aria-label="Motivo do chamado">${reasons.map(r=>`<button class="reason-button ${r.id===reason?'selected':''}" data-reason="${r.id}" aria-pressed="${r.id===reason}">${icon(r.icon)}${r.label}</button>`).join('')}</div><label class="field-label" for="call-note"><span>Quer acrescentar algo? <span style="font-size:9px">(opcional)</span></span><span id="note-count">${note.length}/140</span></label><textarea id="call-note" maxlength="140" placeholder="Ex.: uma água sem gás, por favor.">${escapeHTML(note)}</textarea><button class="primary-button" data-action="send-call">${icon('bell')} Enviar chamado</button><p class="sheet-note">Mesa ${tableName(tableNumber)} · seu lugar já está identificado</p>`);
}
async function sendCall(){
  if(serverMode){await sendLiveCall();return;}
  state=loadState();
  const existing=state.calls.find(c=>c.table===tableNumber&&c.reason===selectedReason&&['pending','progress'].includes(c.status));
  if(existing){closeSheet();render();toast('Esse chamado já está com a equipe.');return;}
  const note=sheet.querySelector('#call-note').value.trim().slice(0,140);
  const call={id:globalThis.crypto?.randomUUID?.()||`call-${Date.now()}-${Math.random().toString(36).slice(2)}`,table:8,reason:selectedReason,note,status:'pending',created:Date.now()};
  state.calls.push(call);saveState();render();
  const r=reasons.find(r=>r.id===selectedReason);
  openSheet('Chamado enviado',`<div class="success-body"><div class="success-illustration">${icon('check')}</div><h2>Já avisamos a equipe.</h2><p>Pode continuar aproveitando.<br>Seu pedido de atendimento está na fila.</p></div><div class="success-detail"><div class="total-line"><span>Mesa</span><strong>${tableName(tableNumber)} · ${tableSeats} pessoas</strong></div><div class="total-line"><span>Motivo</span><strong>${r.label}</strong></div></div><button class="primary-button" data-action="close-sheet">Tudo certo ${icon('check')}</button>`);
  if(navigator.vibrate)navigator.vibrate(25);
}
function openDish(id){
  const dish=menu.find(d=>d.id===id);if(!dish)return;
  openSheet(dish.name,`<div class="detail-art">${dishArt(dish.art)}</div><div class="dish-detail-heading"><span class="eyebrow">${escapeHTML(dish.category)}</span><strong>${money(dish.price)}</strong></div><p class="detail-description">${escapeHTML(dish.description)}</p><div>${dish.tags.map(t=>`<span class="ingredient-tag">${escapeHTML(t)}</span>`).join('')}</div><button class="primary-button" data-action="ask-dish" data-id="${escapeHTML(dish.id)}">${icon('bell')} Pedir com o garçom</button><p class="sheet-note">O garçom confirma seu pedido à mesa.</p>`);
}
function openPayment(){
  if(serverMode){openSheet('Como encerrar sua conta?',`<p class="sheet-description">Pix e cartão online serão disponibilizados após a configuração do provedor de pagamentos.</p><button class="primary-button" data-action="ask-bill">${icon('bell')} Solicitar pagamento com a equipe</button><p class="sheet-note">Total atual: ${money(totalValue())}</p>`);return;}
  selectedMethod=null;
  openSheet('Escolha como pagar',`<p class="sheet-description">Total da mesa ${tableName(tableNumber)}: <strong style="color:var(--ink)">${money(totalValue())}</strong></p><div class="method-list"><button class="method-button" data-method="pix">${icon('pix')}<span><strong>Pix</strong><small>Rápido, direto do seu banco.</small></span>${icon('arrow')}</button><button class="method-button" data-method="card">${icon('card')}<span><strong>Cartão de crédito</strong><small>Escolha o cartão na próxima etapa.</small></span>${icon('arrow')}</button></div><div class="demo-info">${icon('info')}<span>Esta é uma demonstração da interface. Nenhuma cobrança será feita e nenhum dado bancário será solicitado.</span></div><button class="primary-button" data-action="simulate-payment" disabled>Selecione uma forma de pagamento</button>`);
}
function simulatePayment(){
  if(!selectedMethod)return;
  openSheet('Prévia do pagamento',`<div class="success-body"><div class="success-illustration">${icon(selectedMethod==='pix'?'pix':'card')}</div><h2>Tudo pronto por aqui.</h2><p>Esta seria a etapa de pagamento por ${selectedMethod==='pix'?'Pix':'cartão'}. A integração será feita depois.</p></div><div class="success-detail"><div class="total-line"><span>Total da mesa</span><strong>${money(totalValue())}</strong></div><div class="total-line"><span>Forma escolhida</span><strong>${selectedMethod==='pix'?'Pix':'Cartão de crédito'}</strong></div></div><div class="demo-info">${icon('info')}<span>Sua conta permanece aberta. Nenhum pagamento foi realizado.</span></div><button class="primary-button" data-action="close-sheet">Voltar para minha conta</button>`);
}
async function callStatus(action,id){
  if(serverMode){try{await api(`/calls/${encodeURIComponent(id)}/${action}`,{});await refreshLive();render();toast(action==='accept'?'Atendimento com você.':'Atendimento concluído.');}catch(e){toast(e.message);await refreshLive().catch(()=>{});render();}return;}
  state=loadState();
  const call=state.calls.find(c=>c.id===id);
  if(!call)return;
  if(action==='accept'&&call.status==='pending'){call.status='progress';call.accepted=Date.now();saveState();render();toast(`Mesa ${tableName(call.table)}: atendimento com você.`);}
  else if(action==='complete'&&call.status==='progress'){call.status='done';call.completed=Date.now();saveState();render();toast(`Mesa ${tableName(call.table)}: atendimento concluído.`);}
  else {render();toast('Esse atendimento já foi atualizado.');}
}
function openTableDetails(n){
  if(serverMode){openLiveTable(n);return;}
  const calls=state.calls.filter(c=>c.table===n&&['pending','progress'].includes(c.status));
  openSheet(`Mesa ${tableName(n)}`,calls.length?`<p class="sheet-description">${calls.length} ${calls.length===1?'chamado ativo':'chamados ativos'} nesta mesa.</p><div class="call-list">${calls.map(waiterCard).join('')}</div>`:`<div class="empty-state" style="margin-bottom:12px">${icon('circleCheck')}<h3>Nenhum chamado ativo.</h3><p>O atendimento desta mesa está em dia.</p></div><button class="primary-button" data-action="close-sheet">Tudo certo</button>`);
}
function openOptions(){
  if(serverMode){openSheet('Mezô · à sua mesa',`<p class="sheet-description">${escapeHTML(restaurantName)}<br>Seu atendimento e sua conta acompanham esta mesa.</p><div class="option-list"><button data-action="switch-role">${icon('people')} ${role==='customer'?'Entrar como equipe':'Ver minha mesa'}</button>${role==='waiter'?`<button data-action="logout">${icon('lock')} Sair da equipe</button>`:''}<a href="/admin">Administração Mezô →</a></div>`);return;}
  openSheet('À vontade, sempre.',`<p class="sheet-description">Casa do Sol · cozinha & boa companhia.<br>Estamos aqui para cuidar da sua experiência.</p><div class="option-list"><button data-action="about">${icon('info')} Sobre esta prévia</button><button data-action="switch-role">${icon('people')} ${role==='customer'?'Ver painel do garçom':'Voltar para a visão do cliente'}</button><button data-action="reset-confirm">${icon('reset')} Reiniciar demonstração</button></div>`);
}
document.addEventListener('click',async event=>{
  const target=event.target.closest('button, a');if(!target)return;
  if(target.dataset.role){closeSheet();location.hash=target.dataset.role==='waiter'?'fila':'mesa';return;}
  if(target.dataset.reason){selectedReason=target.dataset.reason;sheet.querySelectorAll('[data-reason]').forEach(b=>{b.classList.toggle('selected',b.dataset.reason===selectedReason);b.setAttribute('aria-pressed',String(b.dataset.reason===selectedReason));});return;}
  if(target.dataset.category){category=target.dataset.category;document.querySelectorAll('[data-category]').forEach(b=>{b.classList.toggle('active',b.dataset.category===category);b.setAttribute('aria-pressed',String(b.dataset.category===category));});menuList();return;}
  if(target.dataset.filter){filter=target.dataset.filter;renderQueue();return;}
  if(target.dataset.method){selectedMethod=target.dataset.method;sheet.querySelectorAll('[data-method]').forEach(b=>{b.classList.toggle('selected',b.dataset.method===selectedMethod);b.setAttribute('aria-pressed',String(b.dataset.method===selectedMethod));});const b=sheet.querySelector('[data-action="simulate-payment"]');b.disabled=false;b.innerHTML=`Ver prévia com ${selectedMethod==='pix'?'Pix':'cartão'} ${icon('arrow')}`;return;}
  const action=target.dataset.action;
  if(serverMode&&['login','logout','consumption','close-table','confirm-close','open-table'].includes(action)){event.preventDefault();await liveAction(action,target);return;}
  if(action==='call')openCall();
  else if(action==='close-sheet')closeSheet();
  else if(action==='send-call')sendCall();
  else if(action==='dish')openDish(target.dataset.id);
  else if(action==='ask-dish'){const dish=menu.find(d=>d.id===target.dataset.id);openCall('order',`Gostaria de pedir: ${escapeHTML(dish.name)}.`);}
  else if(action==='payment')openPayment();
  else if(action==='simulate-payment')simulatePayment();
  else if(action==='ask-bill')openCall('bill');
  else if(action==='accept'||action==='complete'){const inSheet=sheet.contains(target);callStatus(action,target.dataset.id);if(inSheet)closeSheet();}
  else if(action==='table-details')openTableDetails(Number(target.dataset.table));
  else if(action==='clear-search'){query='';category='Todos';renderMenu();}
  else if(action==='switch-role'){closeSheet();location.hash=role==='customer'?'fila':'mesa';}
  else if(action==='about')openSheet('Uma mesa para experimentar.',`<p class="sheet-description">Esta interface usa o restaurante fictício Casa do Sol. Os pratos, valores, mesas e atendimentos são exemplos.</p><div class="demo-info">${icon('info')}<span>Chamados são simulados neste navegador. Nenhuma equipe real recebe notificações e nenhum pagamento é processado.</span></div><p class="sheet-description">Explore a visão do cliente e do garçom para acompanhar um chamado do início ao fim.</p><button class="primary-button" data-action="close-sheet">Vamos experimentar</button>`);
  else if(action==='reset-confirm')openSheet('Começar de novo?',`<p class="sheet-description">Os chamados criados nesta demonstração serão apagados e os exemplos iniciais voltarão.</p><div class="button-row"><button class="secondary-button" data-action="close-sheet">Manter como está</button><button class="primary-button" data-action="reset">Reiniciar</button></div>`);
  else if(action==='reset'){state=seedState();saveState();query='';category='Todos';filter='all';closeSheet();if(location.hash==='#mesa')navigate('mesa',true);else location.hash='mesa';toast('Demonstração reiniciada.');}
});
document.addEventListener('input',event=>{
  if(event.target.id==='menu-search'){query=event.target.value;menuList();}
  if(event.target.id==='call-note')sheet.querySelector('#note-count').textContent=`${event.target.value.length}/140`;
});
document.addEventListener('change',async event=>{
  if(event.target.id==='service-toggle'){if(serverMode){try{await api('/service',{enabled:event.target.checked});await refreshLive();render();}catch(e){toast(e.message);await refreshLive().catch(()=>{});render();}return;}state=loadState();state.service=event.target.checked;saveState();document.querySelector('#service-value').textContent=money(serviceValue());document.querySelector('#bill-total').textContent=money(totalValue());}
});
document.querySelector('#more-button').addEventListener('click',openOptions);
sheet.addEventListener('click',event=>{
  if(event.target===sheet){const r=sheet.getBoundingClientRect();if(event.clientY<r.top||event.clientY>r.bottom||event.clientX<r.left||event.clientX>r.right)closeSheet();}
});
window.addEventListener('hashchange',()=>navigate(location.hash.slice(1),true));
window.addEventListener('storage',event=>{if(!serverMode&&event.key===storageKey){state=loadState();render();}});
setInterval(()=>{if(!sheet.open&&document.activeElement?.tagName!=='INPUT'&&document.activeElement?.tagName!=='TEXTAREA')render();},60000);
document.querySelectorAll('[data-icon]').forEach(el=>el.innerHTML=icon(el.dataset.icon));
navigate(location.hash.slice(1)||(serverMode&&location.pathname.endsWith('/equipe')?'fila':'mesa'));
