(async function () {
  "use strict";
  var value = await Promise.resolve(1);
  window.__diagProbeES2017 = value === 1;
})().catch(function () {
  window.__diagProbeES2017 = false;
});
