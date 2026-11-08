/* Tailwind configuration — IBM Carbon-inspired palette and type.
   Build the stylesheet with `npm run build:css` (see package.json); the output
   web/tailwind.css is committed and embedded in the Go binary, so the portal
   has no runtime dependency on a CDN and works in air-gapped clusters.
   Light theme: white background, Gray 100 text, Blue 60 actions. Dark theme
   (class="dark" on <html>): black background, Gray 10 text, Blue 50/40 actions.
   Square corners, as in Carbon. */
module.exports = {
  content: ["./web/index.html", "./web/app.js", "./web/content.js", "./web/demo.js"],
  darkMode: "class",
  theme: {
    extend: {
      fontFamily: {
        sans: ['"IBM Plex Sans"', "system-ui", "-apple-system", "Segoe UI", "Roboto", "Helvetica Neue", "Arial", "sans-serif"],
        mono: ['"IBM Plex Mono"', "ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace"],
      },
      colors: {
        blue: { 10: "#edf5ff", 20: "#d0e2ff", 30: "#a6c8ff", 40: "#78a9ff", 50: "#4589ff", 60: "#0f62fe", 70: "#0043ce", 80: "#002d9c", 90: "#001d6c", 100: "#001141" },
        gray: { 10: "#f4f4f4", 20: "#e0e0e0", 30: "#c6c6c6", 40: "#a8a8a8", 50: "#8d8d8d", 60: "#6f6f6f", 70: "#525252", 80: "#393939", 90: "#262626", 100: "#161616" },
        green: { 20: "#a7f0ba", 40: "#42be65", 50: "#24a148", 60: "#198038" },
        red: { 20: "#ffd7d9", 50: "#fa4d56", 60: "#da1e28" },
        yellow: { 20: "#fddc69", 30: "#f1c21b" },
        teal: { 50: "#009d9a" },
        purple: { 50: "#a56eff" },
      },
      borderRadius: { none: "0", DEFAULT: "0", sm: "0", md: "0", lg: "0", xl: "0", "2xl": "0", full: "9999px" },
      boxShadow: { carbon: "0 2px 6px rgba(0,0,0,.3)" },
      maxWidth: { page: "1200px" },
    },
  },
  safelist: ["hidden", "dark", { pattern: /^(toast|status|pill|callout|btn|tile|chip|tag-src|term|check-card|progress-step|step-card|step-num|modal|nav-link|icon-btn|link-btn|input|select|textarea|field|help|card|cmd|prose|tbl|spinner|skip-link|page-width)/ }],
};
