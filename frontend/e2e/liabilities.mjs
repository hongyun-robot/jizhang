import { chromium } from 'playwright-core';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const base = process.env.LIABILITY_E2E_BASE ?? 'http://127.0.0.1:5602';
assert(['127.0.0.1','localhost'].includes(new URL(base).hostname));
const fixture = await (await fetch(base + '/api/v1/__test__/info')).json();
assert.equal(fixture.fixture, 'liability-browser', 'refuse to write to a real ledger');
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXE ?? 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true });
const errors=[]; const shots=path.resolve('e2e/shots'); fs.mkdirSync(shots,{recursive:true}); let checks=0;
const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN',timezoneId:'Asia/Shanghai'});
await context.addInitScript(() => { const NativeDate=Date; let time=NativeDate.parse('2026-11-01T12:00:00+08:00');try{time=Number(localStorage.getItem('__liability_test_date'))||time;}catch{}globalThis.__liability_test_date=time;globalThis.Date=class extends NativeDate{constructor(...args){super(...(args.length?args:[globalThis.__liability_test_date]));}static now(){return globalThis.__liability_test_date;}}; });
const page=await context.newPage(); let authorization='';
page.on('request',(r)=>{authorization=r.headers().authorization||authorization;});
page.on('pageerror',(e)=>errors.push(e.message));
const api=async(method,endpoint,input)=>{const r=await context.request.fetch(base+'/api/v1'+endpoint,{method,headers:{Authorization:authorization},data:input});const b=await r.json();assert.equal(r.status(),200,JSON.stringify(b));assert.equal(b.code,0,JSON.stringify(b));return b.data;};
const check=(condition,label)=>{assert(condition,label);checks++;console.log(`PASS ${checks}: ${label}`);};
const dialog=(name)=>page.getByRole('dialog',{name,exact:true});
const han=(name)=>new RegExp('^'+[...name].join('\\s*')+'$');
const choose=async(modal,label,value)=>{await modal.getByLabel(label,{exact:true}).click();await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({hasText:value}).first().click();};
const waitState=async(id,predicate,label)=>{for(let i=0;i<40;i++){const d=await api('GET',`/accounts/${id}/liability`);if(predicate(d))return d;await new Promise(r=>setTimeout(r,100));}throw Error('state timeout: '+label);};
const clock=async(date)=>{const r=await context.request.post(base+'/api/v1/__test__/date',{data:{date}});assert.equal(r.status(),200,await r.text());await page.evaluate((d)=>{const n=Date.parse(d+'T12:00:00+08:00');localStorage.setItem('__liability_test_date',String(n));globalThis.__liability_test_date=n;},date);};
const login=async(user)=>{await page.goto(base+'/login');await page.getByPlaceholder('用户名').fill(user);await page.getByPlaceholder('密码').fill('voucher-test-only');await page.getByRole('button',{name:/登\s*录/}).click();await page.waitForURL(base+'/');};
try {
 await login('voucher-admin');await clock('2026-11-01');
 await page.goto(base+'/accounts');await page.getByRole('button',{name:'新建账户'}).click();let m=dialog('新建账户');
 const cardName='测试花呗-'+Date.now();await m.getByLabel('名称',{exact:true}).fill(cardName);await m.locator('.ant-segmented-item').filter({hasText:'负债'}).click();await choose(m,'消费信用类型','花呗');
 await m.getByRole('button',{name:/确\s*定/}).click();await m.waitFor({state:'hidden'});
 const accounts=await api('GET','/accounts');const card=accounts.find(a=>a.name===cardName);check(card?.billing_enabled,'administrator configures a zero-balance consumer-credit account');
 await page.goto(base+`/liabilities/${card.id}`);await page.getByRole('button',{name:/记分期/}).click();m=dialog('记录固定分期');
 await m.getByLabel('消费本金',{exact:true}).fill('3000');await m.getByLabel('每期手续费',{exact:true}).fill('20');
 await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 let d=await waitState(card.id,x=>x.plans.length===1,'plan');const planId=d.plans[0].id;
 check(d.summary.total_debt==='3000'&&d.summary.forecast_fees==='60','one principal expense; manually entered future fees stay forecasts');
 check(d.plans[0].terms.every(t=>t.principal==='1000'),'three equal principal terms');
 await clock('2026-12-01');await page.reload();await page.locator('.liability-summary').waitFor();d=await api('GET',`/accounts/${card.id}/liability`);
 check(d.summary.current_due==='1020'&&d.summary.total_debt==='3020','first actual fee and principal form the billed amount');
 await page.getByRole('button',{name:han('还款'),exact:true}).click();m=dialog('记录还款');await m.getByLabel('还款金额',{exact:true}).fill('1220');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await waitState(card.id,x=>x.summary.pending_credit==='200','prepayment');check(d.summary.total_debt==='1800'&&d.summary.current_due==='0','repayment is allocated once; excess funds remain available');
 await page.getByRole('tab',{name:'分期计划',exact:true}).click();await page.locator('.liability-plan').first().getByRole('button',{name:'查看流水',exact:true}).first().click();
 m=dialog('资金流水');await m.getByRole('button',{name:'记录实际退款'}).click();m=dialog('记一笔');
 await m.locator('input[inputmode="decimal"]').first().fill('500');await m.getByRole('button',{name:han('记账'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await waitState(card.id,x=>x.plans[0].status==='needs_reconcile','refund');check(d.summary.total_debt==='1300','principal refund lowers debt and requests plan reconciliation');
 await page.locator('.liability-plan').first().getByRole('button',{name:'核对退款后的期次'}).click();m=dialog('调整分期与费用');await choose(m,'已到账的本金退款记录','2026-12-01');await m.getByLabel('核对 / 操作说明').fill('按平台实际剩余本金核对');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await waitState(card.id,x=>x.plans[0].status==='active','reconciled');check(d.plans[0].terms.reduce((s,t)=>s+Number(t.remaining),0)===1500,'refund reconciliation does not erase unpaid principal');
 await page.locator('.liability-plan').first().getByRole('button',{name:'提前结清',exact:true}).click();m=dialog('提前结清');await m.getByLabel('实际结清手续费（手填）').fill('15');await m.getByRole('button',{name:'核对结清预览'}).click();await m.locator('.liability-preview').waitFor();
 check((await m.locator('.liability-preview').textContent()).includes('1,300.00'),'settlement preview includes the available 200 prepayment');
 await m.getByRole('button',{name:'确认结清'}).click();await m.waitFor({state:'hidden'});
 d=await waitState(card.id,x=>x.plans[0].status==='settled','settled');check(d.summary.total_debt==='0'&&d.summary.forecast_fees==='0','settlement pays principal, records actual fee, cancels future fees');
 check(d.transactions.some(t=>t.kind==='expense'&&t.amount==='15'&&t.liability_operation_id),'asset-paid settlement fee stays linked to the liability record');
 await page.screenshot({path:path.join(shots,'liabilities-desktop.png'),fullPage:true});
 await page.getByRole('tab',{name:'操作记录'}).click();const settlementRow=page.getByRole('row').filter({hasText:'提前结清'}).first();await settlementRow.getByRole('button',{name:'撤销并恢复'}).click();m=dialog('撤销并恢复记录');await m.getByLabel('核对 / 操作说明').fill('平台核对后撤销此次结清');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await waitState(card.id,x=>x.plans[0].status==='active','reversed');check(d.summary.total_debt==='1300'&&d.summary.forecast_fees==='40','dedicated reversal restores money and the future plan');
 for(const role of ['viewer','member','recorder']) {
  const roleContext=await browser.newContext({viewport:{width:role==='viewer'?390:1100,height:850},locale:'zh-CN',timezoneId:'Asia/Shanghai'});const p=await roleContext.newPage();let roleToken='';p.on('request',r=>{roleToken=r.headers().authorization||roleToken;});
  await p.goto(base+'/login');await p.getByPlaceholder('用户名').fill('voucher-'+role);await p.getByPlaceholder('密码').fill('voucher-test-only');await p.getByRole('button',{name:/登\s*录/}).click();await p.waitForURL(base+'/');await p.goto(base+'/liabilities');await p.locator('.liability-accounts').waitFor();
  check(await p.locator('.liability-account-row').count()===5&&!((await p.locator('body').textContent()).includes(cardName)),role+' receives five own defaults without seeing the administrator debt');
  const denied=await roleContext.request.get(base+`/api/v1/accounts/${card.id}/liability`,{headers:{Authorization:roleToken}});check(denied.status()===403,role+' cannot read another user’s liability by ID');
  const owned=[];
  for(let i=0;i<2;i++){
   await p.getByRole('button',{name:/新增负债账户/}).click();let dm=p.getByRole('dialog',{name:'新增负债账户',exact:true});await dm.getByLabel('账户名称').fill(role+'花呗账号'+i);await dm.getByLabel('消费信用类型').click();await p.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({hasText:'花呗'}).click();
   if(i===0){await dm.getByLabel('账单日（1–31）').fill('20');await dm.getByLabel('还款日（1–31）').fill('25');await dm.getByLabel('还款月份').click();await p.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({hasText:'出账当月'}).click();}
   await dm.getByRole('button',{name:han('保存'),exact:true}).click();await dm.waitFor({state:'hidden'});await p.waitForURL(/\/liabilities\/\d+$/);const ownId=Number(new URL(p.url()).pathname.split('/').at(-1));const detailResponse=await roleContext.request.get(base+`/api/v1/accounts/${ownId}/liability`,{headers:{Authorization:roleToken}});const own=(await detailResponse.json()).data;owned.push(own);
   check(own.account.owner_id!==1&&own.rules[0].statement_day===(i===0?20:31)&&own.rules[0].repayment_day===(i===0?25:10),role+' owns an independently configured account '+i);
   const adminDenied=await context.request.get(base+`/api/v1/accounts/${ownId}/liability`,{headers:{Authorization:authorization}});check(adminDenied.status()===403,'administrator cannot inspect '+role+' private account');
   if(i===0){await p.goto(base+'/liabilities');await p.locator('.liability-accounts').waitFor();}
  }
  check(owned[0].account.id!==owned[1].account.id,role+' can own multiple accounts of the same platform');
  if(role==='viewer'){check(await p.getByRole('button',{name:han('还款'),exact:true}).count()===0,'read-only financial role cannot initiate a money operation');check(await p.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1),'mobile personal detail has no page overflow');await p.screenshot({path:path.join(shots,'liabilities-mobile.png'),fullPage:true});}
  await roleContext.close();
 }
 // Reconcile actual transactions through the page; the platform reference
 // cannot change the ledger by itself.
 await clock('2026-11-01');
 const referenceCard=await api('POST','/accounts',{name:cardName+'-核对',type:'liability',opening_balance:'0',liability_type:'douyin_monthly',statement_day:31,repayment_day:10,repayment_month_offset:1});
 const categories=await api('GET','/categories');const root=categories.flatMap(w=>w.children??[]).find(c=>c.kind==='expense');const expenseCategory=root?.children?.[0]?.id??root?.id;
 assert(expenseCategory,'expense fixture category');
 await api('POST','/transactions',{kind:'expense',amount:'480',account_id:referenceCard.id,category_id:expenseCategory,entry_date:'2026-11-15'});
 await clock('2026-12-01');await page.goto(base+`/liabilities/${referenceCard.id}`);
 await page.getByRole('row').filter({hasText:'2026-11-01 — 2026-11-30'}).getByRole('button',{name:'核对 / 调整',exact:true}).click();m=dialog('核对账单');
 await m.getByLabel('平台账单参考金额（仅用于核对）').fill('500');
 await m.getByText('差异已解决，标记核对完成').click();await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await api('GET',`/accounts/${referenceCard.id}/liability`);check(d.summary.total_debt==='480','platform reference does not rewrite the debt');
 await api('POST','/transactions',{kind:'expense',amount:'20',account_id:referenceCard.id,category_id:expenseCategory,entry_date:'2026-11-20'});await page.reload();
 await page.getByRole('row').filter({hasText:'2026-11-01 — 2026-11-30'}).getByRole('button',{name:'核对 / 调整',exact:true}).click();m=dialog('核对账单');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await api('GET',`/accounts/${referenceCard.id}/liability`);check(d.summary.total_debt==='500'&&d.statements.some(b=>b.reconciled_at),'supplementing the real 20 expense closes the reference difference');
 await page.getByRole('button',{name:han('还款'),exact:true}).click();m=dialog('记录还款');await m.getByLabel('还款金额').fill('600');await m.locator('p').filter({hasText:'本次保留待抵扣'}).filter({hasText:'100.00'}).waitFor();check((await m.textContent()).includes('100.00'),'overpayment preview shows the exact remaining credit');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 await api('POST','/transactions',{kind:'expense',amount:'80',account_id:referenceCard.id,category_id:expenseCategory,entry_date:'2026-12-02'});d=await api('GET',`/accounts/${referenceCard.id}/liability`);check(d.summary.balance==='20','new ordinary purchase consumes overpayment without another money entry');
 // Handoff from the ordinary entry form creates a single principal source.
 const draftCard=await api('POST','/accounts',{name:cardName+'-分期入口',type:'liability',opening_balance:'0',liability_type:'jd_baitiao',statement_day:31,repayment_day:10,repayment_month_offset:1});
 await page.goto(base+'/');await page.getByRole('button',{name:/快捷记账/}).click();m=dialog('记一笔');await m.locator('input[inputmode="decimal"]').first().fill('100');await m.locator('.cat-leaves .ant-tag').first().click();await m.getByRole('combobox').last().fill(draftCard.name);await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({hasText:draftCard.name}).click();
 await m.getByRole('button',{name:'填写期次和手续费，记固定分期'}).click();m=dialog('记录固定分期');check(await m.getByLabel('消费本金',{exact:true}).inputValue()==='100','ordinary entry passes its draft to installments');
 await choose(m,'手填手续费方式','总手续费按期分摊');await m.getByLabel('总手续费',{exact:true}).fill('10');await m.getByText('3.34',{exact:true}).waitFor();check((await m.textContent()).includes('3.34'),'fee preview absorbs the exact final-cent remainder');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await api('GET',`/accounts/${draftCard.id}/liability`);check(d.transactions.filter(t=>t.kind==='expense').length===1&&d.summary.total_debt==='100','draft handoff records the principal once');
 await page.getByRole('button',{name:/记分期/}).click();m=dialog('记录固定分期');await m.getByLabel('消费本金',{exact:true}).fill('200');await choose(m,'手填手续费方式','一次性收取');await m.getByLabel('总手续费',{exact:true}).fill('60');await choose(m,'费用支付账户','测试现金');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await api('GET',`/accounts/${draftCard.id}/liability`);check(d.summary.total_debt==='300'&&d.transactions.filter(t=>t.amount==='60'&&t.kind==='expense'&&t.account_id!==draftCard.id).length===1,'upfront asset-paid fee is one actual expense and adds no debt');
 await page.getByRole('button',{name:/记分期/}).click();m=dialog('记录固定分期');await m.getByLabel('消费本金',{exact:true}).fill('30');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await api('GET',`/accounts/${draftCard.id}/liability`);check(d.plans[0].terms.every(t=>t.fee_amount==='0'&&t.fee_txn_id===null)&&d.summary.total_debt==='330','explicit zero fee creates no zero money transaction');
 await page.setViewportSize({width:390,height:850});await page.getByRole('button',{name:han('还款'),exact:true}).click();m=dialog('记录还款');check(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1),'mobile repayment modal has no page overflow');await m.getByRole('button',{name:han('取消'),exact:true}).click();await page.setViewportSize({width:1440,height:1000});
 await api('POST','/transactions',{kind:'expense',amount:'30',account_id:referenceCard.id,category_id:expenseCategory,entry_date:'2026-12-02'});await clock('2027-01-01');await page.goto(base+`/liabilities/${referenceCard.id}`);await page.getByRole('button',{name:han('还款'),exact:true}).click();m=dialog('记录还款');await m.getByLabel('还款金额').fill('20');await choose(m,'还款分配','手动指定账单');await m.getByRole('button',{name:'添加账单分配'}).click();await m.getByLabel('指定账单').click();await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content, .selection-sheet:visible .selection-sheet-item').filter({hasText:'2026-12-31'}).click();await m.getByLabel('分配金额').fill('5');await m.locator('p').filter({hasText:'本次保留待抵扣'}).filter({hasText:'15.00'}).waitFor();check((await m.textContent()).includes('15.00'),'manual allocation preview preserves the unused 15');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});
 d=await api('GET',`/accounts/${referenceCard.id}/liability`);check(d.summary.pending_credit==='15'&&d.summary.current_due==='5','manual target and remaining credit persist after save');
 await clock('2026-12-01');await page.goto(base+'/records');const feeRow=page.getByRole('row').filter({hasText:'60.00'});await feeRow.getByRole('button',{name:'查看负债记录'}).click();m=dialog('资金流水');check(page.url().includes(`/liabilities/${draftCard.id}`)&&(await m.textContent()).includes('费用由资产直接支付'),'asset-paid managed fee links to its liability and shows separate bill attribution');
 await clock('2027-01-07');await page.goto(base+'/notifications');let reminder=page.locator('.notif-row').filter({hasText:referenceCard.name});await reminder.waitFor();check((await reminder.textContent()).includes('欠款 ¥5.00'),'reminder page shows the actual billed remainder');await reminder.getByRole('button',{name:'标为已读'}).click();await reminder.getByText('已读',{exact:true}).waitFor();await reminder.getByRole('button',{name:'查看账单'}).click();check(page.url().includes(`/liabilities/${referenceCard.id}?bill=`),'reminder opens the referenced bill');
 await page.getByRole('button',{name:han('还款'),exact:true}).click();m=dialog('记录还款');await m.getByLabel('还款金额').fill('5');await m.getByRole('button',{name:han('保存'),exact:true}).click();await m.waitFor({state:'hidden'});await page.goto(base+'/notifications');reminder=page.locator('.notif-row').filter({hasText:referenceCard.name});await reminder.getByText('已解决',{exact:true}).waitFor();check((await reminder.textContent()).includes('原提醒金额 ¥5.00')&&!((await reminder.getAttribute('class')).includes('unread')),'resolved reminder preserves history and exits unread');
 await page.goto(base+'/accounts');await page.getByRole('button',{name:'新建账户'}).click();m=dialog('新建账户');const customName=cardName+'-数据库类型';await m.getByLabel('名称',{exact:true}).fill(customName);await m.locator('.ant-segmented-item').filter({hasText:'负债'}).click();await choose(m,'消费信用类型','测试月付');await m.getByRole('button',{name:/确\s*定/}).click();await m.waitFor({state:'hidden'});const customAccount=(await api('GET','/accounts')).find(a=>a.name===customName);check(customAccount.liability_type==='test_monthly'&&customAccount.billing_enabled,'account form and validation accept a type defined only in the database');await page.goto(base+`/liabilities/${customAccount.id}`);await page.getByRole('heading',{name:/测试月付/}).waitFor();check((await page.locator('.page-head').textContent()).includes('测试月付'),'liability detail uses the database label for a new type');
 check(errors.length===0,'no browser runtime errors: '+errors.join('; '));console.log(`LIABILITY E2E: ${checks} checks passed; plan ${planId}`);
} catch(e) {console.error(e);console.error('CURRENT PAGE',await page.locator('body').innerText());await page.screenshot({path:path.join(shots,'liabilities-failure.png'),fullPage:true});process.exitCode=1;} finally {await browser.close();}
