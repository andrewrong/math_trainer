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
  'function chineseSyllablePath(){}',
  extractFunction('pinyinToAudioKey'),
  extractFunction('pinyinBare'),
  extractFunction('splitPinyin'),
  extractFunction('pinyinMatchesGroup'),
  `
assert.strictEqual(pinyinToAudioKey('běi'), 'bei3');
assert.strictEqual(pinyinMatchesGroup('běi', 'compound-finals'), true);
assert.strictEqual(pinyinMatchesGroup('zhōng', 'retroflex'), true);
assert.strictEqual(pinyinMatchesGroup('shàng', 'nasal'), true);
assert.strictEqual(pinyinMatchesGroup('bà', 'common-initials'), true);
assert.strictEqual(pinyinMatchesGroup('yú', 'whole'), true);
`,
].join('\n');

new Function('assert', source)(assert);
console.log('chinese pinyin checks ok');
