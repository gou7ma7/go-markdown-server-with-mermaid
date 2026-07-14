(function () {
  "use strict";
  const value = { number: 1 };
  const read = (item) => `${item.number}`;
  class ProbeMarker {}
  window.__diagProbeES2015 = read(value) === "1" && typeof ProbeMarker === "function";
})();
