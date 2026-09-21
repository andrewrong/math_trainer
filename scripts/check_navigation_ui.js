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
assert(script.includes('showChineseModule(page'));
assert(html.includes('<h2>Piano</h2>'));
assert(script.includes('function initPianoModule'));
assert(script.includes("showSection('piano')"));

console.log('navigation ui checks ok');
