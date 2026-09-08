const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*)<\/script>/)[1];
const start = script.indexOf('async function renderWordCards');
const end = script.indexOf('/* --- TTS --- */', start);
assert(start >= 0 && end > start, 'renderWordCards section not found');
const render = script.slice(start, end);

const firstCard = render.indexOf('c.appendChild(card)');
assert(firstCard >= 0, 'word cards must be appended');
assert(!render.includes('Pronunciation unavailable'), 'IPA lookup failure must not claim that playback is unavailable');
assert(render.includes('data-phonetic aria-live="polite" hidden'), 'word cards must keep the optional IPA field hidden until it is available');
assert(render.includes("/english/pronunciations/lookup"), 'word cards must use the local pronunciation lookup');
assert(script.includes("/english/pronunciations/audio"), 'word playback must use the local British speech endpoint');
assert(!html.includes('api.dictionaryapi.dev'), 'dictionaryapi.dev must not be a runtime dependency');
assert(script.includes('dict.youdao.com/dictvoice?type=1'), 'British Youdao audio must be the preferred word playback');
assert(script.includes('function renderParentPronunciationLibrary'), 'parent pronunciation review library must exist');
assert(script.includes('data-action="rename"'), 'week cards must offer a rename action');
assert(script.includes('function showEnglishWeekRename'), 'week rename form handler must exist');
assert(script.includes("apiFetch('PATCH','/english/weeks/"), 'week rename must persist through the rename endpoint');
assert(script.includes('data-action="edit"'), 'week cards must offer an edit words action');
assert(script.includes('function showEnglishWeekEditor'), 'week editor handler must exist');
assert(script.includes("+'/words',{words}"), 'week editor must persist edited words through the refresh endpoint');
console.log('english vocabulary UI checks ok');
