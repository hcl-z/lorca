// The curves the app's motion uses. Reanimated's built-ins start slow or ease in, which delays the
// frame the user is watching; these are a strong ease-out for anything entering or leaving, and an
// ease-in-out for something moving while on screen.

import { Easing } from "react-native-reanimated";

export const EASE_OUT = Easing.bezier(0.23, 1, 0.32, 1);
export const EASE_IN_OUT = Easing.bezier(0.77, 0, 0.175, 1);
