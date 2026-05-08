// Package plugin defines the minimal contract for GoPress plugins.
//
// Plugins are compiled into the GoPress binary and activated by the Engine at
// runtime. A plugin usually integrates through hooks, filters, admin settings
// providers, custom routes, or repository access exposed through the Engine.
// Deactivate should remove any registered hook handles so disabled plugins stop
// affecting live requests without a process restart.
package plugin
