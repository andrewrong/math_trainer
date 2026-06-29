const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*)<\/script>/)[1];

new Function(script);

assert(script.includes('function renderPianoStaff'));
assert(script.includes('function renderPianoKeyboard'));
assert(!script.includes('middle C below treble staff'));
assert(!script.includes('Which note is highlighted on the keyboard?'));
assert(!script.includes('k===q.key'));
assert(script.includes("letter:'C'"));
assert(script.includes("note:'E4',letter:'E',octave:4,staffStep:0"));
assert(script.includes("note:'C5',letter:'C',octave:5,staffStep:5"));
assert(script.includes("note:'C3',letter:'C',octave:3,staffStep:3"));
assert(script.includes("note:'F3',letter:'F',octave:3,staffStep:6"));
assert(script.includes('function renderPianoRhythm'));
assert(!script.includes('q.symbol'));
assert(script.includes("shape:'quarter'"));
assert(script.includes("shape:'half-rest'"));

console.log('piano ui checks ok');
