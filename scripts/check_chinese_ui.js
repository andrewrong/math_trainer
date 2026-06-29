const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*)<\/script>/)[1];

new Function(script);

assert(script.includes('function toggleChineseSetCards'));
assert(script.includes('I am not sure'));
assert(script.includes('speakChineseSentence'));
assert(script.includes('chineseMeaningPrompt'));
assert(script.includes('chineseAudioPathForPinyinKey'));
assert(script.includes('/audio/chinese-syllables/'));
assert(script.includes('CHINESE_CORE_100'));
assert(html.includes("#/chinese/core"));
assert(script.includes('function renderChineseCore'));
assert(html.includes('zh-core-rest'));
assert(script.includes('function startChineseCoreQuiz'));

console.log('chinese ui checks ok');
