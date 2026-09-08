const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*)<\/script>/)[1];

function extractFunction(name) {
  const start = script.indexOf(`function ${name}`);
  assert(start >= 0, name);
  const bodyStart = script.indexOf('{', start);
  let depth = 0;
  for (let i = bodyStart; i < script.length; i++) {
    if (script[i] === '{') depth++;
    if (script[i] === '}') depth--;
    if (depth === 0) return script.slice(start, i + 1);
  }
  throw new Error(`unclosed function ${name}`);
}

const source = [
  'function fmtNum(n){return String(n)}',
  'function sshuffle(a){return a}',
  'function sri(){return 7}',
  extractFunction('dailyNumberOptions'),
  extractFunction('fmtMoneyCents'),
  extractFunction('improveMCQ'),
  `
const rng = () => 0.7;
const q = improveMCQ('Word Problem', 'A toy costs $6.87. Maya pays $10.00. How much change should she get?', 'Change means paid minus cost.', 313, '$10.00 - $6.87 = <b>$3.13</b>.', rng, 'transfer', fmtMoneyCents);
assert.strictEqual(q.correct, '$3.13');
assert(q.options.includes('$3.13'));
assert(q.options.every(x => /^\\$\\d+\\.\\d{2}$/.test(x)), q.options.join(','));
assert.strictEqual(new Set(q.options).size, q.options.length);
`,
].join('\n');

new Function('assert', source)(assert);
console.log('improve money checks ok');
