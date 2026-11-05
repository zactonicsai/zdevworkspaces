/* Tailwind config for the single-file simulation page (portal/web/simulation.html).
   `npm run build:sim` compiles only the utilities that page uses and inlines them. */
const base = require("./tailwind.config.js");
module.exports = { ...base, content: ["./web/simulation.html"], safelist: ["hidden", "dark"] };
