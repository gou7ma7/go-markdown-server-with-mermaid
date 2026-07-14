(function () {
  "use strict";

  var modernPath = "/assets/mermaid-11.16.0.min.js";
  var legacyPath = "/assets/mermaid-10.9.6.min.js";
  var modernVersion = "11.16.0";
  var legacyVersion = "10.9.6";

  function emit(name) {
    var event;
    if (typeof window.CustomEvent === "function") {
      event = new window.CustomEvent(name);
    } else if (document.createEvent) {
      event = document.createEvent("Event");
      event.initEvent(name, false, false);
    }
    if (event) {
      document.dispatchEvent(event);
    }
  }

  function setState(state, version, compatibilityMode) {
    window.__mermaidLibraryState = state;
    window.__mermaidRuntime = {
      state: state,
      version: version,
      compatibilityMode: compatibilityMode
    };
    emit("mermaid-library-" + state);
  }

  function installLegacyPolyfills() {
    if (typeof window.structuredClone !== "function") {
      window.structuredClone = function (value) {
        if (typeof value === "undefined") {
          return value;
        }
        return JSON.parse(JSON.stringify(value));
      };
    }

    if (typeof Object.hasOwn !== "function") {
      Object.hasOwn = function (object, property) {
        return Object.prototype.hasOwnProperty.call(Object(object), property);
      };
    }

    if (typeof String.prototype.replaceAll !== "function") {
      String.prototype.replaceAll = function (searchValue, replaceValue) {
        var value = String(this);
        if (searchValue instanceof RegExp) {
          if (!searchValue.global) {
            throw new TypeError("replaceAll requires a global regular expression");
          }
          return value.replace(searchValue, replaceValue);
        }
        return value.split(String(searchValue)).join(String(replaceValue));
      };
    }
  }

  function supportsModernMermaid() {
    return (
      window.__mermaidModernSyntax === true &&
      typeof window.structuredClone === "function" &&
      typeof Object.hasOwn === "function" &&
      typeof String.prototype.replaceAll === "function"
    );
  }

  function loadScript(path, version, compatibilityMode, allowLegacyRetry) {
    var script = document.createElement("script");
    script.src = path;

    script.onload = function () {
      if (typeof window.mermaid !== "undefined") {
        setState("ready", version, compatibilityMode);
        return;
      }

      // A newer engine can occasionally parse the probe but still reject a
      // Mermaid 11 dependency. The lower-version bundle is a safe retry.
      if (allowLegacyRetry) {
        installLegacyPolyfills();
        loadScript(legacyPath, legacyVersion, true, false);
        return;
      }
      setState("failed", version, compatibilityMode);
    };

    script.onerror = function () {
      setState("failed", version, compatibilityMode);
    };

    (document.head || document.getElementsByTagName("head")[0] || document.body).appendChild(script);
  }

  if (supportsModernMermaid()) {
    loadScript(modernPath, modernVersion, false, true);
    return;
  }

  installLegacyPolyfills();
  loadScript(legacyPath, legacyVersion, true, false);
})();
