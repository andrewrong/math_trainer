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
assert(html.includes('#/math/division'));
assert(script.includes('showChineseModule(page'));

console.log('navigation ui checks ok');
