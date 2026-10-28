// The snap points and action order shared by the phone's bottom sheets. Android has two states
// (about half and full); a longer list uses the first and the last.

export const SHEET_PRESETS: {
  participants: (string | number)[];
  attach: (string | number)[];
  actions: (string | number)[];
  picker: (string | number)[];
  form: (string | number)[];
} = {
  participants: ["60%", "92%"],
  attach: ["35%"],
  actions: ["40%"],
  picker: ["90%", "100%"],
  form: ["50%", "100%"],
};

/// Destructive actions sit last, where a thumb is least likely to catch them on the way to a
/// common one.
export function orderActions<T extends { destructive?: boolean }>(actions: T[]): T[] {
  return [...actions].sort((a, b) => Number(!!a.destructive) - Number(!!b.destructive));
}
