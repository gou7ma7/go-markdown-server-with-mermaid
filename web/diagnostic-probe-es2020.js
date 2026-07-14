(function () {
  "use strict";
  var value = { nested: { answer: 42 } };
  window.__diagProbeES2020 = (value?.nested?.answer ?? 0) === 42;
})();
