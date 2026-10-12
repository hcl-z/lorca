# Questions with options

A bot asks the user to choose with `ask_user` (`crates/cli/src/ask.rs`): a question, two to six options with an optional one-line description each, `multiple` for several picks, `allow_other` for a line of the user's own words, and `recommended`, the place of the option the bot suggests. A turn nobody watches (a routine, a workflow's sample) does not get to ask; the tool tells the bot to decide and say what it chose.

## The card and the wait

The question is a permission card (`Body::Permission`) with `tool` `ask`, the question as its `summary`, and an `ask` of `AskQuestion { question, options[{ label, description? }], multiple?, allow_other?, recommended?, answer? }`. The call waits as every card does (`mcp::await_answer`): ten minutes at most, Stop ends it, and a message the user writes instead dismisses it, so the bot follows that message. A pending question is pushed to the phones with the question as its text.

## The answer

The apps answer through `chats.permission { chat_id, message_id, decision, answer? }`, which goes to the bot's Runner here or sealed, as any card's answer does. `decision` `allow` carries `answer { choices: [place], other? }`; `deny` skips the question. On the Runner `ask::answer` checks it against the card: picks within the options, one pick unless `multiple`, the user's own words only with `allow_other` and, for one pick, instead of an option. A wrong answer is refused and the card keeps waiting. A good one is kept on the card as `ask.answer`, so every Device shows what was picked, then the waiting call returns it: "The user answered: "Thorough"", with the answer in its details.

| Outcome | Card `decision` | The bot hears |
| --- | --- | --- |
| Picked | `allowed`, with `ask.answer` | the labels picked and the user's words |
| Skip | `denied` | to go on with what it thinks best and say what it chose |
| Stop | `dismissed` | that the turn was stopped |
| A new message | `dismissed` | to follow that message; the turn ends after this batch |
| Ten minutes | `expired` | to say what it needs decided and stop |

## In the apps

- **Desktop** (`desktop/ask_card.go`, `model/ask.go`): each option is a bordered row with a radio for one pick or a checkbox for several, its description under it, Recommended on the bot's pick, and its number, which 1 to 9 pick while the options have the keyboard. One pick sends on its click; several, or Other, which opens a field, send with Submit. Skip leaves the choice to the bot. The card reads the answer at once (`Store.AnswerAsk`) and goes back to asking, with why under it, when the Runner refuses or cannot be reached. Answered, it keeps the question and says "You chose …", or Skipped, No answer in time, or Answered in the chat instead.
- **macOS and the phone** list the options under the question, numbered, ask for the choice as a reply in the chat, and offer Skip; an answer given on a computer reads "You chose …" there.
