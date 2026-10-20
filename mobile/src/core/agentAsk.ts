/// What a coding agent asks the user, on a card of its own in the chat (`tool` = `agent_ask`): the
/// agent's message and a form, field by field. Its answer goes to the agent through the
/// `coding_agent` row that started it (`coding.answer` with `values` by field key, or `decline`).
/// Answered, the card keeps the form and says who answered and what.

export interface AgentAsk {
  agent_id: string;
  kind: string;
  name: string;
  /// The `coding_agent` row whose agent asks, which the answer names.
  row_id: string;
  message: string;
  fields: AgentAskField[];
  /// Once answered: each field's answer in words, in the form's order.
  answers?: { name: string; value: string }[];
  /// Who answered or declined it.
  by?: "you" | "bot";
}

export interface AgentAskField {
  key: string;
  label: string;
  kind: "one" | "many" | "flag" | "text" | "number" | "integer";
  required?: boolean;
  choices?: { value: string; title: string; description?: string }[];
  /// A pick may be the user's own words.
  other?: boolean;
}

/// What the user has filled in so far: the picks of each field by their place, Other's words by
/// field, and what was typed.
export interface AgentAskDraft {
  picks: Record<string, number[]>;
  other: Record<string, string | undefined>;
  words: Record<string, string>;
}

export const emptyDraft = (): AgentAskDraft => ({ picks: {}, other: {}, words: {} });

/// What a field offers to pick: its choices, or Yes and No.
export function fieldChoices(field: AgentAskField, yes: string, no: string): { value: string; title: string; description?: string }[] {
  if (field.kind === "flag") return [{ value: "yes", title: yes }, { value: "no", title: no }];
  return field.kind === "one" || field.kind === "many" ? (field.choices ?? []) : [];
}

/// A form that asks for one pick sends on its tap.
export function sendsOnPick(ask: AgentAsk, draft: AgentAskDraft): boolean {
  const [field] = ask.fields;
  return ask.fields.length === 1 && (field.kind === "one" || field.kind === "flag") && draft.other[field.key] === undefined;
}

/// The picks of a field after a tap on its choice `index`: one, or several for `many`.
export function togglePick(field: AgentAskField, picks: number[], index: number): number[] {
  if (field.kind !== "many") return [index];
  return picks.includes(index) ? picks.filter((at) => at !== index) : [...picks, index].sort((a, b) => a - b);
}

/// The form as it is filled in: the values the agent is sent, by field key; the answer in words, by
/// field label; and whether every required field has one.
export function formValues(ask: AgentAsk, draft: AgentAskDraft, yes: string, no: string, separator: string): { values: Record<string, unknown>; answers: { name: string; value: string }[]; ready: boolean } {
  const values: Record<string, unknown> = {};
  const answers: { name: string; value: string }[] = [];
  let ready = true;
  for (const field of ask.fields) {
    const choices = fieldChoices(field, yes, no);
    const picked = (draft.picks[field.key] ?? []).map((at) => choices[at]).filter(Boolean);
    const own = field.other ? (draft.other[field.key] ?? "").trim() : "";
    const words = (draft.words[field.key] ?? "").trim();
    let shown = "";
    switch (field.kind) {
      case "one":
        if (own) [values[field.key], shown] = [own, own];
        else if (picked[0]) [values[field.key], shown] = [picked[0].value, picked[0].title];
        break;
      case "many": {
        const list = [...picked.map((choice) => choice.value), ...(own ? [own] : [])];
        if (list.length > 0) [values[field.key], shown] = [list, [...picked.map((choice) => choice.title), ...(own ? [own] : [])].join(separator)];
        break;
      }
      case "flag":
        if (picked[0]) [values[field.key], shown] = [picked[0].value === "yes", picked[0].title];
        break;
      default:
        if (words) [values[field.key], shown] = [words, words];
    }
    if (shown) answers.push({ name: field.label, value: shown });
    else if (field.required) ready = false;
  }
  return { values, answers, ready: ready && Object.keys(values).length > 0 };
}
