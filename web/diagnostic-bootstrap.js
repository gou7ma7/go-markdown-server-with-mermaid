(function () {
  "use strict";

  var startedAt = new Date().getTime();
  var diagnosticFinished = false;
  var reportTimer = null;
  var roleLabels = {
    unknown: "未指定设备",
    reader_builtin: "电子书自带浏览器",
    reader_via: "电子书 Via 浏览器",
    phone_via: "手机 Via 浏览器",
    desktop: "电脑浏览器"
  };
  var role = normalizeRole(readQueryValue("role"));
  var report = {
    schemaVersion: 1,
    runId: createRunID(),
    role: role,
    phase: "bootstrap",
    clientTime: clientTime(),
    userAgent: safeNavigatorValue("userAgent"),
    platform: safeNavigatorValue("platform"),
    vendor: safeNavigatorValue("vendor"),
    environment: {},
    capabilities: {},
    network: {},
    svg: {},
    mermaid: {},
    errors: []
  };

  window.__mermaidDiagnosticReport = report;
  window.__diagProbeES2015 = false;
  window.__diagProbeES2017 = false;
  window.__diagProbeES2018 = false;
  window.__diagProbeES2020 = false;
  window.__diagProbeModule = false;

  window.onerror = function (message, source, line, column) {
    addError("window_error", message, source, line, column, "");
    scheduleErrorReport();
    return false;
  };

  if (window.addEventListener) {
    window.addEventListener("unhandledrejection", function (event) {
      var reason = event && event.reason;
      var message = reason && reason.message ? reason.message : String(reason || "Unhandled promise rejection");
      addError("unhandled_rejection", message, "", 0, 0, "");
      scheduleErrorReport();
    });
  }

  onReady(startDiagnostic);

  function startDiagnostic() {
    var roleElement = document.getElementById("device-role");
    if (roleElement) {
      setText(roleElement, roleLabels[role] + "（" + role + "）");
    }

    collectEnvironment();
    testStaticSVG();
    setStep("step-bootstrap", "ok", "基础脚本已运行，runId: " + report.runId);
    setOverall("running", "正在检测，请保持页面打开");
    updateReportOutput();

    sendReport("bootstrap", function () {
      runSyntaxProbes(function () {
        runNetworkChecks(function () {
          loadAndTestMermaid();
        });
      });
    });

    window.setTimeout(function () {
      if (!diagnosticFinished) {
        report.mermaid.globalTimeout = true;
        addError("timeout", "Diagnostic did not finish within 120 seconds", "", 0, 0, "diagnostic");
        finishDiagnostic("检测超时，但已上传当前进度");
      }
    }, 120000);
  }

  function collectEnvironment() {
    var documentElement = document.documentElement || {};
    var viewportWidth = documentElement.clientWidth || window.innerWidth || 0;
    var viewportHeight = documentElement.clientHeight || window.innerHeight || 0;
    var ratio = window.devicePixelRatio || 1;
    var serviceWorkerControlled = false;

    try {
      serviceWorkerControlled = !!(navigator.serviceWorker && navigator.serviceWorker.controller);
    } catch (error) {
      serviceWorkerControlled = false;
    }

    report.environment.protocol = window.location ? window.location.protocol : "";
    report.environment.secureContext = typeof window.isSecureContext === "boolean" ? window.isSecureContext : null;
    report.environment.visibilityState = document.visibilityState || "unknown";
    report.environment.online = typeof navigator.onLine === "boolean" ? navigator.onLine : null;
    report.environment.touch = "ontouchstart" in window;
    report.environment.viewportWidthBucket = bucket(viewportWidth, 50);
    report.environment.viewportHeightBucket = bucket(viewportHeight, 50);
    report.environment.devicePixelRatioBucket = Math.round(ratio * 2) / 2;
    report.environment.serviceWorkerControlled = serviceWorkerControlled;

    report.capabilities.json = !!window.JSON;
    report.capabilities.xmlHttpRequest = typeof window.XMLHttpRequest !== "undefined";
    report.capabilities.promise = typeof window.Promise !== "undefined";
    report.capabilities.fetch = typeof window.fetch !== "undefined";
    report.capabilities.url = typeof window.URL !== "undefined";
    report.capabilities.textEncoder = typeof window.TextEncoder !== "undefined";
    report.capabilities.domParser = typeof window.DOMParser !== "undefined";
    report.capabilities.mutationObserver = typeof window.MutationObserver !== "undefined";
    report.capabilities.resizeObserver = typeof window.ResizeObserver !== "undefined";
    report.capabilities.matchMedia = typeof window.matchMedia === "function";
    report.capabilities.requestAnimationFrame = typeof window.requestAnimationFrame === "function";
    report.capabilities.objectFromEntries = typeof Object.fromEntries === "function";
    report.capabilities.promiseAllSettled = !!(window.Promise && typeof window.Promise.allSettled === "function");
    report.capabilities.structuredClone = typeof window.structuredClone === "function";
    report.capabilities.cryptoSubtle = !!(window.crypto && window.crypto.subtle);
    report.capabilities.cssSupports = !!(window.CSS && typeof window.CSS.supports === "function");
    report.capabilities.documentFonts = !!document.fonts;
    report.capabilities.elementReplaceWith = !!(window.Element && Element.prototype && typeof Element.prototype.replaceWith === "function");
  }

  function testStaticSVG() {
    var namespace = "http://www.w3.org/2000/svg";
    var probe = document.getElementById("static-svg-probe");
    var createdSVG = null;
    var foreignObject = null;
    var rect = null;

    try {
      createdSVG = document.createElementNS(namespace, "svg");
      foreignObject = document.createElementNS(namespace, "foreignObject");
      report.svg.createElementNS = !!(createdSVG && foreignObject);
      report.svg.getBBoxAPI = !!(createdSVG && typeof createdSVG.getBBox === "function");
      report.svg.xmlSerializer = typeof window.XMLSerializer !== "undefined";
      report.svg.foreignObjectAPI = !!foreignObject;
    } catch (error) {
      report.svg.createElementNS = false;
      addError("svg_probe", errorMessage(error), "", 0, 0, "createElementNS");
    }

    try {
      rect = probe && probe.getBoundingClientRect ? probe.getBoundingClientRect() : null;
      report.svg.inlineVisible = !!(rect && rect.width > 0 && rect.height > 0);
      report.svg.inlineWidth = rect ? Math.round(rect.width) : 0;
      report.svg.inlineHeight = rect ? Math.round(rect.height) : 0;
      report.svg.inlinePathCount = probe ? probe.getElementsByTagName("path").length : 0;
      report.svg.inlineTextCount = probe ? probe.getElementsByTagName("text").length : 0;
    } catch (error2) {
      report.svg.inlineVisible = false;
      addError("svg_layout", errorMessage(error2), "", 0, 0, "static-svg");
    }
  }

  function runSyntaxProbes(done) {
    var probes = [
      { name: "es2015", marker: "__diagProbeES2015", url: "/__diag/assets/probe-es2015-v1.js", type: "" },
      { name: "es2017", marker: "__diagProbeES2017", url: "/__diag/assets/probe-es2017-v1.js", type: "" },
      { name: "es2018", marker: "__diagProbeES2018", url: "/__diag/assets/probe-es2018-v1.js", type: "" },
      { name: "es2020", marker: "__diagProbeES2020", url: "/__diag/assets/probe-es2020-v1.js", type: "" },
      { name: "module", marker: "__diagProbeModule", url: "/__diag/assets/probe-module-v1.js", type: "module" }
    ];
    var index = 0;

    setStep("step-syntax", "running", "正在逐级加载语法探针");

    function next() {
      var probe;
      if (index >= probes.length) {
        setStep(
          "step-syntax",
          report.capabilities.es2020 ? "ok" : "fail",
          "ES2015=" + yesNo(report.capabilities.es2015) +
            "，ES2017=" + yesNo(report.capabilities.es2017) +
            "，ES2018=" + yesNo(report.capabilities.es2018) +
            "，ES2020=" + yesNo(report.capabilities.es2020) +
            "，Module=" + yesNo(report.capabilities.moduleScript)
        );
        sendReport("syntax_complete", done);
        return;
      }

      probe = probes[index];
      index += 1;
      loadScript(probe.url, probe.marker, probe.type, 6000, function (result) {
        report.capabilities[probe.name === "module" ? "moduleScript" : probe.name] =
          result.status === "ok";
        report.capabilities[probe.name + "ProbeStatus"] = result.status;
        next();
      });
    }

    next();
  }

  function runNetworkChecks(done) {
    setStep("step-network", "running", "正在访问 NAS 诊断接口与 Mermaid 文件");

    xhrRequest("GET", "/__diag/meta.json", 15000, function (metaResult) {
      report.network.metaStatus = metaResult.status;
      report.network.metaDurationMs = metaResult.durationMs;
      if (metaResult.ok) {
        try {
          var metadata = JSON.parse(metaResult.text);
          report.network.diagnosticVersion = metadata.diagnosticVersion;
          report.network.mermaidVersion = metadata.mermaidVersion;
          report.network.mermaidPath = metadata.mermaidPath;
          report.network.mermaidBytes = metadata.mermaidBytes;
          report.network.mermaidSHA256 = metadata.mermaidSHA256;
          report.network.securityHeaders = metadata.securityHeaders;
        } catch (error) {
          addError("meta_json", errorMessage(error), "", 0, 0, "/__diag/meta.json");
        }
      }

      xhrRequest("GET", "/__diag/ping", 15000, function (pingResult) {
        report.network.pingStatus = pingResult.status;
        report.network.pingDurationMs = pingResult.durationMs;

        xhrRequest("HEAD", report.network.mermaidPath || "/assets/mermaid-11.16.0.min.js", 20000, function (assetResult) {
          report.network.mermaidHeadStatus = assetResult.status;
          report.network.mermaidHeadDurationMs = assetResult.durationMs;
          report.network.mermaidContentType = assetResult.contentType;
          report.network.mermaidContentLength = assetResult.contentLength;
          report.network.sameOriginChecksOK = !!(metaResult.ok && pingResult.ok && assetResult.ok);

          setStep(
            "step-network",
            report.network.sameOriginChecksOK ? "ok" : "fail",
            "meta=" + metaResult.status + "，ping=" + pingResult.status + "，Mermaid=" + assetResult.status
          );
          sendReport("network_complete", done);
        });
      });
    });
  }

  function loadAndTestMermaid() {
    var mermaidPath = report.network.mermaidPath || "/assets/mermaid-11.16.0.min.js";
    var loadStart = new Date().getTime();
    setStep("step-mermaid", "running", "正在加载约 3.5 MB 的 Mermaid 11");

    loadScript(mermaidPath, "", "", 45000, function (loadResult) {
      report.mermaid.bundleLoadStatus = loadResult.status;
      report.mermaid.bundleLoadDurationMs = new Date().getTime() - loadStart;
      report.mermaid.globalPresent = typeof window.mermaid !== "undefined";

      if (!report.mermaid.globalPresent) {
        setStep("step-mermaid", "fail", "脚本未建立 window.mermaid：" + loadResult.status);
        addError("mermaid_bundle", "Mermaid global was not created", "", 0, 0, mermaidPath);
        sendReport("bundle_failed", function () {
          testProductionInitializer();
        });
        return;
      }

      report.mermaid.api = {
        initialize: typeof window.mermaid.initialize === "function",
        parse: typeof window.mermaid.parse === "function",
        render: typeof window.mermaid.render === "function"
      };
      if (!report.mermaid.api.initialize || !report.mermaid.api.parse || !report.mermaid.api.render) {
        setStep("step-mermaid", "fail", "Mermaid API 不完整");
        addError("mermaid_api", "Mermaid initialize/parse/render API is incomplete", "", 0, 0, mermaidPath);
        sendReport("bundle_api_failed", function () {
          testProductionInitializer();
        });
        return;
      }

      runMermaidTest(
        "default",
        "flowchart TD\n  A[Start] --> B[Done]",
        {
          startOnLoad: false,
          securityLevel: "strict",
          suppressErrorRendering: true,
          theme: "default"
        },
        "manual-default",
        function () {
          runMermaidTest(
            "conservative",
            "flowchart TD\n  A[开始] --> B[完成]",
            {
              startOnLoad: false,
              securityLevel: "strict",
              suppressErrorRendering: true,
              theme: "default",
              flowchart: { htmlLabels: false }
            },
            "manual-conservative",
            function () {
              var defaultOK = report.mermaid.default && report.mermaid.default.status === "ok";
              var conservativeOK = report.mermaid.conservative && report.mermaid.conservative.status === "ok";
              setStep(
                "step-mermaid",
                defaultOK || conservativeOK ? "ok" : "fail",
                "默认配置=" + yesNo(defaultOK) + "，兼容配置=" + yesNo(conservativeOK)
              );
              sendReport("mermaid_complete", function () {
                testProductionInitializer();
              });
            }
          );
        }
      );
    });
  }

  function runMermaidTest(name, source, config, containerID, done) {
    var resultRecord = {
      status: "running",
      parse: "waiting",
      render: "waiting",
      insert: "waiting"
    };
    var container = document.getElementById(containerID);
    var start = new Date().getTime();
    report.mermaid[name] = resultRecord;

    try {
      window.mermaid.initialize(config);
      resultRecord.initialize = "ok";
    } catch (error) {
      resultRecord.status = "initialize_failed";
      resultRecord.initialize = "failed";
      resultRecord.error = cleanMessage(errorMessage(error));
      addError("mermaid_initialize", errorMessage(error), "", 0, 0, name);
      setContainerText(container, name + "：初始化失败");
      done();
      return;
    }

    var parseValue;
    try {
      parseValue = window.mermaid.parse(source, { suppressErrors: true });
    } catch (parseCallError) {
      failTest("parse_failed", "mermaid_parse", parseCallError);
      return;
    }

    settleValue(
      parseValue,
      20000,
      function (parsed) {
        if (parsed === false) {
          failTest("parse_failed", "mermaid_parse", new Error("Mermaid parse returned false"));
          return;
        }
        resultRecord.parse = "ok";
        var renderValue;
        try {
          renderValue = window.mermaid.render(
            "diag-" + name + "-" + report.runId,
            source
          );
        } catch (renderCallError) {
          failTest("render_failed", "mermaid_render", renderCallError);
          return;
        }

        settleValue(
          renderValue,
          30000,
          function (rendered) {
            var svgText = rendered && rendered.svg ? rendered.svg : rendered;
            var svg;
            var rect;
            if (typeof svgText !== "string" || svgText.indexOf("<svg") === -1) {
              failTest("render_empty", "mermaid_render", new Error("Render result did not contain SVG"));
              return;
            }
            resultRecord.render = "ok";
            resultRecord.svgBytes = svgText.length;
            try {
              container.innerHTML = svgText;
              if (rendered && typeof rendered.bindFunctions === "function") {
                rendered.bindFunctions(container);
              }
              svg = container.getElementsByTagName("svg")[0];
              rect = svg && svg.getBoundingClientRect ? svg.getBoundingClientRect() : null;
              resultRecord.insert = svg ? "ok" : "failed";
              resultRecord.svgVisible = !!(rect && rect.width > 0 && rect.height > 0);
              resultRecord.svgWidth = rect ? Math.round(rect.width) : 0;
              resultRecord.svgHeight = rect ? Math.round(rect.height) : 0;
              resultRecord.pathCount = svg ? svg.getElementsByTagName("path").length : 0;
              resultRecord.textCount = svg ? svg.getElementsByTagName("text").length : 0;
              resultRecord.foreignObjectCount = svg ? svg.getElementsByTagName("foreignObject").length : 0;
              resultRecord.status = svg && resultRecord.svgVisible ? "ok" : "svg_not_visible";
              resultRecord.durationMs = new Date().getTime() - start;
            } catch (insertError) {
              failTest("insert_failed", "mermaid_insert", insertError);
              return;
            }
            done();
          },
          function (renderError) {
            failTest("render_failed", "mermaid_render", renderError);
          }
        );
      },
      function (parseError) {
        failTest("parse_failed", "mermaid_parse", parseError);
      }
    );

    function failTest(status, kind, error) {
      resultRecord.status = status;
      resultRecord.error = cleanMessage(errorMessage(error));
      resultRecord.durationMs = new Date().getTime() - start;
      addError(kind, errorMessage(error), "", 0, 0, name);
      setContainerText(container, name + "：" + status);
      done();
    }
  }

  function testProductionInitializer() {
    setStep("step-production", "running", "正在加载与正式页面相同的初始化脚本");
    loadScript("/assets/mermaid-init-v1.js", "", "", 15000, function (loadResult) {
      report.mermaid.productionInitLoadStatus = loadResult.status;
      window.setTimeout(function () {
        var wrapper = document.getElementById("production-probe-wrapper");
        var svgCount = wrapper ? wrapper.getElementsByTagName("svg").length : 0;
        var preCount = wrapper ? wrapper.getElementsByTagName("pre").length : 0;
        var errorCount = 0;
        var preElements;
        var index;

        if (wrapper) {
          preElements = wrapper.getElementsByTagName("pre");
          for (index = 0; index < preElements.length; index += 1) {
            if ((" " + preElements[index].className + " ").indexOf(" mermaid-error ") !== -1) {
              errorCount += 1;
            }
          }
        }

        report.mermaid.productionProbe = {
          svgCount: svgCount,
          sourcePreCount: preCount,
          errorPreCount: errorCount,
          status: svgCount > 0 ? "rendered" : errorCount > 0 ? "source_with_error" : "source_unchanged"
        };

        setStep(
          "step-production",
          svgCount > 0 ? "ok" : "fail",
          "脚本=" + loadResult.status + "，结果=" + report.mermaid.productionProbe.status
        );
        finishDiagnostic("检测完成");
      }, 6000);
    });
  }

  function finishDiagnostic(message) {
    if (diagnosticFinished) {
      return;
    }
    diagnosticFinished = true;
    report.environment.totalDurationMs = new Date().getTime() - startedAt;
    sendReport("final", function (uploaded) {
      if (uploaded) {
        setStep("step-upload", "ok", "最终报告已保存到 NAS");
        setOverall("ok", message + "，报告已上传");
      } else {
        setStep("step-upload", "fail", "自动上传失败，请保留下面的本机报告");
        setOverall("fail", message + "，但最终报告上传失败");
      }
      updateReportOutput();
    });
  }

  function settleValue(value, timeoutMs, success, failure) {
    var finished = false;
    var timer = window.setTimeout(function () {
      if (!finished) {
        finished = true;
        failure(new Error("Operation timed out"));
      }
    }, timeoutMs);

    function succeed(result) {
      if (!finished) {
        finished = true;
        window.clearTimeout(timer);
        success(result);
      }
    }

    function fail(error) {
      if (!finished) {
        finished = true;
        window.clearTimeout(timer);
        failure(error || new Error("Operation failed"));
      }
    }

    try {
      if (value && typeof value.then === "function") {
        value.then(succeed, fail);
      } else {
        succeed(value);
      }
    } catch (error) {
      fail(error);
    }
  }

  function loadScript(url, marker, type, timeoutMs, callback) {
    var script = document.createElement("script");
    var finished = false;
    var start = new Date().getTime();
    var timer;

    function finish(status) {
      if (finished) {
        return;
      }
      finished = true;
      window.clearTimeout(timer);
      callback({
        status: status,
        durationMs: new Date().getTime() - start,
        marker: marker ? !!window[marker] : null
      });
    }

    script.src = url;
    script.async = true;
    if (type) {
      script.setAttribute("type", type);
    }
    script.onload = function () {
      window.setTimeout(function () {
        if (marker) {
          finish(window[marker] ? "ok" : "loaded_without_marker");
        } else {
          finish("loaded");
        }
      }, 250);
    };
    script.onerror = function () {
      addError("script_load", "Script failed to load or parse", "", 0, 0, url);
      finish("error");
    };
    timer = window.setTimeout(function () {
      addError("script_timeout", "Script did not finish loading", "", 0, 0, url);
      finish("timeout");
    }, timeoutMs);

    (document.getElementsByTagName("head")[0] || document.documentElement).appendChild(script);
  }

  function xhrRequest(method, url, timeoutMs, callback) {
    var xhr;
    var started = new Date().getTime();
    var finished = false;

    function finish(ok, status, text) {
      var contentLength = "";
      var contentType = "";
      if (finished) {
        return;
      }
      finished = true;
      try {
        contentLength = xhr.getResponseHeader("Content-Length") || "";
        contentType = xhr.getResponseHeader("Content-Type") || "";
      } catch (headerError) {
        contentLength = "";
        contentType = "";
      }
      callback({
        ok: ok,
        status: status,
        text: text || "",
        durationMs: new Date().getTime() - started,
        contentLength: contentLength,
        contentType: contentType
      });
    }

    try {
      xhr = new XMLHttpRequest();
      xhr.open(method, url, true);
      try {
        xhr.timeout = timeoutMs;
      } catch (timeoutError) {
        report.network.xhrTimeoutProperty = false;
      }
      xhr.onreadystatechange = function () {
        if (xhr.readyState === 4) {
          finish(xhr.status >= 200 && xhr.status < 300, xhr.status, xhr.responseText);
        }
      };
      xhr.onerror = function () {
        addError("xhr_error", "XHR failed", "", 0, 0, url);
        finish(false, xhr.status || 0, "");
      };
      xhr.ontimeout = function () {
        addError("xhr_timeout", "XHR timed out", "", 0, 0, url);
        finish(false, xhr.status || 0, "");
      };
      xhr.send(null);
    } catch (error) {
      addError("xhr_exception", errorMessage(error), "", 0, 0, url);
      callback({
        ok: false,
        status: 0,
        text: "",
        durationMs: new Date().getTime() - started,
        contentLength: "",
        contentType: ""
      });
    }
  }

  function sendReport(phase, callback) {
    var xhr;
    var payload;
    var finished = false;
    report.phase = phase;
    report.clientTime = clientTime();
    updateReportOutput();

    if (!window.JSON || typeof window.XMLHttpRequest === "undefined") {
      setStep("step-upload", "fail", "浏览器缺少 JSON 或 XMLHttpRequest");
      if (callback) {
        callback(false);
      }
      return;
    }

    try {
      payload = JSON.stringify(report);
      xhr = new XMLHttpRequest();
      xhr.open("POST", "/__diag/reports", true);
      xhr.setRequestHeader("Content-Type", "application/json; charset=utf-8");
      try {
        xhr.timeout = 12000;
      } catch (timeoutError) {
      }
      xhr.onreadystatechange = function () {
        if (xhr.readyState === 4 && !finished) {
          finished = true;
          var ok = xhr.status === 202;
          setStep("step-upload", ok ? "ok" : "fail", ok ? "阶段报告已保存：" + phase : "HTTP " + xhr.status);
          if (callback) {
            callback(ok);
          }
        }
      };
      xhr.onerror = function () {
        if (!finished) {
          finished = true;
          setStep("step-upload", "fail", "报告上传发生网络错误");
          if (callback) {
            callback(false);
          }
        }
      };
      xhr.ontimeout = function () {
        if (!finished) {
          finished = true;
          setStep("step-upload", "fail", "报告上传超时");
          if (callback) {
            callback(false);
          }
        }
      };
      xhr.send(payload);
    } catch (error) {
      setStep("step-upload", "fail", "报告序列化或上传失败");
      if (callback) {
        callback(false);
      }
    }
  }

  function scheduleErrorReport() {
    if (reportTimer) {
      window.clearTimeout(reportTimer);
    }
    reportTimer = window.setTimeout(function () {
      sendReport("window_error");
    }, 100);
  }

  function addError(kind, message, file, line, column, resource) {
    if (report.errors.length >= 20) {
      return;
    }
    report.errors.push({
      kind: cleanToken(kind, 64),
      message: cleanMessage(message),
      file: cleanResource(file),
      line: toSmallInteger(line),
      column: toSmallInteger(column),
      resource: cleanResource(resource)
    });
    updateReportOutput();
  }

  function setStep(id, state, detail) {
    var element = document.getElementById(id);
    var spans;
    if (!element) {
      return;
    }
    element.className = "step " + state;
    spans = element.getElementsByTagName("span");
    if (spans.length) {
      setText(spans[0], detail);
    }
  }

  function setOverall(state, message) {
    var element = document.getElementById("overall-status");
    if (!element) {
      return;
    }
    element.className = "overall " + state;
    setText(element, message);
  }

  function setContainerText(container, message) {
    if (container) {
      container.innerHTML = "";
      setText(container, message);
    }
  }

  function updateReportOutput() {
    var output = document.getElementById("report-output");
    if (!output || !window.JSON) {
      return;
    }
    try {
      output.value = JSON.stringify(report, null, 2);
    } catch (error) {
      output.value = "报告暂时无法序列化。";
    }
  }

  function onReady(callback) {
    if (document.readyState === "loading") {
      if (document.addEventListener) {
        document.addEventListener("DOMContentLoaded", callback, false);
      } else {
        window.attachEvent("onload", callback);
      }
    } else {
      window.setTimeout(callback, 0);
    }
  }

  function setText(element, value) {
    if (typeof element.textContent === "string") {
      element.textContent = value;
    } else {
      element.innerText = value;
    }
  }

  function readQueryValue(name) {
    var query = window.location && window.location.search ? window.location.search.substring(1) : "";
    var parts = query ? query.split("&") : [];
    var index;
    var pair;
    for (index = 0; index < parts.length; index += 1) {
      pair = parts[index].split("=");
      if (decodeQueryPart(pair[0]) === name) {
        return decodeQueryPart(pair.length > 1 ? pair.slice(1).join("=") : "");
      }
    }
    return "";
  }

  function decodeQueryPart(value) {
    try {
      return decodeURIComponent(String(value || "").replace(/\+/g, " "));
    } catch (error) {
      return "";
    }
  }

  function normalizeRole(value) {
    if (value === "reader_builtin" || value === "reader_via" || value === "phone_via" || value === "desktop") {
      return value;
    }
    return "unknown";
  }

  function safeNavigatorValue(name) {
    try {
      return cleanText(navigator[name] || "", 2048);
    } catch (error) {
      return "";
    }
  }

  function createRunID() {
    return "r" + new Date().getTime() + "-" + Math.floor(Math.random() * 1000000000);
  }

  function clientTime() {
    var value = new Date();
    try {
      return value.toISOString();
    } catch (error) {
      return String(value);
    }
  }

  function bucket(value, size) {
    var number = Number(value) || 0;
    return Math.round(number / size) * size;
  }

  function yesNo(value) {
    return value ? "是" : "否";
  }

  function errorMessage(error) {
    if (error && error.message) {
      return String(error.message);
    }
    return String(error || "Unknown error");
  }

  function cleanMessage(value) {
    return cleanText(value, 1000);
  }

  function cleanResource(value) {
    var text = cleanText(value, 256);
    var queryIndex = text.indexOf("?");
    var hashIndex = text.indexOf("#");
    if (queryIndex >= 0) {
      text = text.substring(0, queryIndex);
    }
    if (hashIndex >= 0) {
      text = text.substring(0, hashIndex);
    }
    return text;
  }

  function cleanToken(value, limit) {
    return cleanText(value, limit).replace(/[^A-Za-z0-9_-]/g, "_");
  }

  function cleanText(value, limit) {
    var text = String(value || "").replace(/[\r\n\t]+/g, " ");
    if (text.length > limit) {
      return text.substring(0, limit);
    }
    return text;
  }

  function toSmallInteger(value) {
    var number = parseInt(value, 10);
    if (!isFinite(number) || number < 0 || number > 10000000) {
      return 0;
    }
    return number;
  }
})();
