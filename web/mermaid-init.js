(function () {
  "use strict";

  function showSource(pre, error) {
    pre.classList.add("mermaid-error");
    pre.title = "Mermaid could not render this diagram; showing the source instead.";
    console.warn("Mermaid rendering failed; showing source:", error);
  }

  function showAllSources(error) {
    var blocks = document.querySelectorAll("pre > code.language-mermaid");
    for (var i = 0; i < blocks.length; i += 1) {
      showSource(blocks[i].parentElement, error);
    }
  }

  var renderingStarted = false;

  async function renderMermaidBlocks() {
    if (renderingStarted) {
      return;
    }
    renderingStarted = true;

    var blocks = Array.from(
      document.querySelectorAll("pre > code.language-mermaid")
    );

    if (blocks.length === 0) {
      return;
    }

    if (typeof window.mermaid === "undefined") {
      showAllSources(new Error("Mermaid failed to load"));
      return;
    }

    window.mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      suppressErrorRendering: true,
      theme: window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "default"
    });

    for (var i = 0; i < blocks.length; i += 1) {
      var code = blocks[i];
      var pre = code.parentElement;
      var source = code.textContent;

      try {
        var isValid = await window.mermaid.parse(source, {
          suppressErrors: true
        });
        if (!isValid) {
          throw new Error("Invalid Mermaid syntax");
        }

        var result = await window.mermaid.render(
          "mermaid-diagram-" + Date.now() + "-" + i,
          source
        );
        var container = document.createElement("div");
        container.className = "mermaid";
        container.innerHTML = result.svg;
        pre.replaceWith(container);
        if (result.bindFunctions) {
          result.bindFunctions(container);
        }
      } catch (error) {
        showSource(pre, error);
      }
    }
  }

  function startWhenLibraryIsReady() {
    if (window.__mermaidLibraryState === "ready") {
      renderMermaidBlocks();
      return;
    }
    if (window.__mermaidLibraryState === "failed") {
      showAllSources(new Error("Mermaid failed to load"));
      return;
    }

    document.addEventListener("mermaid-library-ready", renderMermaidBlocks, {
      once: true
    });
    document.addEventListener(
      "mermaid-library-failed",
      function () {
        showAllSources(new Error("Mermaid failed to load"));
      },
      { once: true }
    );
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", startWhenLibraryIsReady, {
      once: true
    });
  } else {
    startWhenLibraryIsReady();
  }
})();
