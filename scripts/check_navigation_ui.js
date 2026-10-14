const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*)<\/script>/)[1];

new Function(script);

assert(html.includes('id="sec-home"'));
assert(html.includes('class="nav-shell"'));
assert(script.includes('function navigate('));
assert(script.includes('function applyRoute('));
assert(script.includes('function parseRoute('));
assert(script.includes("window.addEventListener('hashchange'"));
assert(html.includes('#/chinese/core'));
assert(html.includes("onclick=\"go('division',this)\""));
assert(script.includes("const mathIds=['add','mul','division'"));
assert(html.includes("#/math/foundations"));
assert(html.includes('id="sec-foundations"'));
assert(script.includes('function initFoundations'));
assert(script.includes("foundations:'Math foundations'"));
assert(script.includes('function submitFoundationAnswer'));
assert(script.includes('Work it out as you would in your workbook'));
assert(script.includes(".map((q,i)=>({...q,key:`${key}-${foundationSt.mode}-${i}`}))"));
assert(script.includes('showChineseModule(page'));
assert(html.includes('<h2>Piano</h2>'));
assert(script.includes('function initPianoModule'));
assert(script.includes("showSection('piano')"));

console.log('navigation ui checks ok');
