// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package menu manages navigation menus and theme-declared menu locations.
//
// Menus are persisted in the database and cached in memory for front-end
// rendering. Themes register semantic locations such as "header" or "footer",
// while plugins can filter location resolution to provide language-aware,
// permission-aware, or experimental navigation variants.
package menu
