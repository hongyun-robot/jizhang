import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { assertFixture, withFixture, login, shot, until, fixtureApi, fixtureToken } from './support/fixture.mjs';

const base=process.env.WEALTH_E2E_BASE ?? 'http://127.0.0.1:5605';
await assertFixture(base,['wealth-browser']);
const prefix=`理财验收-${Date.now()}`;
const mobileMain=process.env.WEALTH_E2E_MOBILE==='1';
const date=(offset=0)=>new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Shanghai'}).format(new Date(Date.now()+offset*86400000));
const [openingDay,cashDay,confirmDay,rightsDay,paidDay]=[-7,-6,-4,-3,-1].map(date);
let assertions=0;
function check(value,text){assert(value,text);assertions++;}
async function fixture(path,input){const response=await fetch(base+'/api/v1/__test__/'+path,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(input)});assert.equal(response.status,200);}
async function client(username){
  const api=await fixtureApi(base,username);
  const request=async(...args)=>{assertions++;return api(...args);};
  await request('GET','/accounts');return {request,username};
}
async function chooseDate(page,input,value){
  if(!(await input.evaluate((element)=>element.readOnly))){await input.fill(value);await input.press('Enter');return;}
  await input.click();const panel=page.locator('.ant-picker-dropdown:visible').last();
  const year=parseInt(await panel.locator('.ant-picker-year-btn').innerText());const month=parseInt(await panel.locator('.ant-picker-month-btn').innerText());const [y,m]=value.split('-').map(Number);const offset=(y-year)*12+m-month;
  for(let i=0;i<Math.abs(offset);i++)await panel.locator(`.ant-picker-header-${offset<0?'prev':'next'}-btn`).click();
  await panel.locator(`td[title="${value}"] .ant-picker-cell-inner`).click();await panel.waitFor({state:'hidden'});
  await until(()=>input.inputValue(),(v)=>v===value,'date selection');
}
async function chooseOption(page,modal,value,kind='cash'){
  const selects=modal.getByRole('combobox');
  if(await selects.count()){await selects.last().click();await page.locator('.ant-select-dropdown:visible').getByText(value instanceof RegExp?value:value,{exact:typeof value==='string'}).click();return;}
  const name=kind==='product'?'已接入理财产品':'本人现金资产账户';
  await modal.getByRole('button',{name:'选择'+name,exact:true}).click();
  await page.getByRole('dialog',{name:'选择'+name,exact:true}).getByRole('button',{name:value}).click();
}
const admin=await client('voucher-admin');const viewer=await client('voucher-viewer');
const cash=await admin.request('POST','/accounts',{name:prefix+'现金',type:'asset',asset_class:'cash',opening_balance:'5000',request_key:randomUUID()});
const bank=async()=> (await admin.request('GET','/accounts')).find((a)=>a.id===cash.id).balance;
const createAccount=(actor,name)=>actor.request('POST','/accounts',{name,type:'asset',asset_class:'wealth',request_key:randomUUID()});

await withFixture(base,async(browser)=>{
  const context=await browser.newContext({viewport:{width:mobileMain?360:1280,height:900},...(mobileMain?{isMobile:true,hasTouch:true}:{})});const page=await context.newPage();page.setDefaultTimeout(12000);const errors=[];page.on('pageerror',(e)=>errors.push(e.message));
  try {
    await login(page,base);await page.goto(base+'/wealth');await page.getByRole('button',{name:/新建理财账户$/}).click();
    let modal=page.getByRole('dialog',{name:'新建账户',exact:true});await modal.getByLabel('名称',{exact:true}).fill(prefix);await modal.getByRole('button',{name:/保\s*存/}).click();await page.waitForURL(/\/wealth\/\d+$/);
    const accountId=Number(new URL(page.url()).pathname.split('/').at(-1));check(accountId>0,'wealth account missing');
    await page.getByRole('button',{name:'录入已有持仓',exact:true}).click();modal=page.getByRole('dialog',{name:'期初持仓',exact:true});await modal.getByText('暂无开放新增的产品',{exact:true}).waitFor();
    check(await modal.getByRole('button',{name:'保存记录',exact:true}).isDisabled(),'pending admission can be selected');
    await admin.request('POST',`/wealth/accounts/${accountId}/positions`,{request_key:randomUUID(),code:'J07241',shares:'100',cost_price:'1.2',effective_date:openingDay},400);
    await modal.getByRole('button',{name:/取\s*消/}).click();await fixture('wealth-admission',{status:'enabled'});await page.reload();
    await page.getByRole('button',{name:'录入已有持仓',exact:true}).click();modal=page.getByRole('dialog',{name:'期初持仓',exact:true});await chooseOption(page,modal,/苏银理财恒源日申周赎1号A/,'product');
    check(await modal.getByPlaceholder('输入六位场外基金代码').count()===0,'arbitrary code input exposed');
    await modal.getByLabel('已确认持有份额',{exact:true}).fill('100');await modal.getByLabel('实际总成本（元）',{exact:true}).fill('120');await chooseDate(page,modal.getByLabel('持仓生效日期',{exact:true}),openingDay);
    const keys=[];let attempts=0;const path=`**/api/v1/wealth/accounts/${accountId}/positions`;
    await page.route(path,async(route)=>{if(route.request().method()!=='POST')return route.continue();attempts++;keys.push(route.request().postDataJSON().request_key);const response=await route.fetch();if(attempts===1)return route.fulfill({status:500,contentType:'application/json',body:JSON.stringify({code:20000,message:'理财测试回执丢失'})});return route.fulfill({response});});
    await modal.getByRole('button',{name:'保存记录',exact:true}).click();await page.getByText('理财测试回执丢失',{exact:true}).waitFor();check(await modal.getByLabel('已确认持有份额',{exact:true}).inputValue()==='100','lost response discarded draft');
    await modal.getByRole('button',{name:'保存记录',exact:true}).click();await modal.waitFor({state:'hidden'});await page.unroute(path);check(attempts===2 && keys[0]===keys[1],'retry identity changed');
    let account=await admin.request('GET',`/wealth/accounts/${accountId}`);const position=account.positions[0];check(account.positions.length===1 && account.total_value===null,'missing NAV or duplicate opening');check(await bank()==='5000','opening debited cash');
    await page.getByText('估值不完整',{exact:true}).waitFor();await page.getByRole('button',{name:/详\s*情$/}).click();await page.waitForURL(/\/wealth-positions\/\d+$/);
    await page.getByRole('tab',{name:'净值与本人补录',exact:true}).click();await page.getByRole('button',{name:'补录缺失净值',exact:true}).click();modal=page.getByRole('dialog',{name:'补录缺失净值',exact:true});
    await chooseDate(page,modal.getByLabel('真实净值日期',{exact:true}),openingDay);await modal.getByLabel('单位净值（不填累计净值）',{exact:true}).fill('1.2');await modal.getByLabel('来源说明',{exact:true}).fill('银行持仓详情核对');await modal.getByRole('button',{name:'保存补录',exact:true}).click();await modal.waitFor({state:'hidden'});
    let value=await admin.request('GET',`/wealth/positions/${position.id}`);check(value.market_value==='120' && value.quote.source==='manual','manual NAV not applied');
    const second=await createAccount(admin,prefix+'第二账户');await admin.request('POST',`/wealth/accounts/${second.id}/positions`,{request_key:randomUUID(),code:'J07241',shares:'100',cost_price:'1.2',effective_date:openingDay});
    check((await admin.request('GET',`/wealth/accounts/${second.id}`)).total_value==='120','own accounts do not share overlay');
    const viewerAccount=await createAccount(viewer,prefix+'只读');const viewerOpening=await viewer.request('POST',`/wealth/accounts/${viewerAccount.id}/positions`,{request_key:randomUUID(),code:'J07241',shares:'100',cost_price:'1',effective_date:openingDay});
    check((await viewer.request('GET',`/wealth/positions/${viewerOpening.position_id}`)).market_value===null,'other owner inherited manual price');await admin.request('GET',`/wealth/accounts/${viewerAccount.id}`,undefined,403);
    await viewer.request('POST',`/wealth/positions/${viewerOpening.position_id}/events`,{request_key:randomUUID(),kind:'redeem',shares:'1',amount:'1',effective_date:confirmDay},403);
    await fixture('wealth-quote',{date:openingDay,nav:'1.2'});await admin.request('POST',`/wealth/accounts/${accountId}/refresh`);await page.reload();await page.getByRole('tab',{name:'净值与本人补录',exact:true}).click();await page.getByText('自动值已优先采用',{exact:true}).waitFor();
    value=await admin.request('GET',`/wealth/positions/${position.id}`);check(value.quote.source==='automatic' && value.quote.revision===1,'automatic first revision did not replace manual first revision');
    const manuals=await admin.request('GET','/wealth/products/1/manual-navs');await admin.request('POST','/wealth/products/1/manual-navs',{request_key:randomUUID(),nav_date:openingDay,unit_nav:'1.3',expected_revision:manuals[0].revision,source_note:'覆盖自动'},409);
    await page.goto(base+`/wealth-positions/${position.id}`);await page.getByRole('button',{name:/^申\s*购$/}).click();modal=page.getByRole('dialog',{name:'申购',exact:true});
    await modal.getByLabel('实际确认份额',{exact:true}).fill('100');await modal.getByLabel('实际总扣款（含费用）',{exact:true}).fill('110');await modal.getByLabel('实际成交净值',{exact:true}).fill('1.1');
    await chooseDate(page,modal.getByLabel('份额确认日期',{exact:true}),confirmDay);await chooseDate(page,modal.getByLabel('实际付款日期',{exact:true}),cashDay);await chooseOption(page,modal,prefix+'现金');
    await fixture('wealth-quote',{failure:true});await modal.getByRole('button',{name:'刷新参考净值',exact:true}).click();await page.getByText('银行理财净值查询失败，保留上次有效数据',{exact:true}).first().waitFor();check(await modal.getByLabel('实际总扣款（含费用）',{exact:true}).inputValue()==='110','failed quote refresh discarded draft');
    await modal.getByRole('button',{name:'保存记录',exact:true}).click();await modal.waitFor({state:'hidden'});check(await bank()==='4890','purchase cash incorrect');value=await admin.request('GET',`/wealth/positions/${position.id}`);check(value.shares==='200' && value.cost==='230','weighted cost incorrect');
    const curve=await admin.request('GET',`/stats/assets?start_date=${openingDay}&end_date=${confirmDay}`);check(curve.points.find((p)=>p.date===cashDay).accounts.find((a)=>a.account_id===accountId).balance==='230','pending buy principal lost in history');
    await fixture('wealth-quote',{date:confirmDay,nav:'1.3'});await admin.request('POST',`/wealth/accounts/${accountId}/refresh`);
    await page.getByRole('button',{name:/赎\s*回$/}).click();modal=page.getByRole('dialog',{name:'赎回',exact:true});await modal.getByLabel('实际赎回份额',{exact:true}).fill('100');await modal.getByLabel('赎回成交毛额（未扣费用）',{exact:true}).fill('130');await modal.getByLabel('实际费用（已含在扣款或净额）',{exact:true}).fill('2');await chooseDate(page,modal.getByLabel('份额确认日期',{exact:true}),rightsDay);await modal.getByRole('button',{name:'保存记录',exact:true}).click();await modal.waitFor({state:'hidden'});
    value=await admin.request('GET',`/wealth/positions/${position.id}`);check(value.pending_funds==='128' && value.realized_pnl==='13' && value.cost==='115','redemption calculation incorrect');
    await page.getByRole('button',{name:'登记到账',exact:true}).click();modal=page.getByRole('dialog',{name:'登记到账',exact:true});await chooseDate(page,modal.getByLabel('实际到账日期',{exact:true}),paidDay);await chooseOption(page,modal,prefix+'现金');await modal.getByRole('button',{name:'保存记录',exact:true}).click();await modal.waitFor({state:'hidden'});check(await bank()==='5018','settlement cash incorrect');
    const events=await admin.request('GET',`/wealth/positions/${position.id}/events`);const sale=events.find((e)=>e.kind==='redeem');const settlement=events.find((e)=>e.kind==='settlement');await admin.request('POST',`/wealth/positions/${position.id}/events/${sale.id}/undo`,{request_key:randomUUID()},409);
    await page.getByRole('button',{name:'撤销末笔',exact:true}).click();await page.getByRole('button',{name:/确\s*定/}).click();await until(()=>bank(),(v)=>v==='4890','settlement undo');
    await admin.request('POST',`/wealth/positions/${position.id}/events/${sale.id}/undo`,{request_key:randomUUID()});
    check((await admin.request('GET',`/wealth/positions/${position.id}`)).shares==='200','sale undo failed');
    const flows=await admin.request('GET',`/transactions?account_id=${accountId}&page_size=50`);check(flows.items.length===1 && flows.items[0].wealth_code==='J07241' && flows.items[0].expense_effect==='0','flow scope, code or spending effect incorrect');await admin.request('DELETE',`/transactions/${flows.items[0].id}`,undefined,409);
    await admin.request('POST','/transactions',{kind:'transfer',amount:'1',account_id:cash.id,transfer_account_id:accountId,entry_date:date()},409);await admin.request('POST','/transactions',{kind:'transfer',amount:'1',account_id:cash.id,transfer_account_id:cash.id,entry_date:date(),wealth_event_id:settlement.id},400);
    const csv=await fetch(base+`/api/v1/transactions/export?account_id=${accountId}`,{headers:{authorization:'Bearer '+fixtureToken(base,admin.username)}});const text=await csv.text();check(csv.status===200 && text.includes('J07241') && text.includes('理财申购付款') && text.trim().split('\n').length===flows.items.length+1,'CSV/list mismatch');
    await page.reload();await page.getByRole('button',{name:'现金分红',exact:true}).click();modal=page.getByRole('dialog',{name:'现金分红',exact:true});await modal.getByLabel('实际分红金额',{exact:true}).fill('100');await chooseDate(page,modal.getByLabel('权益生效日期',{exact:true}),rightsDay);await modal.getByRole('button',{name:'保存记录',exact:true}).click();await modal.waitFor({state:'hidden'});value=await admin.request('GET',`/wealth/positions/${position.id}`);check(value.dividends==='100' && value.pending_funds==='100' && value.cost==='230','dividend cost or receivable incorrect');
    await page.getByRole('button',{name:'红利再投资',exact:true}).click();modal=page.getByRole('dialog',{name:'红利再投资',exact:true});await modal.getByLabel('实际分红金额',{exact:true}).fill('100');await modal.getByLabel('实际确认份额',{exact:true}).fill('100');await chooseDate(page,modal.getByLabel('权益生效日期',{exact:true}),paidDay);await chooseDate(page,modal.getByLabel('份额确认日期',{exact:true}),paidDay);await modal.getByRole('button',{name:'保存记录',exact:true}).click();await modal.waitFor({state:'hidden'});value=await admin.request('GET',`/wealth/positions/${position.id}`);check(value.shares==='300' && value.cost==='330' && value.dividends==='200' && await bank()==='4890','reinvestment duplicated cash or dividend');
    await page.goto(base+'/');await page.locator('.recent-row').filter({hasText:'J07241'}).first().click();await page.waitForURL(/\/wealth-positions\/\d+\?event=/);await page.locator('.wealth-event-row.is-located').waitFor();check(true,'recent wealth transfer navigates to related event');await page.locator('.ant-message-notice').first().waitFor({state:'hidden'});await shot(page,mobileMain?'wealth-main-mobile-position':'wealth-desktop-position');
    const mobile=await browser.newContext({viewport:{width:360,height:900},isMobile:true,hasTouch:true});const phone=await mobile.newPage();phone.setDefaultTimeout(12000);await login(phone,base,'voucher-viewer');await phone.goto(base+`/wealth-positions/${viewerOpening.position_id}`);
    check(await phone.getByRole('button',{name:/^申\s*购$/}).count()===0,'viewer can see money write');await phone.getByRole('tab',{name:'净值与本人补录',exact:true}).click();await phone.getByRole('button',{name:'补录缺失净值',exact:true}).click();const manualModal=phone.getByRole('dialog',{name:'补录缺失净值',exact:true});await manualModal.getByLabel('单位净值（不填累计净值）',{exact:true}).fill('1.31');await manualModal.getByLabel('来源说明',{exact:true}).fill('本人手机银行核对');await manualModal.getByRole('button',{name:'保存补录',exact:true}).click();await manualModal.waitFor({state:'hidden'});
    value=await viewer.request('GET',`/wealth/positions/${viewerOpening.position_id}`);check(value.quote.source==='manual' && value.market_value==='131','viewer manual overlay failed');
    check(await phone.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'360px page overflow');await phone.locator('.ant-message-notice').first().waitFor({state:'hidden'});await shot(phone,'wealth-mobile-readonly');
    const editable=(await viewer.request('GET','/wealth/products/1/manual-navs'))[0];await phone.getByRole('button',{name:/更\s*正$/}).click();const stale=phone.getByRole('dialog',{name:'更正本人净值补录',exact:true});
    await viewer.request('POST','/wealth/products/1/manual-navs',{request_key:randomUUID(),nav_date:date(),unit_nav:'1.33',source_note:'另一页面已核对更正',expected_revision:editable.revision});
    await stale.getByLabel('单位净值（不填累计净值）',{exact:true}).fill('1.32');await stale.getByLabel('来源及更正原因',{exact:true}).fill('旧页面提交');await stale.getByRole('button',{name:'保存补录',exact:true}).click();await phone.getByText('理财请求已使用、版本过期或记录已有依赖，请刷新核对',{exact:true}).waitFor();check(await stale.getByLabel('单位净值（不填累计净值）',{exact:true}).inputValue()==='1.32','version conflict discarded draft');await stale.getByRole('button',{name:/取\s*消/}).click();await phone.reload();await phone.getByRole('tab',{name:'净值与本人补录',exact:true}).click();
    const manualRecord=(await viewer.request('GET','/wealth/products/1/manual-navs'))[0];await phone.getByRole('button',{name:'撤回补录',exact:true}).click();const withdraw=phone.getByRole('dialog',{name:'撤回本人净值补录',exact:true});await withdraw.getByLabel('撤回原因',{exact:true}).fill('日期核对后撤回');await withdraw.getByRole('button',{name:'确认撤回',exact:true}).click();await withdraw.waitFor({state:'hidden'});check(manualRecord.effective,'saved personal quote not marked effective');
    check((await viewer.request('GET',`/wealth/positions/${viewerOpening.position_id}`)).quote.source==='automatic','withdraw did not fall back to old automatic NAV');
    await mobile.close();
    // Hold a committed personal quote response across an SPA logout/login.
    await page.setViewportSize({width:1280,height:900});
    await page.getByRole('tab',{name:'净值与本人补录',exact:true}).click();await page.getByRole('button',{name:'补录缺失净值',exact:true}).click();
    const late=page.getByRole('dialog',{name:'补录缺失净值',exact:true});await late.getByLabel('单位净值（不填累计净值）',{exact:true}).fill('1.4');await late.getByLabel('来源说明',{exact:true}).fill('甲账号迟到回执测试');
    let ready=false,release;const gate=new Promise((resolve)=>{release=resolve;});
    await page.route('**/api/v1/wealth/products/1/manual-navs',async(route)=>{if(route.request().method()!=='POST')return route.continue();const response=await route.fetch();ready=true;await gate;await route.fulfill({response});});
    await late.getByRole('button',{name:'保存补录',exact:true}).click();await until(async()=>ready,Boolean,'held manual receipt');await late.locator('.ant-modal-close').click();
    await page.getByRole('menuitem',{name:/我的/}).click();await page.getByRole('button',{name:/退出所有设备/}).click();await page.waitForURL(/\/login$/);await page.getByPlaceholder('用户名').fill('voucher-viewer');await page.getByPlaceholder('密码').fill('voucher-test-only');await page.getByRole('button',{name:/登\s*录/}).click();await page.waitForURL((url)=>!url.pathname.endsWith('/login'));release();
    await page.goto(base+'/wealth');await page.getByRole('button',{name:prefix+'只读',exact:true}).waitFor();check(await page.getByText(prefix,{exact:true}).count()===0,'late owner result populated next login');
    check((await viewer.request('GET',`/wealth/positions/${viewerOpening.position_id}`)).quote.source==='automatic','late manual value leaked to next owner');
    check(errors.length===0,errors.join('\n'));
    await context.close();
  }catch(error){await shot(page,'wealth-failure');throw error;}
},'chromium');
console.log(JSON.stringify({assertions,status:'passed',source:'isolated simulation; not real-time admission evidence'}));
