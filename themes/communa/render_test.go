package communa

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/comment"
	"github.com/0xmattg/go-press/core/content"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// TestPagesRenderWithData executes every page template with representative data
// — the same shapes BaseTheme hands to archive/single templates and the typed
// structs the custom routes use. This catches runtime render errors (nil
// dereferences, bad field access, type mismatches in ranges) that a parse-only
// compile test cannot.
func TestPagesRenderWithData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	theme := NewWithDB(nil, ".")
	pages := []string{
		"home", "about", "contact", "members", "member-detail", "groups", "group-detail",
		"discussions", "discussion-detail", "blog", "post-detail", "taxonomy-archive",
		"404", "page", "page-full-width",
	}
	bundle, err := coreTheme.LoadPageBundle(theme, pages)
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	now := time.Now()
	settings := map[string]string{"site_name": "Communa", "company_email": "hi@example.com"}

	member := memberItem()
	group := groupItem()
	discussion := discussionItem(&now)
	post := postItem(&now)
	comments := []comment.View{{
		ID: 1, ContentID: 7, Body: "Great thread!", Status: comment.StatusApproved,
		CreatedAt: now, Author: comment.AuthorView{DisplayName: "Reader"},
	}}
	pagination := &content.PaginatedResult{Total: 8, Page: 1, PerPage: 12, TotalPages: 1}

	cases := map[string]interface{}{
		"home": &HomeData{
			PageData:    PageData{Ctx: c, Title: "Home", ActivePage: "home", Settings: settings},
			Members:     []MemberView{{Title: "Maya", Slug: "maya", Role: "Designer", URL: "/members/maya"}},
			Groups:      []GroupView{{Title: "Design", Slug: "design", MembersCount: "12", Privacy: "Public", URL: "/groups/design", Category: CategoryView{Name: "Arts", Slug: "arts"}}},
			Discussions: []DiscussionView{{Title: "Hi", Slug: "hi", AuthorName: "Maya", RepliesCount: "3", PublishedAt: &now, URL: "/discussions/hi"}},
			Activity: []ActivityItem{
				{Kind: "post", Title: "Welcome", Excerpt: "Hello", AuthorName: "Maya", URL: "/blog/welcome", PublishedAt: &now},
				{Kind: "discussion", Title: "Chat", Excerpt: "Join", AuthorName: "Leo", GroupName: "Design", Count: 5, URL: "/discussions/chat", PublishedAt: &now},
			},
			Featured: []PostView{
				{Title: "Welcome to Communa", Slug: "welcome", ImageURL: "https://example.com/p.jpg", Excerpt: "A quick tour", Category: CategoryView{Name: "News", Slug: "news"}, URL: "/blog/welcome", PublishedAt: &now},
			},
			Stats: CommunityStats{Members: 8, Groups: 8, Discussions: 8, Posts: 8},
		},
		"about":   &AboutData{PageData: PageData{Ctx: c, Title: "About", ActivePage: "about", Settings: settings}, Stats: CommunityStats{Members: 8}},
		"contact": &ContactData{PageData: PageData{Ctx: c, Title: "Contact", ActivePage: "contact", Settings: settings}, Error: "email"},
		"members": gin.H{
			"Ctx": c, "Title": "Members", "Settings": settings, "Pagination": pagination,
			"Items": []map[string]interface{}{member}, "Tags": termList(), "ActiveTag": "",
			"SearchQuery": "", "ArchiveURL": "/members",
		},
		"member-detail": gin.H{
			"Ctx": c, "Title": "Maya", "Settings": settings, "Item": member,
			"Tags": termList(), "Related": []map[string]interface{}{member}, "ArchiveURL": "/members",
		},
		"groups": gin.H{
			"Ctx": c, "Title": "Groups", "Settings": settings, "Pagination": pagination,
			"Items": []map[string]interface{}{group}, "Categories": termList(), "ActiveCat": "",
			"SearchQuery": "", "ArchiveURL": "/groups",
		},
		"group-detail": gin.H{
			"Ctx": c, "Title": "Design", "Settings": settings, "Item": group,
			"Tags": termList(), "Related": []map[string]interface{}{group}, "ArchiveURL": "/groups",
		},
		"discussions": gin.H{
			"Ctx": c, "Title": "Discussions", "Settings": settings, "Pagination": pagination,
			"Items": []map[string]interface{}{discussion}, "Categories": termList(), "ActiveCat": "",
			"SearchQuery": "", "ArchiveURL": "/discussions",
		},
		"discussion-detail": gin.H{
			"Ctx": c, "Title": "Hi", "Settings": settings, "Item": discussion,
			"Tags": termList(), "Related": []map[string]interface{}{discussion}, "ArchiveURL": "/discussions",
			"Permalink": "/discussions/hi", "Comments": comments, "CommentCount": int64(1),
			"CommentsOpen": true, "CanComment": true, "CommentNotice": "", "CommentError": "",
		},
		"blog": gin.H{
			"Ctx": c, "Title": "Blog", "Settings": settings, "Pagination": pagination,
			"Items": []map[string]interface{}{post}, "Categories": termList(), "ActiveCat": "", "ActiveTag": "",
			"SearchQuery": "", "ArchiveURL": "/blog",
		},
		"post-detail": gin.H{
			"Ctx": c, "Title": "Welcome", "Settings": settings, "Item": post,
			"Tags": termList(), "Related": []map[string]interface{}{post}, "ArchiveURL": "/blog",
			"Permalink": "/blog/welcome", "Comments": []comment.View(nil), "CommentCount": int64(0),
			"CommentsOpen": true, "CanComment": false, "CommentNotice": "", "CommentError": "",
		},
		"taxonomy-archive": gin.H{
			"Ctx": c, "Title": "Arts", "Settings": settings, "TaxSlug": "category", "TermName": "Arts",
			"Items": []map[string]interface{}{taxonomyItem(member)},
		},
		"404":             gin.H{"Ctx": c, "Title": "Not found", "Settings": settings},
		"page":            gin.H{"Ctx": c, "Title": "Page", "Settings": settings, "Item": pageItem()},
		"page-full-width": gin.H{"Ctx": c, "Title": "Page", "Settings": settings, "Item": pageItem()},
	}

	for _, name := range pages {
		data, ok := cases[name]
		if !ok {
			t.Fatalf("no test data for page %q", name)
		}
		tmpl := bundle[name]
		if tmpl == nil {
			t.Fatalf("missing compiled template %q", name)
		}
		var out bytes.Buffer
		if err := tmpl.ExecuteTemplate(&out, "base", data); err != nil {
			t.Fatalf("render %q: %v", name, err)
		}
		if !strings.Contains(out.String(), "cmn-footer") {
			t.Fatalf("render %q did not include the footer — base layout broken", name)
		}
	}
}

func termList() []map[string]interface{} {
	return []map[string]interface{}{{"Name": "Arts", "Slug": "arts"}, {"Name": "Tech", "Slug": "tech"}}
}

func cat() map[string]interface{} { return map[string]interface{}{"Name": "Arts", "Slug": "arts"} }

func memberItem() map[string]interface{} {
	return map[string]interface{}{
		"Title": "Maya Chen", "Slug": "maya-chen", "URL": "/members/maya-chen",
		"Excerpt": "Designer & plant hoarder", "Content": "<p>Bio here.</p>",
		"ImageURL": "https://example.com/a.png", "CoverImage": "https://example.com/c.jpg",
		"Role": "Product Designer", "Location": "Lisbon", "Joined": "March 2024",
		"Website": "https://maya.example", "PostsCount": "34", "GroupsCount": "5",
		"Tags": termList(),
	}
}

func groupItem() map[string]interface{} {
	return map[string]interface{}{
		"Title": "Design Collective", "Slug": "design-collective", "URL": "/groups/design-collective",
		"Excerpt": "Share your work", "Content": "<p>About the group.</p>",
		"ImageURL": "https://example.com/g.jpg", "MembersCount": "1,284", "Privacy": "Public",
		"Activity": "Active 2 hours ago", "Lead": "Maya Chen", "Category": cat(), "Tags": termList(),
	}
}

func discussionItem(now *time.Time) map[string]interface{} {
	return map[string]interface{}{
		"ID": uint(7), "Title": "What's on your desk?", "Slug": "desk", "URL": "/discussions/desk",
		"Excerpt": "Show us your setup", "Content": "<p>Opening post.</p>",
		"AuthorName": "Maya Chen", "AuthorAvatar": "https://example.com/a.png",
		"GroupName": "Design Collective", "RepliesCount": "42", "Category": cat(),
		"Tags": termList(), "PublishedAt": now,
	}
}

func postItem(now *time.Time) map[string]interface{} {
	return map[string]interface{}{
		"ID": uint(9), "Title": "Welcome to Communa", "Slug": "welcome", "URL": "/blog/welcome",
		"Excerpt": "A quick tour", "Content": "<p>Body.</p>", "ImageURL": "https://example.com/p.jpg",
		"AuthorName": "Maya Chen", "AuthorAvatar": "https://example.com/a.png",
		"Category": cat(), "Tags": termList(), "PublishedAt": now,
	}
}

func taxonomyItem(base map[string]interface{}) map[string]interface{} {
	item := map[string]interface{}{}
	for k, v := range base {
		item[k] = v
	}
	item["DetailURL"] = "/members/maya-chen"
	item["ContentType"] = "member"
	item["TypeLabel"] = "Member"
	return item
}

func pageItem() map[string]interface{} {
	return map[string]interface{}{
		"Title": "House Rules", "Slug": "house-rules", "Excerpt": "How we roll",
		"Content": "<p>Be kind.</p>", "ImageURL": "",
	}
}
