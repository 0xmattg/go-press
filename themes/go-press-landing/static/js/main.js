/* GoPress Landing — main.js */
(function() {
    'use strict';

    // ---- Sticky header style on scroll ----
    var header = document.getElementById('site-header');
    if (header) {
        window.addEventListener('scroll', function() {
            header.classList.toggle('scrolled', window.scrollY > 40);
        }, { passive: true });
    }

    // ---- Mobile nav toggle ----
    var toggle = document.getElementById('nav-toggle');
    var links = document.getElementById('nav-links');
    if (toggle && links) {
        toggle.addEventListener('click', function() {
            links.classList.toggle('open');
        });
        // Close menu on anchor click
        links.querySelectorAll('a').forEach(function(a) {
            a.addEventListener('click', function() { links.classList.remove('open'); });
        });
    }

    // ---- Scroll-reveal (IntersectionObserver) ----
    var reveals = document.querySelectorAll('.reveal');
    if (reveals.length && 'IntersectionObserver' in window) {
        var observer = new IntersectionObserver(function(entries) {
            entries.forEach(function(entry) {
                if (entry.isIntersecting) {
                    entry.target.classList.add('visible');
                    observer.unobserve(entry.target);
                }
            });
        }, { threshold: 0.12 });
        reveals.forEach(function(el) { observer.observe(el); });
    } else {
        // Fallback: show everything
        reveals.forEach(function(el) { el.classList.add('visible'); });
    }

    // ---- Smooth-scroll for anchor links (fallback for older browsers) ----
    document.querySelectorAll('a[href^="#"]').forEach(function(a) {
        a.addEventListener('click', function(e) {
            var target = document.querySelector(this.getAttribute('href'));
            if (target) {
                e.preventDefault();
                target.scrollIntoView({ behavior: 'smooth' });
            }
        });
    });
})();
