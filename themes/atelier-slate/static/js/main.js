// ===========================
// Mobile Navigation Toggle
// ===========================
(function () {
    const toggle = document.getElementById('nav-toggle');
    const menu = document.getElementById('primary-nav');

    if (toggle && menu) {
        toggle.addEventListener('click', function () {
            toggle.classList.toggle('open');
            menu.classList.toggle('open');
        });

        // Close menu when clicking a link
        menu.querySelectorAll('a').forEach(function (link) {
            link.addEventListener('click', function () {
                toggle.classList.remove('open');
                menu.classList.remove('open');
            });
        });
    }
})();

// ===========================
// Hero Slider
// ===========================
(function () {
    const slider = document.getElementById('hero-slider');
    const dotsContainer = document.getElementById('hero-dots');
    if (!slider) return;

    const slides = slider.querySelectorAll('.hero-slide');
    if (slides.length === 0) return;

    let currentIndex = 0;
    let interval;

    // Create dots
    slides.forEach(function (_, i) {
        const dot = document.createElement('button');
        dot.className = 'hero-dot' + (i === 0 ? ' active' : '');
        dot.setAttribute('aria-label', 'Go to slide ' + (i + 1));
        dot.addEventListener('click', function () {
            goToSlide(i);
            resetInterval();
        });
        dotsContainer.appendChild(dot);
    });

    function goToSlide(index) {
        slides[currentIndex].classList.remove('active');
        dotsContainer.children[currentIndex].classList.remove('active');
        currentIndex = index;
        slides[currentIndex].classList.add('active');
        dotsContainer.children[currentIndex].classList.add('active');
    }

    function nextSlide() {
        goToSlide((currentIndex + 1) % slides.length);
    }

    function resetInterval() {
        clearInterval(interval);
        interval = setInterval(nextSlide, 5000);
    }

    resetInterval();
})();

// ===========================
// Sticky Header Shadow on Scroll
// ===========================
(function () {
    const header = document.getElementById('site-header');
    if (!header) return;
    window.addEventListener('scroll', function () {
        if (window.scrollY > 50) {
            header.classList.add('scrolled');
        } else {
            header.classList.remove('scrolled');
        }
    });
})();

// ===========================
// Scroll to Top Button
// ===========================
(function () {
    const btn = document.getElementById('scroll-top');
    if (!btn) return;

    window.addEventListener('scroll', function () {
        if (window.scrollY > 400) {
            btn.classList.add('visible');
        } else {
            btn.classList.remove('visible');
        }
    });

    btn.addEventListener('click', function () {
        window.scrollTo({ top: 0, behavior: 'smooth' });
    });
})();

// ===========================
// Contact Form Validation
// ===========================
(function () {
    var form = document.getElementById('contact-form');
    if (!form) return;

    form.addEventListener('submit', function (e) {
        var name = form.querySelector('#name');
        var email = form.querySelector('#email');
        var message = form.querySelector('#message');
        var errors = [];

        if (!name.value.trim()) errors.push('Name is required');
        if (!email.value.trim()) {
            errors.push('Email is required');
        } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value)) {
            errors.push('Please enter a valid email');
        }
        if (!message.value.trim()) errors.push('Message is required');

        if (errors.length > 0) {
            e.preventDefault();
            alert(errors.join('\n'));
        }
    });
})();
