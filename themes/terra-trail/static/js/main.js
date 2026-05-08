(function () {
  var toggle = document.querySelector('.tt-nav-toggle');
  var nav = document.querySelector('.tt-nav');
  if (!toggle || !nav) return;
  toggle.addEventListener('click', function () {
    var open = nav.classList.toggle('is-open');
    toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
  });
})();
