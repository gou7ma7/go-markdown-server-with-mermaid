(function () {
  "use strict";
  var original = { one: 1 };
  var copied = { ...original, two: 2 };
  window.__diagProbeES2018 = copied.one === 1 && copied.two === 2;
})();
