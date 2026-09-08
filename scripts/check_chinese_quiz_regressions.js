const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*)<\/script>/)[1];

function extractFunction(name) {
  const start = script.indexOf(`function ${name}`);
  assert(start >= 0, `missing ${name}`);
  const bodyStart = script.indexOf('{', start);
  let depth = 0;
  for (let i = bodyStart; i < script.length; i++) {
    if (script[i] === '{') depth++;
    if (script[i] === '}') depth--;
    if (depth === 0) return script.slice(start, i + 1);
  }
  throw new Error(`unclosed ${name}`);
}

const source = [
  'const shuffle = values => values;',
  extractFunction('isMatch'),
  extractFunction('pinyinToAudioKey'),
  extractFunction('chineseOptions'),
  `
const target = {character: '给', pinyin: 'gěi'};
const pool = [
  target,
  {character: '羊', pinyin: 'yáng'},
  {character: '姐', pinyin: 'jiě'},
  {character: '左', pinyin: 'zuǒ'},
  {character: '字', pinyin: '字'},
];
const choices = chineseOptions(pool, target, 'pinyin', target.pinyin);
assert(choices.every(choice => !/[\u3400-\u9fff]/.test(choice)), 'pinyin choices leaked Chinese characters: ' + choices.join(', '));
`,
].join('\n');

new Function('assert', source)(assert);
assert(!script.includes("btn.textContent='缺'"), 'a missing audio probe must not replace the speaker control with 缺');
console.log('chinese quiz regression checks ok');
