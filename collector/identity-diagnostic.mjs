// Public product text only; never prints URLs with queries, headers or cookies.
import { collect,launch,disposeBrowser,productFor } from '/app/collector.mjs';
const browser=await launch();const product=productFor('J07241');
const fixture={newBrowserCDPSession:()=>browser.newBrowserCDPSession(),close:()=>disposeBrowser(browser),async newContext(options){
 const context=await browser.newContext(options);const newPage=context.newPage.bind(context);
 context.newPage=async()=>{const page=await newPage();const locator=page.locator.bind(page);page.locator=(selector,...args)=>{
  const target=locator(selector,...args);if(selector!=='body')return target;
  const innerText=target.innerText.bind(target);target.innerText=async()=>{const text=await innerText();const clean=text.replace(/\s/g,'');
   const at=clean.indexOf('恒源');const registration=clean.indexOf('Z7003');
   process.stdout.write(JSON.stringify({name_present:clean.includes(product.name),registration_present:clean.includes(product.registration_code),product_excerpt:at<0?null:clean.slice(Math.max(0,at-12),at+80),registration_excerpt:registration<0?null:clean.slice(registration,registration+32)})+'\n');return text;};return target;
 };return page;};return context;
}};
try{const result=await collect(fixture,'J07241');process.stdout.write(JSON.stringify({result:'valid',count:result.response.data.length})+'\n');}
catch(error){process.stdout.write(JSON.stringify({result:'failed',code:error.code||'browser_error'})+'\n');}
finally{await disposeBrowser(browser);}
