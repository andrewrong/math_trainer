const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const handler = fs.readFileSync('internal/handler/english.go', 'utf8');
const migration = fs.readFileSync('internal/db/migrations/013_english_friday_spelling.sql', 'utf8');

assert(html.includes('Friday paper spelling check'), 'parents need a paper-check entry point');
assert(html.includes('Review Friday mistakes ('), 'learners need a focused Friday mistake review entry point');
assert(html.includes("'/friday-spelling'"), 'the Friday paper-check UI must persist its selected mistakes');
assert(html.includes('startFridayMistakePractice'), 'Friday mistakes must start a focused spelling practice');
assert(handler.includes('updateFridaySpelling'), 'the API must accept Friday paper-test evidence');
assert(handler.includes('loadFridayMistakes'), 'weeks must return their saved Friday mistakes');
assert(migration.includes('english_friday_spelling_mistakes'), 'Friday paper-test mistakes need durable local storage');
console.log('Friday spelling checks ok');
