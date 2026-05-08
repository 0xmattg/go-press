// Package media models uploaded files and responsive image derivatives.
//
// The media repository stores original upload metadata while MediaVariant rows
// track generated sizes and formats. Theme helpers use this data to render
// responsive <picture> markup and preload hints without each theme reimplementing
// image lookup or srcset construction.
package media
