/* theme.js — runs before anything renders. Restores the saved theme and text
   size so the page does not flash. Everything here must work with storage
   blocked (private windows), hence the try/catch. */
(function () {
  var root = document.documentElement;
  var theme = null, size = null;
  try {
    theme = localStorage.getItem("ws.theme");
    size = parseInt(localStorage.getItem("ws.fontSize") || "", 10);
  } catch (e) { /* storage unavailable */ }
  if (!theme) theme = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  root.classList.toggle("dark", theme === "dark");
  root.setAttribute("data-theme", theme);
  if (size >= 14 && size <= 36) root.style.fontSize = size + "px";
})();
