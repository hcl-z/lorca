// Android's app theme is Material 3, so what Android draws for the app (alert dialogs, text
// selection handles and cursors, the overscroll glow) has Material 3's look rather than
// AppCompat's, in the app's monochrome colors (`src/ui/theme.ts`) rather than the wallpaper's: ink
// for the primary roles, neutral surfaces. The template's theme is AppCompat; this swaps its
// parent, adds the Material Components library it comes from (expo-router already ships it), and
// sets the color roles from a light and a night set of color resources.
const { withAndroidColors, withAndroidColorsNight, withAndroidStyles, withAppBuildGradle } = require("expo/config-plugins");

const PARENT = "Theme.Material3.DayNight.NoActionBar";
const LIBRARY = 'implementation("com.google.android.material:material:1.13.0")';

const LIGHT = { lorcaInk: "#0A0A0A", lorcaOnInk: "#FFFFFF", lorcaSurface: "#FFFFFF", lorcaOnSurface: "#0A0A0A", lorcaContainer: "#F4F4F4" };
const DARK = { lorcaInk: "#F5F5F5", lorcaOnInk: "#0A0A0A", lorcaSurface: "#0A0A0A", lorcaOnSurface: "#F2F2F2", lorcaContainer: "#1C1C1E" };

// Theme attribute → color resource.
const ROLES = {
  colorPrimary: "lorcaInk",
  colorOnPrimary: "lorcaOnInk",
  colorSecondary: "lorcaInk",
  colorOnSecondary: "lorcaOnInk",
  colorAccent: "lorcaInk",
  colorControlActivated: "lorcaInk",
  colorPrimaryContainer: "lorcaContainer",
  colorOnPrimaryContainer: "lorcaOnSurface",
  colorSurface: "lorcaSurface",
  colorOnSurface: "lorcaOnSurface",
  "android:colorBackground": "lorcaSurface",
};

function setColors(resources, values) {
  resources.color = (resources.color ?? []).filter((color) => !(color.$.name in values));
  for (const [name, value] of Object.entries(values)) resources.color.push({ $: { name }, _: value });
  return resources;
}

module.exports = function withMaterialTheme(config) {
  config = withAndroidColors(config, (config) => {
    config.modResults.resources = setColors(config.modResults.resources, LIGHT);
    return config;
  });
  config = withAndroidColorsNight(config, (config) => {
    config.modResults.resources = setColors(config.modResults.resources, DARK);
    return config;
  });
  config = withAndroidStyles(config, (config) => {
    const theme = config.modResults.resources.style?.find((style) => style.$.name === "AppTheme");
    if (!theme) throw new Error("with-material-theme: the template has no AppTheme; update the plugin");
    theme.$.parent = PARENT;
    theme.item = (theme.item ?? []).filter((item) => !(item.$.name in ROLES));
    for (const [name, color] of Object.entries(ROLES)) theme.item.push({ $: { name }, _: `@color/${color}` });
    return config;
  });
  return withAppBuildGradle(config, (config) => {
    if (!config.modResults.contents.includes("com.google.android.material:material")) {
      config.modResults.contents = config.modResults.contents.replace(/dependencies\s*\{/, (match) => `${match}\n    ${LIBRARY}`);
    }
    return config;
  });
};
