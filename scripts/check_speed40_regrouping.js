const assert = require('assert');
const fs = require('fs');

const html = fs.readFileSync('web/index.html', 'utf8');
const match = html.match(/const ADD_SPEED_REGROUPING_RATE=[\s\S]*?(?=\nfunction makeAddSpeedDeck)/);
assert(match, 'Speed 40 regrouping generator must exist');

let state = 1;
const ri = (min, max) => {
  state = (state * 1664525 + 1013904223) >>> 0;
  return min + (state % (max - min + 1));
};
const makeAddSpeedQuestion = new Function('ri', `${match[0]}; return makeAddSpeedQuestion;`)(ri);

let addition = 0;
let additionWithCarry = 0;
let subtraction = 0;
let subtractionWithBorrow = 0;
for (let i = 0; i < 2000; i++) {
  const question = makeAddSpeedQuestion();
  assert(question.ans >= 20 && question.ans <= 40, 'Speed 40 answers must remain within 20–40');
  if (question.op === '+') {
    addition++;
    if ((question.a % 10) + (question.b % 10) >= 10) additionWithCarry++;
  } else {
    subtraction++;
    if (question.a % 10 < question.b % 10) subtractionWithBorrow++;
  }
}

assert(addition > 0 && subtraction > 0, 'Speed 40 must retain both addition and subtraction');
assert(additionWithCarry / addition >= 0.7, 'Most addition questions must need carrying');
assert(subtractionWithBorrow / subtraction >= 0.7, 'Most subtraction questions must need borrowing');
console.log('Speed 40 regrouping checks ok');
