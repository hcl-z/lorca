import { describe, expect, test } from "bun:test";
import { SHEET_PRESETS, orderActions } from "./sheetPresets";

describe("sheets", () => {
  test("sheet presets open at a usable detent", () => {
    expect(SHEET_PRESETS.participants).toEqual(["60%", "92%"]);
    expect(SHEET_PRESETS.attach).toEqual(["35%"]);
    expect(SHEET_PRESETS.actions).toEqual(["40%"]);
    expect(SHEET_PRESETS.picker).toEqual(["90%", "100%"]);
    expect(SHEET_PRESETS.form).toEqual(["50%", "100%"]);
  });

  test("an action sheet keeps destructive actions last", () => {
    expect(orderActions([{ label: "Delete", destructive: true }, { label: "Copy" }, { label: "Save" }]).map((action) => action.label)).toEqual([
      "Copy",
      "Save",
      "Delete",
    ]);
  });
});
