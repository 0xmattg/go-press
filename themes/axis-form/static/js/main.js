(function () {
  var toggle = document.querySelector('.af-nav-toggle');
  var nav = document.querySelector('.af-nav');
  if (!toggle || !nav) return;
  toggle.addEventListener('click', function () {
    var open = nav.classList.toggle('is-open');
    toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
  });
})();
