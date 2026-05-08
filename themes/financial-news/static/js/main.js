// Financial News Theme - Minimal JS
document.addEventListener('DOMContentLoaded', function() {
    // Smooth scroll for anchor links
    document.querySelectorAll('a[href^="#"]').forEach(function(anchor) {
        anchor.addEventListener('click', function(e) {
            e.preventDefault();
            var target = document.querySelector(this.getAttribute('href'));
            if (target) {
                target.scrollIntoView({ behavior: 'smooth' });
            }
        });
    });

    // Auto-refresh market status indicator
    var statusEl = document.querySelector('.market-status');
    if (statusEl) {
        var now = new Date();
        var hours = now.getHours();
        // Simple check: Chinese market hours 9:30 - 15:00
        if (hours >= 9 && hours < 15) {
            statusEl.textContent = '● 交易中';
            statusEl.style.color = '#3fb950';
        } else {
            statusEl.textContent = '● 已休市';
            statusEl.style.color = '#8b949e';
        }
    }
});
