package rewrite

import (
	"strings"

	"go-press/core/content"
)

// PermalinkStructure defines the URL pattern for a content type.
type PermalinkStructure struct {
	// Pattern is the URL template, e.g. "/%type%/%slug%", "/%year%/%month%/%slug%"
	Pattern string
}

// DefaultPermalink returns the default permalink structure: /{rewrite_slug}/{content_slug}
func DefaultPermalink() PermalinkStructure {
	return PermalinkStructure{Pattern: "/%type%/%slug%"}
}

// Build generates a URL for a content item using this permalink structure.
func (p PermalinkStructure) Build(c *content.Content, typeDef *content.ContentTypeDef) string {
	prefix := typeDef.Rewrite.Slug
	if prefix == "" {
		prefix = typeDef.Name
	}

	result := p.Pattern
	result = strings.ReplaceAll(result, "%type%", prefix)
	result = strings.ReplaceAll(result, "%slug%", c.Slug)

	if c.PublishedAt != nil {
		result = strings.ReplaceAll(result, "%year%", c.PublishedAt.Format("2006"))
		result = strings.ReplaceAll(result, "%month%", c.PublishedAt.Format("01"))
		result = strings.ReplaceAll(result, "%day%", c.PublishedAt.Format("02"))
	}

	return result
}
