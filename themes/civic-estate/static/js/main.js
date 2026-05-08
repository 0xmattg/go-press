(function () {
  var toggle = document.querySelector('.ce-nav-toggle');
  var nav = document.querySelector('.ce-nav');
  if (!toggle || !nav) return;
  toggle.addEventListener('click', function () {
    var open = nav.classList.toggle('is-open');
    toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
  });
})();
