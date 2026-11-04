// Text and fields that follow the system's text size up to 1.2 times, so a very large setting
// does not burst the fixed rows and token line heights; a field's caret and selection in the
// caret color.

import type { Ref } from "react";
import { Text as RNText, TextInput as RNTextInput, type TextInputProps, type TextProps } from "react-native";
import { useTheme } from "./ThemeProvider";

export const MAX_FONT_SIZE_MULTIPLIER = 1.2;

export function Text({ ref, ...props }: TextProps & { ref?: Ref<RNText> }) {
  return <RNText ref={ref} maxFontSizeMultiplier={MAX_FONT_SIZE_MULTIPLIER} {...props} />;
}

export function TextInput({ ref, ...props }: TextInputProps & { ref?: Ref<RNTextInput> }) {
  const { colors } = useTheme();
  return <RNTextInput ref={ref} cursorColor={colors.inputCaret} selectionColor={colors.inputCaret} maxFontSizeMultiplier={MAX_FONT_SIZE_MULTIPLIER} {...props} />;
}
