// Admin JS
function adminText(key, fallback) {
    var dict = window.GoPressAdminI18n || {};
    return dict[key] || fallback || key;
}

function adminFormat(template) {
    var args = Array.prototype.slice.call(arguments, 1);
    var idx = 0;
    return String(template).replace(/%[sdv]/g, function() {
        return idx < args.length ? String(args[idx++]) : '';
    });
}

(function() {
    var i18n = window.GoPressAdminI18n || {};
    // Sidebar toggle (mobile)
    var toggle = document.getElementById('sidebarToggle');
    var sidebar = document.getElementById('sidebar');
    if (toggle && sidebar) {
        toggle.addEventListener('click', function() {
            sidebar.classList.toggle('open');
        });
    }

    // Auto-dismiss alerts after 4 seconds
    var alerts = document.querySelectorAll('.alert');
    alerts.forEach(function(el) {
        setTimeout(function() {
            el.style.transition = 'opacity .3s';
            el.style.opacity = '0';
            setTimeout(function() { el.remove(); }, 300);
        }, 4000);
    });

    // Global pending-action progress overlay.
    // Forms opt in with data-admin-progress and keep their normal submit flow.
    var progressOverlay = document.getElementById('adminProgressOverlay');
    var progressTitle = document.getElementById('adminProgressTitle');
    var progressMessage = document.getElementById('adminProgressMessage');

    function showAdminProgress(form) {
        if (!progressOverlay) return;
        var title = form.getAttribute('data-progress-title') || adminText('progressWorkingTitle', 'Working...');
        var message = form.getAttribute('data-progress-message') || adminText('progressWorkingMessage', 'Please wait while the operation completes.');
        if (progressTitle) progressTitle.textContent = title;
        if (progressMessage) progressMessage.textContent = message;
        progressOverlay.hidden = false;
        document.documentElement.classList.add('admin-progress-lock');
        requestAnimationFrame(function() {
            progressOverlay.classList.add('show');
        });
        form.querySelectorAll('button, input[type="submit"]').forEach(function(el) {
            el.disabled = true;
            el.setAttribute('aria-disabled', 'true');
        });
    }

    document.querySelectorAll('form[data-admin-progress]').forEach(function(form) {
        form.addEventListener('submit', function() {
            showAdminProgress(form);
        });
    });

    // Auto-generate slug from title
    var titleInput = document.getElementById('title');
    var slugInput = document.getElementById('slug');
    if (titleInput && slugInput && !slugInput.value) {
        titleInput.addEventListener('input', function() {
            slugInput.value = titleInput.value
                .toLowerCase()
                .replace(/[^a-z0-9\u4e00-\u9fff]+/g, '-')
                .replace(/^-|-$/g, '');
        });
    }

    // Copy media URL on click
    document.querySelectorAll('.media-url').forEach(function(el) {
        el.addEventListener('click', function() {
            this.select();
            document.execCommand('copy');
        });
    });

    // ==================== Quill Rich Editor ====================
    var richEditors = document.querySelectorAll('textarea.rich-editor');
    if (richEditors.length > 0) {
        // Load Quill CSS
        var link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = 'https://cdn.jsdelivr.net/npm/quill@2.0.3/dist/quill.snow.css';
        document.head.appendChild(link);

        // Load Quill JS
        var script = document.createElement('script');
        script.src = 'https://cdn.jsdelivr.net/npm/quill@2.0.3/dist/quill.js';
        script.onload = function() {
            richEditors.forEach(function(textarea) {
                // Create editor container
                var editorDiv = document.createElement('div');
                editorDiv.style.minHeight = '300px';
                editorDiv.innerHTML = textarea.value;
                textarea.style.display = 'none';
                textarea.parentNode.insertBefore(editorDiv, textarea.nextSibling);

                var quill = new Quill(editorDiv, {
                    theme: 'snow',
                    modules: {
                        toolbar: {
                            container: [
                                [{ 'header': [1, 2, 3, 4, false] }],
                                ['bold', 'italic', 'underline', 'strike'],
                                [{ 'color': [] }, { 'background': [] }],
                                [{ 'align': [] }],
                                [{ 'list': 'ordered' }, { 'list': 'bullet' }],
                                [{ 'indent': '-1' }, { 'indent': '+1' }],
                                ['blockquote', 'code-block'],
                                ['link', 'image'],
                                ['clean'],
                                ['html-source']
                            ],
                            handlers: {
                                'html-source': function() {
                                    toggleSourceMode();
                                },
                                image: function() {
                                    var q = quill;
                                    openMediaPicker(function(url, item) {
                                        var range = q.getSelection(true);
                                        var alt = (item && item.alt_text) || '';
                                        var title = (item && item.title) || '';
                                        var caption = (item && item.caption) || '';
                                        // Build HTML: figure + img (+ figcaption if caption)
                                        var imgAttrs = 'src="' + url + '"';
                                        if (alt) imgAttrs += ' alt="' + escapeAttr(alt) + '"';
                                        if (title) imgAttrs += ' title="' + escapeAttr(title) + '"';
                                        var html;
                                        if (caption) {
                                            html = '<figure><img ' + imgAttrs + '><figcaption>' + escapeHTML(caption) + '</figcaption></figure>';
                                        } else {
                                            html = '<img ' + imgAttrs + '>';
                                        }
                                        q.clipboard.dangerouslyPasteHTML(range.index, html);
                                        q.setSelection(range.index + 1);
                                    });
                                }
                            }
                        }
                    }
                });

                var sourceTextarea = document.createElement('textarea');
                sourceTextarea.className = 'html-source-editor';
                sourceTextarea.setAttribute('aria-label', adminText('editorHtmlSourceLabel', 'HTML source'));
                sourceTextarea.setAttribute('spellcheck', 'false');
                sourceTextarea.style.display = 'none';
                editorDiv.parentNode.insertBefore(sourceTextarea, editorDiv.nextSibling);

                var sourceMode = false;
                var toolbar = editorDiv.previousElementSibling;
                if (!toolbar || !toolbar.classList.contains('ql-toolbar')) {
                    toolbar = editorDiv.parentNode.querySelector('.ql-toolbar');
                }
                var sourceButton = toolbar ? toolbar.querySelector('.ql-html-source') : null;

                if (sourceButton) {
                    sourceButton.setAttribute('type', 'button');
                    sourceButton.setAttribute('aria-pressed', 'false');
                    var sourceGroup = sourceButton.closest('.ql-formats');
                    if (sourceGroup) {
                        sourceGroup.classList.add('ql-html-source-group');
                    }
                }

                function setToolbarDisabled(disabled) {
                    if (!toolbar) return;
                    toolbar.querySelectorAll('button, select').forEach(function(control) {
                        if (control === sourceButton) return;
                        control.disabled = disabled;
                        control.classList.toggle('ql-source-disabled', disabled);
                    });
                }

                function setSourceButtonActive(active) {
                    if (!sourceButton) return;
                    var label = active
                        ? adminText('editorVisualModeTitle', 'Return to visual editor')
                        : adminText('editorHtmlSourceTitle', 'View HTML source');
                    sourceButton.textContent = label;
                    sourceButton.classList.toggle('ql-active', active);
                    sourceButton.setAttribute('aria-pressed', active ? 'true' : 'false');
                    sourceButton.setAttribute('aria-label', label);
                }

                setSourceButtonActive(false);

                function enterSourceMode() {
                    sourceTextarea.value = quill.root.innerHTML;
                    textarea.value = sourceTextarea.value;
                    editorDiv.style.display = 'none';
                    sourceTextarea.style.display = 'block';
                    sourceMode = true;
                    setToolbarDisabled(true);
                    setSourceButtonActive(true);
                    sourceTextarea.focus();
                }

                function exitSourceMode() {
                    var html = sourceTextarea.value;
                    textarea.value = html;
                    quill.root.innerHTML = html;
                    quill.update('silent');
                    sourceTextarea.style.display = 'none';
                    editorDiv.style.display = '';
                    sourceMode = false;
                    setToolbarDisabled(false);
                    setSourceButtonActive(false);
                    quill.focus();
                }

                function toggleSourceMode() {
                    if (sourceMode) {
                        exitSourceMode();
                    } else {
                        enterSourceMode();
                    }
                }

                sourceTextarea.addEventListener('input', function() {
                    if (sourceMode) {
                        textarea.value = sourceTextarea.value;
                    }
                });

                // Sync HTML back to textarea on form submit
                var form = textarea.closest('form') || document.getElementById('contentForm');
                if (form) {
                    form.addEventListener('submit', function() {
                        textarea.value = sourceMode ? sourceTextarea.value : quill.root.innerHTML;
                    });
                }
            });
        };
        document.head.appendChild(script);
    }

    // ==================== Content List Drag & Drop Sort ====================
    // Opt-in via table.sortable-list[data-reorder-url]. Each <tr> that should
    // participate must carry data-id and draggable="true". On drop we rewrite
    // the row order in the DOM, renumber the .sort-order-cell column (optimistic
    // update), then POST {ids:[...]} to the reorder endpoint. Works under any
    // active tab (e.g. language tab) because we read whatever IDs are currently
    // rendered — the server rewrites sort_order for exactly those rows.
    document.querySelectorAll('table.sortable-list[data-reorder-url]').forEach(function(table) {
        var url = table.getAttribute('data-reorder-url');
        var tbody = table.querySelector('tbody');
        if (!tbody || !url) return;

        var dragged = null;

        function rowsInOrder() {
            return Array.prototype.filter.call(
                tbody.querySelectorAll('tr[data-id]'),
                function(r) { return r.offsetParent !== null; }
            );
        }

        function renumber() {
            rowsInOrder().forEach(function(row, i) {
                var cell = row.querySelector('.sort-order-cell');
                if (cell) cell.textContent = i + 1;
            });
        }

        function submitOrder() {
            var ids = rowsInOrder().map(function(r) {
                return parseInt(r.getAttribute('data-id'), 10);
            }).filter(function(n) { return !isNaN(n); });
            if (ids.length === 0) return;

            table.classList.add('reordering');
            fetch(url, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'same-origin',
                body: JSON.stringify({ ids: ids })
            }).then(function(res) {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            }).then(function() {
                renumber();
                flashToast(i18n.orderSaved || 'Order saved');
            }).catch(function(err) {
                flashToast((i18n.orderSaveFailed || 'Failed to save order: ') + err.message, true);
            }).finally(function() {
                table.classList.remove('reordering');
            });
        }

        tbody.addEventListener('dragstart', function(e) {
            var tr = e.target.closest('tr[data-id]');
            if (!tr) return;
            dragged = tr;
            tr.classList.add('dragging');
            e.dataTransfer.effectAllowed = 'move';
            // Firefox requires setData to actually start the drag.
            try { e.dataTransfer.setData('text/plain', tr.getAttribute('data-id')); } catch (_) {}
        });

        tbody.addEventListener('dragover', function(e) {
            if (!dragged) return;
            e.preventDefault();
            var tr = e.target.closest('tr[data-id]');
            if (!tr || tr === dragged) return;
            var rect = tr.getBoundingClientRect();
            var after = (e.clientY - rect.top) > rect.height / 2;
            tbody.insertBefore(dragged, after ? tr.nextSibling : tr);
        });

        tbody.addEventListener('dragend', function() {
            if (!dragged) return;
            dragged.classList.remove('dragging');
            dragged = null;
            submitOrder();
        });
    });

    function flashToast(msg, isError) {
        var t = document.createElement('div');
        t.className = 'admin-toast' + (isError ? ' is-error' : '');
        t.textContent = msg;
        document.body.appendChild(t);
        requestAnimationFrame(function() { t.classList.add('show'); });
        setTimeout(function() {
            t.classList.remove('show');
            setTimeout(function() { t.remove(); }, 300);
        }, 2000);
    }

    // ==================== Content List Title Filter ====================
    // Pure client-side filter over already-rendered rows. The magnifier button
    // is a no-op visual "commit" affordance (blurs the input). The native
    // <input type="search"> × handles clearing — its empty value fires an
    // input event that runs applyFilter and restores all rows.
    var searchBox = document.querySelector('[data-content-list-search]');
    if (searchBox) {
        var input = searchBox.querySelector('[data-search-input]');
        var commitBtn = searchBox.querySelector('[data-search-commit]');
        var table = document.querySelector('.content-list-table tbody');
        if (input && table) {
            var rows = Array.prototype.slice.call(table.querySelectorAll('tr'));
            var emptyMsg = null;
            var applyFilter = function() {
                var q = (input.value || '').trim().toLowerCase();
                var visible = 0;
                rows.forEach(function(row) {
                    if (row.querySelector('.empty-msg')) return;
                    var cell = row.querySelector('.title-cell');
                    var title = cell ? (cell.dataset.title || cell.textContent || '') : '';
                    var match = q === '' || title.toLowerCase().indexOf(q) !== -1;
                    row.style.display = match ? '' : 'none';
                    if (match) visible++;
                });
                if (q !== '' && visible === 0) {
                    if (!emptyMsg) {
                        emptyMsg = document.createElement('tr');
                        emptyMsg.className = 'js-no-match-row';
                        var td = document.createElement('td');
                        td.colSpan = 20;
                        td.className = 'empty-msg';
                        td.textContent = i18n.noMatches || 'No matching items';
                        emptyMsg.appendChild(td);
                        table.appendChild(emptyMsg);
                    }
                    emptyMsg.style.display = '';
                } else if (emptyMsg) {
                    emptyMsg.style.display = 'none';
                }
            };
            input.addEventListener('input', applyFilter);
            input.addEventListener('keydown', function(e) {
                if (e.key === 'Enter') { e.preventDefault(); input.blur(); }
            });
            if (commitBtn) commitBtn.addEventListener('click', function() { input.blur(); });
        }
    }
})();

// ==================== Image Field Functions ====================

// ==================== Image Pool (Unified) ====================

function getImagePool() {
    var featuredUrl = (document.getElementById('image_url') || {}).value || '';
    var galleryInput = (document.getElementById('gallery_images') || {}).value || '';
    var galleryUrls = galleryInput ? galleryInput.split(',').filter(function(u) { return u.trim() !== ''; }) : [];
    var pool = [];
    if (featuredUrl) pool.push(featuredUrl);
    galleryUrls.forEach(function(u) {
        if (u !== featuredUrl) pool.push(u);
    });
    return { urls: pool, featuredIndex: featuredUrl ? 0 : -1 };
}

function syncImagePool(urls, featuredIndex) {
    var imageUrlInput = document.getElementById('image_url');
    var galleryInput = document.getElementById('gallery_images');
    if (imageUrlInput) {
        imageUrlInput.value = (featuredIndex >= 0 && featuredIndex < urls.length) ? urls[featuredIndex] : '';
    }
    if (galleryInput) {
        var others = urls.filter(function(_, i) { return i !== featuredIndex; });
        galleryInput.value = others.join(',');
    }
}

function renderImagePool() {
    var container = document.getElementById('imagePoolGrid');
    if (!container) return;
    var pool = getImagePool();
    container.innerHTML = '';

    pool.urls.forEach(function(url, idx) {
        var card = document.createElement('div');
        card.className = 'image-pool-card' + (idx === pool.featuredIndex ? ' featured' : '');
        var html = '<img src="' + url + '" alt="image-' + idx + '">';
        if (idx === pool.featuredIndex) {
            html += '<span class="image-pool-badge">' + adminText('featured', 'Featured') + '</span>';
        }
        html += '<button type="button" class="image-pool-remove" onclick="removeImageFromPool(' + idx + ')" title="' + adminText('remove', 'Remove') + '">&times;</button>';
        if (idx !== pool.featuredIndex) {
            html += '<button type="button" class="image-pool-set-featured" onclick="setFeaturedImage(' + idx + ')">' + adminText('setFeatured', 'Set Featured') + '</button>';
        }
        card.innerHTML = html;
        container.appendChild(card);
    });

    // Add card at the end
    var addCard = document.createElement('div');
    addCard.className = 'image-pool-add';
    addCard.onclick = function() { addImageToPool(); };
    addCard.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg><span>' + adminText('addImage', 'Add Image') + '</span>';
    container.appendChild(addCard);
}

function setFeaturedImage(idx) {
    var pool = getImagePool();
    if (idx >= 0 && idx < pool.urls.length) {
        syncImagePool(pool.urls, idx);
        renderImagePool();
    }
}

function removeImageFromPool(idx) {
    var pool = getImagePool();
    var newFeatured = pool.featuredIndex;
    if (idx === pool.featuredIndex) {
        pool.urls.splice(idx, 1);
        newFeatured = pool.urls.length > 0 ? 0 : -1;
    } else {
        pool.urls.splice(idx, 1);
        if (idx < newFeatured) newFeatured--;
    }
    syncImagePool(pool.urls, newFeatured);
    renderImagePool();
}

function addImageToPool() {
    openMediaPicker(function(url) {
        var pool = getImagePool();
        pool.urls.push(url);
        if (pool.featuredIndex < 0) pool.featuredIndex = 0;
        syncImagePool(pool.urls, pool.featuredIndex);
        renderImagePool();
    });
}

function uploadImageToPool(input) {
    if (!input.files || !input.files.length) return;
    Array.from(input.files).forEach(function(file) {
        var formData = new FormData();
        formData.append('file', file);
        fetch('/admin/media/upload-json', { method: 'POST', body: formData })
            .then(function(resp) { return resp.json(); })
            .then(function(data) {
                if (data.error) { alert(adminText('uploadFailed', 'Upload failed') + ': ' + data.error); return; }
                var pool = getImagePool();
                pool.urls.push(data.url);
                if (pool.featuredIndex < 0) pool.featuredIndex = 0;
                syncImagePool(pool.urls, pool.featuredIndex);
                renderImagePool();
            })
            .catch(function() { alert(adminText('uploadFailed', 'Upload failed')); });
    });
    input.value = '';
}

// Backward compatibility: old functions delegate to pool
function setImagePreview(url) {
    var pool = getImagePool();
    if (url) {
        if (pool.featuredIndex >= 0) pool.urls[pool.featuredIndex] = url;
        else { pool.urls.unshift(url); pool.featuredIndex = 0; }
    } else if (pool.featuredIndex >= 0) {
        pool.urls.splice(pool.featuredIndex, 1);
        pool.featuredIndex = pool.urls.length > 0 ? 0 : -1;
    }
    syncImagePool(pool.urls, pool.featuredIndex);
    renderImagePool();
}
function removeImage() { setImagePreview(''); }

// Init image pool on page load
document.addEventListener('DOMContentLoaded', function() {
    if (document.getElementById('imagePoolGrid')) {
        renderImagePool();
    }
});

// ==================== Media Picker Modal ====================

var _mediaPickerSelected = '';   // selected URL (legacy)
var _mediaPickerSelectedItem = null;  // selected full item (id/url/alt_text/title/caption)
var _mediaPickerCallback = null; // Callback signature: (url, item)
var _mediaPickerPage = 1;

function escapeAttr(s) { return String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }
function escapeHTML(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }

function openMediaPicker(callback) {
    _mediaPickerSelected = '';
    _mediaPickerSelectedItem = null;
    _mediaPickerCallback = callback || null;
    var modal = document.getElementById('mediaPickerModal');
    var confirmBtn = document.getElementById('mediaPickerConfirm');
    if (confirmBtn) confirmBtn.disabled = true;
    var metaPanel = document.getElementById('mediaPickerMetaPanel');
    if (metaPanel) metaPanel.style.display = 'none';
    var pager = document.getElementById('mediaPickerPagination');
    if (pager) pager.innerHTML = '';
    modal.classList.add('active');
    loadMediaPickerItems(1);
}

function closeMediaPicker() {
    document.getElementById('mediaPickerModal').classList.remove('active');
    _mediaPickerCallback = null;
    _mediaPickerSelectedItem = null;
}

function confirmMediaPicker() {
    if (!_mediaPickerSelected) { closeMediaPicker(); return; }
    var item = _mediaPickerSelectedItem;
    var altEl = document.getElementById('mediaPickerAlt');
    var titleEl = document.getElementById('mediaPickerTitle');
    var capEl = document.getElementById('mediaPickerCaption');
    var newAlt = altEl ? altEl.value : '';
    var newTitle = titleEl ? titleEl.value : '';
    var newCaption = capEl ? capEl.value : '';
    var changed = item && (newAlt !== (item.alt_text || '') || newTitle !== (item.title || '') || newCaption !== (item.caption || ''));

    function fire() {
        if (item) { item.alt_text = newAlt; item.title = newTitle; item.caption = newCaption; }
        if (typeof _mediaPickerCallback === 'function') {
            _mediaPickerCallback(_mediaPickerSelected, item);
        } else {
            setImagePreview(_mediaPickerSelected);
        }
        closeMediaPicker();
    }

    if (changed && item && item.id) {
        var fd = new FormData();
        fd.append('alt_text', newAlt);
        fd.append('title', newTitle);
        fd.append('caption', newCaption);
        fetch('/admin/media/' + item.id + '/meta', { method: 'POST', body: fd })
            .then(function(r) { return r.json(); })
            .then(function() { fire(); })
            .catch(function() { fire(); });
    } else {
        fire();
    }
}

function loadMediaPickerItems(page) {
    var grid = document.getElementById('mediaPickerGrid');
    var pager = document.getElementById('mediaPickerPagination');
    var metaPanel = document.getElementById('mediaPickerMetaPanel');
    var confirmBtn = document.getElementById('mediaPickerConfirm');
    page = parseInt(page, 10) || 1;
    if (page < 1) page = 1;
    _mediaPickerPage = page;
    _mediaPickerSelected = '';
    _mediaPickerSelectedItem = null;
    if (confirmBtn) confirmBtn.disabled = true;
    if (metaPanel) metaPanel.style.display = 'none';
    grid.innerHTML = '<div class="media-picker-loading">' + adminText('loading', 'Loading...') + '</div>';
    if (pager) pager.innerHTML = '';

    fetch('/admin/media/json?page=' + page)
        .then(function(resp) { return resp.json(); })
        .then(function(data) {
            var totalPages = parseInt(data.pages, 10) || 0;
            if (totalPages > 0 && page > totalPages) {
                loadMediaPickerItems(totalPages);
                return;
            }
            grid.innerHTML = '';
            if (!data.items || data.items.length === 0) {
                grid.innerHTML = '<div class="media-picker-loading">' + adminText('emptyUploadFirst', 'No images yet. Upload one first.') + '</div>';
                renderMediaPickerPagination(data);
                return;
            }
            data.items.forEach(function(item) {
                var div = document.createElement('div');
                div.className = 'media-picker-item';
                div.setAttribute('data-url', item.url);
                div.innerHTML = '<img src="' + escapeAttr(item.url) + '" alt="' + escapeAttr(item.alt_text || item.name || '') + '" loading="lazy">';
                div.addEventListener('click', function() {
                    document.querySelectorAll('.media-picker-item.selected').forEach(function(el) {
                        el.classList.remove('selected');
                    });
                    div.classList.add('selected');
                    _mediaPickerSelected = item.url;
                    _mediaPickerSelectedItem = item;
                    var confirmBtn = document.getElementById('mediaPickerConfirm');
                    if (confirmBtn) confirmBtn.disabled = false;
                    // Populate meta panel
                    var altEl = document.getElementById('mediaPickerAlt');
                    var titleEl = document.getElementById('mediaPickerTitle');
                    var capEl = document.getElementById('mediaPickerCaption');
                    var panel = document.getElementById('mediaPickerMetaPanel');
                    if (altEl) altEl.value = item.alt_text || '';
                    if (titleEl) titleEl.value = item.title || '';
                    if (capEl) capEl.value = item.caption || '';
                    if (panel) panel.style.display = '';
                });
                grid.appendChild(div);
            });
            renderMediaPickerPagination(data);
        })
        .catch(function() {
            grid.innerHTML = '<div class="media-picker-loading">' + adminText('loadFailed', 'Failed to load') + '</div>';
            if (pager) pager.innerHTML = '';
        });
}

function renderMediaPickerPagination(data) {
    var pager = document.getElementById('mediaPickerPagination');
    if (!pager) return;
    var page = parseInt(data.page, 10) || _mediaPickerPage || 1;
    var pages = parseInt(data.pages, 10) || 0;
    var total = parseInt(data.total, 10) || 0;
    pager.innerHTML = '';
    if (pages <= 1) {
        if (total > 0) {
            var only = document.createElement('span');
            only.className = 'media-picker-page-info';
            only.textContent = adminFormat(adminText('totalImages', '%d images'), total);
            pager.appendChild(only);
        }
        return;
    }

    var info = document.createElement('span');
    info.className = 'media-picker-page-info';
    info.textContent = adminFormat(adminText('pageInfo', 'Page %d / %d, %d images'), page, pages, total);
    pager.appendChild(info);

    var controls = document.createElement('div');
    controls.className = 'media-picker-page-controls';

    function addButton(label, targetPage, disabled, active) {
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'media-picker-page-btn' + (active ? ' active' : '');
        btn.textContent = label;
        btn.disabled = !!disabled;
        if (!disabled && !active) {
            btn.addEventListener('click', function() {
                loadMediaPickerItems(targetPage);
            });
        }
        controls.appendChild(btn);
    }

    addButton(adminText('previousPage', 'Previous'), page - 1, page <= 1, false);

    var start = Math.max(1, page - 2);
    var end = Math.min(pages, page + 2);
    if (start > 1) {
        addButton('1', 1, false, page === 1);
        if (start > 2) {
            var leftDots = document.createElement('span');
            leftDots.className = 'media-picker-page-ellipsis';
            leftDots.textContent = '...';
            controls.appendChild(leftDots);
        }
    }
    for (var i = start; i <= end; i++) {
        addButton(String(i), i, false, i === page);
    }
    if (end < pages) {
        if (end < pages - 1) {
            var rightDots = document.createElement('span');
            rightDots.className = 'media-picker-page-ellipsis';
            rightDots.textContent = '...';
            controls.appendChild(rightDots);
        }
        addButton(String(pages), pages, false, page === pages);
    }

    addButton(adminText('nextPage', 'Next'), page + 1, page >= pages, false);
    pager.appendChild(controls);
}

function modalUploadFile(input) {
    if (!input.files || !input.files[0]) return;
    var status = document.getElementById('modalUploadStatus');
    if (status) status.textContent = adminText('uploading', 'Uploading...');

    var formData = new FormData();
    formData.append('file', input.files[0]);

    fetch('/admin/media/upload-json', { method: 'POST', body: formData })
        .then(function(resp) { return resp.json(); })
        .then(function(data) {
            if (data.error) {
                if (status) status.textContent = adminText('uploadFailed', 'Upload failed') + ': ' + data.error;
                return;
            }
            if (status) status.textContent = adminText('uploadSuccess', 'Upload complete');
            // Reload the grid and auto-select the new image
            loadMediaPickerItems(1);
            setTimeout(function() {
                // Select the first (newest) item
                var first = document.querySelector('.media-picker-item');
                if (first) first.click();
            }, 500);
        })
        .catch(function() {
            if (status) status.textContent = adminText('uploadFailed', 'Upload failed');
        });

    input.value = '';
}

// (Gallery functions merged into Image Pool above)

// ==================== Sitemap Generation ====================
function generateSitemap() {
    var btn = document.getElementById('btn-generate-sitemap');
    var status = document.getElementById('sitemap-status');
    if (!btn) return;
    btn.disabled = true;
    btn.textContent = adminText('sitemapGenerating', 'Generating...');
    if (status) status.textContent = '';

    fetch('/admin/sitemap/generate', { method: 'POST' })
        .then(function(resp) { return resp.json(); })
        .then(function(data) {
            if (data.error) {
                if (status) status.textContent = '❌ ' + data.error;
                if (status) status.style.color = '#d63031';
            } else {
                if (status) status.textContent = '✅ ' + adminFormat(adminText('sitemapGenerated', 'Generated sitemap.xml (%d URLs)'), data.count);
                if (status) status.style.color = '#00b894';
            }
        })
        .catch(function() {
            if (status) status.textContent = '❌ ' + adminText('requestFailed', 'Request failed');
            if (status) status.style.color = '#d63031';
        })
        .finally(function() {
            btn.disabled = false;
            btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:-2px;margin-right:4px"><path d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9"/></svg> ' + adminText('generateSitemap', 'Generate Sitemap');
        });
}
