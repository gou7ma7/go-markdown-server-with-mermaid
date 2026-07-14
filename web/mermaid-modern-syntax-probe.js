(function () {
  "use strict";

  // Mermaid 11.16 uses logical assignment. Keeping that syntax in a separate
  // classic script lets an older WebView fail this probe without preventing
  // the small ES5-compatible loader that follows it from running.
  var supported = false;
  supported ||= true;
  window.__mermaidModernSyntax = supported;
})();
