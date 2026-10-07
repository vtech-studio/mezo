'use strict';
(() => {
 const slug=document.body.dataset.slug, view=document.body.dataset.view;
 const content=document.getElementById('content'),nav=document.getElementById('navigation'),connection=document.getElementById('connection'),drawer=document.getElementById('drawer'),body=document.getElementById('drawer-body');
 const esc=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 const money=value=>new Intl.NumberFormat('pt-BR',{style:'currency',currency:'BRL'}).format(value/100);
 const age=value=>{const m=Math.max(0,Math.floor((Date.now()-value)/60000));return m===0?'agora':`há ${m} min`;};
 const labels={queued:'Na fila',preparing:'Em preparo',ready:'Pronto',delivered:'Entregue',cancelled:'Cancelado'};
 const reasons={help:'Preciso de ajuda',order:'Ajuda para pedir',water:'Pedir água',bill:'Pedir a conta'};
 let data=null,csrf='',busy=false,refreshing=false,online=false,timer=null,toastTimer=null,filter='active',route=location.hash.slice(1)|| (view==='kitchen'?'pedidos':'chamados');
 let pending=null,revision=null,loadedAt=0;
 const pendingKey=()=>`mezo-pending:${slug}:${data?.staff.id}`;
 const savePending=()=>{try{if(pending)sessionStorage.setItem(pendingKey(),JSON.stringify(pending));else sessionStorage.removeItem(pendingKey());}catch{}};
 const notify=message=>{const t=document.getElementById('toast');t.textContent=message;t.hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>t.hidden=true,4500);};
 const button=(text,action,id='',style='')=>`<button class="button ${style}" data-action="${action}" data-id="${esc(id)}" ${busy||!online?'disabled':''}>${text}</button>`;
 async function api(path, payload){
  const controller=new AbortController(),timeout=setTimeout(()=>controller.abort(),15000);
  try{
   const res=await fetch(`/api/r/${encodeURIComponent(slug)}${path}`,{credentials:'same-origin',signal:controller.signal,headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},...(payload!==undefined?{method:'POST',body:JSON.stringify(payload)}:{})});
   let result;try{result=await res.json();}catch{throw new Error('Resposta indisponível. Confira a conexão e tente novamente.');}
   if(!res.ok){const error=new Error(result.error||'Não foi possível concluir');error.status=res.status;throw error;}
   return result;
  }catch(e){if(e.name==='AbortError'||e instanceof TypeError)throw new Error('Sem resposta do servidor. Confira a conexão antes de tentar novamente.');throw e;}finally{clearTimeout(timeout);}
 }
 function showLogin(message=''){
  data=null;online=false;nav.innerHTML='';document.getElementById('account-button').hidden=true;connection.textContent=`Acesso da ${view==='kitchen'?'cozinha':'equipe do salão'}`;connection.className='connection';
  if(drawer.open)drawer.close();
  content.innerHTML=`<section class="login"><p class="eyebrow">${view==='kitchen'?'COZINHA':'GARÇOM'} · ACESSO DA EQUIPE</p><h1>${view==='kitchen'?'Cada prato,<br><em>no seu tempo.</em>':'Bom atendimento<br><em>começa aqui.</em>'}</h1><p class="intro">Entre com o acesso fornecido pelo restaurante.</p><form id="login-form"><label for="email">E-mail</label><input id="email" name="email" type="email" autocomplete="username" required maxlength="200"><label for="password">Senha</label><input id="password" name="password" type="password" autocomplete="current-password" required maxlength="72"><p class="error" id="form-error" role="alert">${esc(message)}</p><button class="button full">Entrar →</button></form><p class="hint">Cada pessoa deve usar seu próprio acesso.</p><a class="hint" href="/r/${esc(slug)}/${view==='kitchen'?'garcom':'cozinha'}">Acesso ${view==='kitchen'?'do garçom':'da cozinha'} →</a></section>`;
 }
 function markUpdated(){
  connection.textContent=`${data.staff.name} · atualizado ${new Date().toLocaleTimeString('pt-BR',{hour:'2-digit',minute:'2-digit'})}`;connection.className='connection';document.getElementById('account-button').hidden=false;
 }
 // Between full reloads, a small revision check tells whether anything changed in the restaurant.
 async function poll(){
  if(document.hidden||busy||refreshing||!(data||online))return;
  if(data&&online&&revision!==null&&Date.now()-loadedAt<60000){
   try{const result=await api('/revision?role=staff');if(result.revision===revision){markUpdated();return;}}catch{}
  }
  await refresh();
 }
 async function refresh(){
  if(refreshing)return;refreshing=true;
  try{
   const result=await api(`/operations?view=${view}`);data=result;csrf=result.csrf;online=true;revision=result.revision??null;loadedAt=Date.now();
   markUpdated();
   if(!pending){try{const stored=JSON.parse(sessionStorage.getItem(pendingKey()));if(stored&&typeof stored.key==='string'&&Array.isArray(stored.items))pending=stored;}catch{}}
   render();
  }catch(e){
   if(e.status===401){showLogin();}else if(e.status===403){showLogin(e.message);}else{online=false;connection.textContent='Sem atualização · confira a conexão e tente atualizar';connection.className='connection offline';if(data)render();else showLogin(e.message);}
  }finally{refreshing=false;}
 }
 function navigation(){
  const links=view==='kitchen'?[['pedidos','◷','Pedidos'],['historico','✓','Histórico']]:[['chamados','◉','Chamados'],['mesas','▦','Mesas'],['pedidos','◷','Pedidos'],['historico','✓','Histórico']];
  nav.innerHTML=links.map(([id,icon,name])=>`<a href="#${id}" class="${route===id?'active':''}" ${route===id?'aria-current="page"':''}><span aria-hidden="true">${icon}</span>${name}</a>`).join('');
 }
 function ticket(o){
  let action='';
  if(view==='kitchen'&&o.status==='queued')action=button('Começar preparo →','start',o.id,'full');
  if(view==='kitchen'&&o.status==='preparing'&&(o.assignedId===data.staff.id||data.staff.role==='manager'))action=button('Marcar como pronto ✓','ready',o.id,'full');
  if(view==='waiter'&&o.status==='ready')action=button('Confirmar entrega ✓','deliver',o.id,'full');
  if(view==='waiter'&&data.staff.role==='manager'&&['queued','preparing','ready'].includes(o.status))action+=`<div class="actions">${button('Cancelar pedido','cancel',o.id,'danger')}</div>`;
  return `<article class="card"><div class="card-top"><div><span class="ticket-id">PEDIDO ${esc(o.id.slice(-5).toUpperCase())}</span><h2 class="table-number">Mesa ${String(o.table).padStart(2,'0')}</h2><p class="meta">${o.source==='customer'?'Pedido pelo cliente':'Pedido pelo salão'} · ${age(o.created)}</p></div><span class="badge ${o.status}">${labels[o.status]}</span></div><ul class="ticket-items">${o.items.map(i=>`<li><span class="quantity">${i.qty}×</span><div><h3>${esc(i.name)}</h3>${i.note?`<small>${esc(i.note)}</small>`:''}</div></li>`).join('')}</ul>${o.note?`<p class="note">${esc(o.note)}</p>`:''}${o.assigned?`<p class="meta">Preparo: ${esc(o.assigned)}</p>`:''}${action}</article>`;
 }
 function render(){
  if(!data)return;navigation();
  if(route==='mesas'&&view==='waiter'){
   content.innerHTML=`<p class="eyebrow">SALÃO · ${data.tables.length} MESAS</p><h1>Cada mesa<br><em>importa.</em></h1><p class="intro">Acompanhe as contas e ajude o cliente quando precisar.</p>${pending?`<article class="card"><h3>Um envio precisa de confirmação</h3><p class="hint">Mesa ${pending.table}. Confira os pedidos antes de iniciar outro.</p>${button('Retomar envio','resume','','full')}</article>`:''}<div class="table-grid">${data.tables.map(t=>{const calls=data.calls.filter(c=>c.table===t.number&&['pending','progress'].includes(c.status));return `<button class="table-button ${calls.length?'called':''}" data-action="table" data-id="${t.number}"><span class="badge ${calls.length?'queued':''}">${calls.length?'Chamado':t.sessionId?'Aberta':'Livre'}</span><strong>${String(t.number).padStart(2,'0')}</strong><small>${t.sessionId?money(t.subtotal)+' · consumo':'Pronta para receber'}</small></button>`;}).join('')}</div>`;return;
  }
  if(route==='chamados'&&view==='waiter'){
   const active=data.calls.filter(c=>['pending','progress'].includes(c.status)).sort((a,b)=>a.created-b.created);
   const ready=data.orders.filter(o=>o.status==='ready');
   content.innerHTML=`<p class="eyebrow">BOM ATENDIMENTO, ${esc(data.staff.name.split(' ')[0])}</p><h1>A casa chama.<br><em>Você cuida.</em></h1><div class="stats"><div class="stat"><strong>${active.filter(c=>c.status==='pending').length}</strong>aguardando</div><div class="stat"><strong>${active.filter(c=>c.assignedId===data.staff.id).length}</strong>com você</div><div class="stat"><strong>${ready.length}</strong>prontos na cozinha</div></div>${ready.length?`<div class="section-head"><h2>Prontos para servir</h2><a class="hint" href="#pedidos">Ver pedidos →</a></div>${ready.map(ticket).join('')}`:''}<div class="section-head"><h2>Chamados do salão</h2><span class="badge">${active.length}</span></div>${active.length?active.map(c=>`<article class="card"><div class="card-top"><h2 class="table-number">Mesa ${String(c.table).padStart(2,'0')}</h2><span class="badge ${c.status==='pending'?'queued':''}">${c.status==='pending'?'Aguardando':esc(c.assigned)}</span></div><h3>${reasons[c.reason]}</h3>${c.note?`<p class="note">${esc(c.note)}</p>`:''}<p class="meta">Chamado ${age(c.created)}</p>${c.status==='pending'?button('Assumir atendimento →','accept',c.id,'full'):c.assignedId===data.staff.id?button('Abrir mesa e conta →','table',c.table,'full'):'<p class="hint">Já está com outro atendente.</p>'}${c.status==='progress'&&data.staff.role==='manager'?`<div class="actions">${button('Devolver à fila','release',c.id,'secondary')}</div>`:''}</article>`).join(''):empty('Tudo certo no salão.','Os chamados das mesas aparecem aqui.')}`;return;
  }
  const history=route==='historico';const orders=data.orders.filter(o=>history?['delivered','cancelled'].includes(o.status):filter==='active'?['queued','preparing','ready'].includes(o.status):o.status===filter);
  content.innerHTML=`<p class="eyebrow">${view==='kitchen'?'COZINHA':'SALÃO & COZINHA'}</p><h1>${history?'O cuidado<br><em>fica registrado.</em>':view==='kitchen'?'Tudo no ponto.<br><em>Uma mesa por vez.</em>':'Da cozinha<br><em>para a mesa.</em>'}</h1><p class="intro">${history?'Últimos 100 pedidos concluídos ou cancelados.':view==='kitchen'?'Assuma o preparo e avise quando estiver pronto.':'Acompanhe o preparo e confirme a entrega ao cliente.'}</p>${!history?`<div class="tabs" aria-label="Filtrar pedidos">${[['active','Todos'],['queued','Na fila'],['preparing','Em preparo'],['ready','Prontos']].map(([id,name])=>`<button class="tab ${filter===id?'active':''}" data-filter="${id}" aria-pressed="${filter===id}">${name} · ${data.orders.filter(o=>id==='active'?['queued','preparing','ready'].includes(o.status):o.status===id).length}</button>`).join('')}</div>`:''}<div class="board">${orders.length?orders.map(ticket).join(''):empty(history?'O histórico começa aqui.':'Nenhum pedido nesta fila.',history?'Os pedidos concluídos aparecerão aqui.':'A fila se atualiza automaticamente.')}</div>`;
 }
 function empty(title,text){return `<div class="empty"><h3>${title}</h3><p>${text}</p></div>`;}
 function open(title,html){document.getElementById('drawer-title').textContent=title;body.innerHTML=html;if(!drawer.open)drawer.showModal();}
 function error(message){let e=body.querySelector('.error');if(!e){e=document.createElement('p');e.className='error';e.setAttribute('role','alert');body.append(e);}e.textContent=message;}
 async function table(number){
  const t=data.tables.find(t=>t.number===number);if(!t)return;
  if(!t.sessionId){open(`Mesa ${String(number).padStart(2,'0')}`,`<p class="drawer-intro">Abra uma conta nova para receber os clientes.</p>${button('Abrir mesa','open',number,'full')}`);return;}
  open(`Mesa ${String(number).padStart(2,'0')}`,`<p class="drawer-intro">Carregando a conta…</p>`);
  try{const bill=await api(`/tables/${number}/bill`);const calls=data.calls.filter(c=>c.table===number&&c.status==='progress'&&c.assignedId===data.staff.id);open(`Mesa ${String(number).padStart(2,'0')}`,`<p class="eyebrow">CONTA ABERTA</p>${calls.map(c=>`<article class="table-call"><strong>${reasons[c.reason]}</strong>${c.note?`<p class="note">${esc(c.note)}</p>`:''}${button('Concluir atendimento ✓','complete',c.id,'secondary full')}</article>`).join('')}${bill.items.length?bill.items.map(i=>`<div class="summary-line"><span>${i.qty}× ${esc(i.name)}</span><strong>${money(i.price*i.qty)}</strong></div>`).join(''):'<p class="hint">Nenhum consumo registrado.</p>'}<div class="total"><span>Subtotal</span><span>${money(bill.subtotal)}</span></div><div class="total"><span>Serviço opcional</span><span>${money(bill.service)}</span></div><div class="total grand"><span>Total</span><span>${money(bill.total)}</span></div><div class="actions">${button('Adicionar pedido pelo cliente →','new-order',number)}${button('Encerrar conta','settle',number,'secondary')}</div><p class="hint">Pedidos confirmados entram na conta ao serem enviados. Cancelamentos são feitos pelo responsável.</p>`);}catch(e){error(e.message);}
 }
 function orderForm(number){
  if(pending){resume();return;}
  open(`Pedido · mesa ${number}`,`<form id="order-form" data-table="${number}"><p class="drawer-intro">Escolha as quantidades. As observações vão para a cozinha.</p>${data.menu.length?data.menu.map(i=>`<div class="cart-row"><div class="cart-top"><label for="qty-${esc(i.id)}">${esc(i.name)}<br><span class="hint">${money(i.price)}</span></label><input id="qty-${esc(i.id)}" type="number" data-menu="${esc(i.id)}" aria-label="Quantidade de ${esc(i.name)}" min="0" max="100" value="0"></div><input data-item-note="${esc(i.id)}" maxlength="200" placeholder="Observação do item (opcional)" aria-label="Observação para ${esc(i.name)}"></div>`).join(''):empty('O cardápio está vazio.','Cadastre os itens no painel administrativo.')}<label for="order-note">Observação geral</label><textarea id="order-note" maxlength="300" placeholder="Ex.: servir as entradas primeiro."></textarea><p class="error" role="alert"></p><button class="button full" ${!data.menu.length?'disabled':''}>Enviar para a cozinha →</button></form>`);
 }
 function resume(){open(`Confirmar envio · mesa ${pending.table}`,`<p class="drawer-intro">O envio ainda precisa ser confirmado. Tente novamente com este pedido; ele será lançado uma única vez.</p>${pending.items.map(i=>`<p>${i.qty}× ${esc(data.menu.find(m=>m.id===i.menuId)?.name||'Item do pedido')}</p>`).join('')}<p class="hint">Confira a lista de pedidos antes de abandonar este envio.</p><p class="error" role="alert"></p><div class="actions">${button('Enviar / confirmar','retry','','full')}${button('Revisar lista de pedidos','review','','secondary')}</div>`);}
 async function sendOrder(){
  try{await api('/orders',pending);pending=null;savePending();drawer.close();notify('Pedido recebido pela cozinha.');await refresh();}catch(e){error(e.message);if(e.status===400||e.status===409){pending=null;savePending();error(`${e.message}. Feche esta tela e revise o pedido.`);body.querySelectorAll('button[data-action="retry"]').forEach(b=>b.disabled=true);}}
 }
 async function settle(number){
  const bill=await api(`/tables/${number}/bill`);
  open(`Encerrar · mesa ${number}`,`<form id="settle-form" data-table="${number}" data-total="${bill.total}"><div class="total grand"><span>Total a receber</span><span>${money(bill.total)}</span></div><p class="drawer-intro">Confirme somente depois de receber o pagamento presencial.</p><label for="method">Pagamento recebido por</label><select id="method" required><option value="">Selecione</option><option value="pix">Pix conferido no banco</option><option value="card">Cartão na maquininha</option><option value="cash">Dinheiro</option><option value="other">Outra forma presencial</option></select><label><input id="received" type="checkbox" required> Confirmo o recebimento e o encerramento da mesa.</label><p class="error" role="alert"></p><button class="button full">Registrar recebimento e encerrar</button><p class="hint">O Mezô registra esta confirmação. O pagamento é realizado fora do aplicativo.</p></form>`);
 }
 function account(){open('Seu acesso',`<p class="eyebrow">${esc(data.staff.name)}</p><div class="account-links">${data.staff.role==='manager'?`<a class="button secondary" href="/r/${esc(slug)}/${view==='kitchen'?'garcom':'cozinha'}">Abrir ${view==='kitchen'?'salão':'cozinha'} →</a>${button('Recebimentos do dia','receipts','','secondary')}`:''}${button('Atualizar agora','refresh','','secondary')}${button('Trocar senha','password','','secondary')}${button('Sair do acesso','logout','','secondary')}</div>`);}
 document.getElementById('account-button').addEventListener('click',account);
 document.getElementById('close-drawer').addEventListener('click',()=>drawer.close());
 window.addEventListener('hashchange',()=>{route=location.hash.slice(1)||'pedidos';render();});
 document.addEventListener('click',async event=>{
  const f=event.target.closest('[data-filter]');if(f){filter=f.dataset.filter;render();return;}
  const b=event.target.closest('[data-action]');if(!b||busy)return;const action=b.dataset.action,id=b.dataset.id;
  if(action==='table'){await table(Number(id));return;}if(action==='new-order'){orderForm(Number(id));return;}if(action==='settle'){try{await settle(Number(id));}catch(e){error(e.message);}return;}
  if(action==='cancel'){open('Cancelar pedido',`<form id="cancel-form" data-id="${esc(id)}"><p class="drawer-intro">O valor será retirado da conta. Confirme com a cozinha antes de cancelar.</p><label for="cancel-note">Motivo obrigatório</label><textarea id="cancel-note" maxlength="300" required></textarea><p class="error" role="alert"></p><button class="button danger full">Confirmar cancelamento</button></form>`);return;}
  if(action==='password'){open('Trocar sua senha',`<form id="password-form"><label for="current">Senha atual</label><input id="current" type="password" autocomplete="current-password" required><label for="new-password">Nova senha</label><input id="new-password" type="password" autocomplete="new-password" minlength="12" maxlength="72" required><p class="hint">Os acessos anteriores serão encerrados.</p><p class="error" role="alert"></p><button class="button full">Salvar nova senha</button></form>`);return;}
  if(action==='resume'){resume();return;}if(action==='review'){drawer.close();location.hash='pedidos';return;}
  busy=true;b.disabled=true;
  try{
   if(action==='retry'){if(pending)await sendOrder();return;}
   if(action==='receipts'){
    const report=await api('/receipts');const methods={cash:'Dinheiro',pix:'Pix',card:'Cartão',other:'Outros',none:'Sem consumo'};
    open('Recebimentos do dia',`<p class="eyebrow">${esc(report.date)} · SÃO PAULO</p><div class="total grand"><span>${report.count} ${report.count===1?'conta encerrada':'contas encerradas'}</span><span>${money(report.total)}</span></div>${Object.entries(report.methods).map(([method,total])=>`<div class="total"><span>${methods[method]}</span><span>${money(total)}</span></div>`).join('')}<p class="hint">Valores confirmados presencialmente pela equipe. Confira com o banco e a maquininha.</p><a class="button secondary full" href="/api/r/${encodeURIComponent(slug)}/receipts?date=${encodeURIComponent(report.date)}&format=csv">Baixar recebimentos em CSV ↓</a>${report.records.slice(0,50).map(r=>`<div class="summary-line"><span>Mesa ${r.table} · ${esc(r.staff)}<br><span class="hint">${methods[r.method]} · ${new Date(r.created).toLocaleTimeString('pt-BR',{hour:'2-digit',minute:'2-digit',timeZone:'America/Sao_Paulo'})}</span></span><strong>${money(r.total)}</strong></div>`).join('')}`);return;
   }
   if(action==='refresh'){drawer.close();await refresh();return;}
   if(action==='logout'){await api('/logout',{});drawer.close();pending=null;showLogin();return;}
   if(action==='open')await api(`/tables/${id}/open`,{});
   else if(['accept','complete','release'].includes(action))await api(`/calls/${id}/${action}`,{});
   else if(['start','ready','deliver'].includes(action))await api(`/orders/${id}/${action}`,{});
   else return;
   const calledTable=action==='accept'?data.calls.find(c=>c.id===id)?.table:null;
   if(drawer.open)drawer.close();notify(action==='ready'?'O salão já pode servir.':'Atualizado.');
   await refresh();
   if(calledTable){busy=false;await table(calledTable);}
  }catch(e){if(drawer.open)error(e.message);else notify(e.message);await refresh();}finally{busy=false;if(b.isConnected)b.disabled=!online;render();}
 });
 document.addEventListener('submit',async event=>{
  event.preventDefault();const form=event.target;if(busy)return;busy=true;const submit=form.querySelector('button');submit.disabled=true;
  try{
   if(form.id==='login-form'){
    const result=await api('/login',{email:form.email.value,password:form.password.value});
    if(result.role==='kitchen'&&view!=='kitchen'){location.href=`/r/${slug}/cozinha`;return;}
    if(result.role==='waiter'&&view!=='waiter'){location.href=`/r/${slug}/garcom`;return;}
    await refresh();
   }else if(form.id==='order-form'){
    const items=[...form.querySelectorAll('[data-menu]')].filter(i=>Number(i.value)>0).map(i=>({menuId:i.dataset.menu,qty:Number(i.value),note:form.querySelector(`[data-item-note="${i.dataset.menu}"]`).value}));
    if(!items.length)throw new Error('Escolha pelo menos um item.');
    pending={table:Number(form.dataset.table),key:crypto.randomUUID(),note:document.getElementById('order-note').value,items};savePending();resume();await sendOrder();
   }else if(form.id==='cancel-form'){
    await api(`/orders/${form.dataset.id}/cancel`,{note:document.getElementById('cancel-note').value});drawer.close();notify('Pedido cancelado e retirado da conta.');await refresh();
   }else if(form.id==='settle-form'){
    await api(`/tables/${form.dataset.table}/close`,{confirmed:document.getElementById('received').checked,method:document.getElementById('method').value,expectedTotal:Number(form.dataset.total)});drawer.close();notify('Recebimento registrado. Mesa encerrada.');await refresh();
   }else if(form.id==='password-form'){
    await api('/password',{current:document.getElementById('current').value,password:document.getElementById('new-password').value});drawer.close();showLogin('Senha atualizada. Entre com sua nova senha.');
   }
  }catch(e){if(form.id==='login-form')document.getElementById('form-error').textContent=e.message;else error(e.message);}finally{busy=false;if(submit.isConnected)submit.disabled=false;render();body.querySelectorAll('[data-action="retry"]').forEach(b=>b.disabled=!online||!pending);}
 });
 document.addEventListener('visibilitychange',()=>{if(!document.hidden&&!busy)refresh();});
 window.addEventListener('online',()=>refresh());window.addEventListener('offline',()=>{online=false;connection.className='connection offline';connection.textContent='Sem conexão · os pedidos enviados continuam salvos';render();});
 refresh();timer=setInterval(poll,5000);
})();
