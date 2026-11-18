// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Inline payment bootstrap. It hands the gateway's client data to the
// payment widget that the commerce.payment.inline hook rendered, by
// dispatching gopress:payment:inline on window. A static file rather than an
// inline script, so pages keep working under a Content-Security-Policy that
// allows no inline scripts (theme packages).
(function () {
  var root = document.getElementById('gopress-inline-payment');
  if (!root) return;
  var clientData = {};
  try { clientData = JSON.parse(root.getAttribute('data-client') || '{}'); } catch (_) {}
  window.dispatchEvent(new CustomEvent('gopress:payment:inline', {detail: {
    root: root,
    clientData: clientData,
    completeURL: root.getAttribute('data-complete-url') || ''
  }}));
}());
